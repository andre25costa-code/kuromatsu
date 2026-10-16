package webui

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/andre25costa-code/kuromatsu/pkg/config"
	"github.com/andre25costa-code/kuromatsu/pkg/logger"
	"github.com/andre25costa-code/kuromatsu/pkg/skills"
)

// skillInstallTimeout bounds one install from GitHub.
const skillInstallTimeout = 2 * time.Minute

var (
	repoPattern      = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	skillNamePattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
)

// skillInstaller is the part of skills.SkillInstaller the UI uses.
type skillInstaller interface {
	InstallFromGitHub(ctx context.Context, repo string) error
	Uninstall(name string) error
}

// defaultInstaller installs into the workspace with the running config's
// GitHub settings (base URL, token, proxy).
func defaultInstaller(workspace string, rt *config.Config) (skillInstaller, error) {
	var base, token, proxy string
	if rt != nil {
		gh := rt.Tools.Skills.Github
		base, token, proxy = gh.BaseURL, gh.Token.String(), gh.Proxy
	}
	return skills.NewSkillInstallerWithBaseURL(workspace, base, token, proxy)
}

func (s *server) workspace() (string, bool) {
	if s.d.Workspace == nil {
		return "", false
	}
	ws := s.d.Workspace()
	return ws, ws != ""
}

func (s *server) listSkills(w http.ResponseWriter, _ *http.Request) {
	ws, ok := s.workspace()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "workspace unknown")
		return
	}
	type view struct {
		Name        string `json:"name"`
		Source      string `json:"source"`
		Description string `json:"description"`
	}
	list := skills.NewSkillsLoader(ws, "", "").ListSkills()
	out := make([]view, 0, len(list))
	for _, sk := range list {
		out = append(out, view{Name: sk.Name, Source: sk.Source, Description: sk.Description})
	}
	writeJSON(w, http.StatusOK, map[string]any{"skills": out})
}

// installSkill installs owner/name from GitHub. It runs past the shared
// server's write timeout, so it first extends this response's deadline.
func (s *server) installSkill(w http.ResponseWriter, r *http.Request) {
	if err := withDeadline(w, longOpDeadline); err != nil {
		writeError(w, http.StatusInternalServerError, "cannot extend the response deadline: "+err.Error())
		return
	}
	var body struct {
		Repo string `json:"repo"`
	}
	if err := decodeJSON(w, r, &body, maxJSONBody); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !repoPattern.MatchString(body.Repo) {
		writeError(w, http.StatusUnprocessableEntity, `repo must be "owner/name"`)
		return
	}
	ws, ok := s.workspace()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "workspace unknown")
		return
	}
	rt, err := s.runtimeConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load config: "+err.Error())
		return
	}
	inst, err := s.newInstaller(ws, rt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "installer: "+err.Error())
		return
	}
	ctx, cancel := opContext(r, skillInstallTimeout)
	defer cancel()
	if err = inst.InstallFromGitHub(ctx, body.Repo); err != nil {
		writeError(w, http.StatusBadGateway, "install failed: "+err.Error())
		return
	}
	logger.InfoCF("webui", "skill installed from the web UI", map[string]any{"repo": body.Repo})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// removeSkill deletes a workspace skill. The name must be a plain skill
// name (no path, no dot-only name) whose directory holds a SKILL.md.
func (s *server) removeSkill(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !skillNamePattern.MatchString(name) || skills.ValidateSkillName(name) != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid skill name")
		return
	}
	ws, ok := s.workspace()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "workspace unknown")
		return
	}
	root := filepath.Join(ws, "skills")
	dir := filepath.Join(root, name)
	if filepath.Dir(dir) != root {
		writeError(w, http.StatusUnprocessableEntity, "invalid skill name")
		return
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "no workspace skill named "+name)
			return
		}
		writeError(w, http.StatusInternalServerError, "stat failed")
		return
	}
	rt, err := s.runtimeConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load config: "+err.Error())
		return
	}
	inst, err := s.newInstaller(ws, rt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "installer: "+err.Error())
		return
	}
	if err = inst.Uninstall(name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	logger.InfoCF("webui", "skill removed from the web UI", map[string]any{"name": name})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

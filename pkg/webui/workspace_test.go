package webui

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceFiles_WhitelistAndSizeCap(t *testing.T) {
	e := newTestEnv(t)
	configBefore := readFile(t, e.configPath)

	for _, p := range []string{
		"/api/ui/workspace/../config.json",
		"/api/ui/workspace/x.md",
		"/api/ui/workspace/..%2Fconfig.json",
		"/api/ui/workspace/%2E%2E%2Fconfig.json",
		"/api/ui/workspace/soul.md",
		"/api/ui/workspace/SOUL.md/",
		"/api/ui/workspace/./SOUL.md",
	} {
		rec := e.do(bearer(req(http.MethodPut, p, "pwned")))
		assert.Equal(t, http.StatusNotFound, rec.Code, p)
		rec = e.do(bearer(req(http.MethodGet, p, "")))
		assert.Equal(t, http.StatusNotFound, rec.Code, p)
	}
	assert.Equal(t, configBefore, readFile(t, e.configPath))
	_, err := os.Stat(filepath.Join(e.workspace, "x.md"))
	assert.True(t, os.IsNotExist(err))

	big := strings.Repeat("a", maxWorkspaceFile+1)
	rec := e.do(bearer(req(http.MethodPut, "/api/ui/workspace/SOUL.md", big)))
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	_, err = os.Stat(filepath.Join(e.workspace, "SOUL.md"))
	assert.True(t, os.IsNotExist(err), "nothing written on 413")

	exact := strings.Repeat("b", maxWorkspaceFile)
	rec = e.do(bearer(req(http.MethodPut, "/api/ui/workspace/SOUL.md", exact)))
	require.Equal(t, http.StatusOK, rec.Code)

	for _, name := range []string{"SOUL.md", "USER.md", "AGENT.md", "HEARTBEAT.md"} {
		rec = e.do(bearer(req(http.MethodPut, "/api/ui/workspace/"+name, "# "+name)))
		require.Equal(t, http.StatusOK, rec.Code, name)
		rec = e.do(bearer(req(http.MethodGet, "/api/ui/workspace/"+name, "")))
		require.Equal(t, http.StatusOK, rec.Code, name)
		var got struct {
			Content string `json:"content"`
			Exists  bool   `json:"exists"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, "# "+name, got.Content)
		assert.True(t, got.Exists)
	}
	assert.Equal(t, int32(0), e.reloads.Load(), "workspace files need no reload")
}

func TestSkills_RemoveRefusesTraversal(t *testing.T) {
	e := newTestEnv(t)
	skillDir := filepath.Join(e.workspace, "skills", "hello")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: hello\ndescription: says hi\n---\n# hello\n"), 0o644))
	soul := filepath.Join(e.workspace, "SOUL.md")
	require.NoError(t, os.WriteFile(soul, []byte("keep"), 0o644))

	rec := e.do(bearer(req(http.MethodGet, "/api/ui/skills", "")))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"name":"hello"`)

	for _, name := range []string{"..", ".", "...", "a.b", "%2E%2E", "..%2F..", "hello%2F..", "missing"} {
		rec = e.do(bearer(req(http.MethodDelete, "/api/ui/skills/"+name, "")))
		assert.Contains(t, []int{http.StatusUnprocessableEntity, http.StatusNotFound}, rec.Code, name)
	}
	assert.Equal(t, "keep", readFile(t, soul), "workspace untouched")
	_, err := os.Stat(skillDir)
	require.NoError(t, err)

	rec = e.do(bearer(req(http.MethodDelete, "/api/ui/skills/hello", "")))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	_, err = os.Stat(skillDir)
	assert.True(t, os.IsNotExist(err))
	assert.Equal(t, "keep", readFile(t, soul))
}

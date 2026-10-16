package webui

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/andre25costa-code/kuromatsu/pkg/fileutil"
)

// maxWorkspaceFile caps a workspace file written from the UI.
const maxWorkspaceFile = 256 << 10

// workspaceFiles are the only workspace files the UI reads or writes; the
// name in the URL is matched against this list, never used as a path.
var workspaceFiles = map[string]bool{
	"SOUL.md":      true,
	"USER.md":      true,
	"AGENT.md":     true,
	"HEARTBEAT.md": true,
}

func (s *server) workspaceFile(r *http.Request) (string, bool) {
	name := r.PathValue("file")
	if !workspaceFiles[name] || s.d.Workspace == nil {
		return "", false
	}
	ws := s.d.Workspace()
	if ws == "" {
		return "", false
	}
	return filepath.Join(ws, name), true
}

func (s *server) getWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	p, ok := s.workspaceFile(r)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown file")
		return
	}
	data, err := os.ReadFile(p)
	exists := true
	if errors.Is(err, os.ErrNotExist) {
		data, exists, err = nil, false, nil
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": r.PathValue("file"), "content": string(data), "exists": exists,
	})
}

// putWorkspaceFile replaces the file with the request body (the raw text).
func (s *server) putWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	p, ok := s.workspaceFile(r)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown file")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWorkspaceFile))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "file over 256 KB")
			return
		}
		writeError(w, http.StatusBadRequest, "read body failed")
		return
	}
	if err = os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "workspace not writable")
		return
	}
	if err = fileutil.WriteFileAtomic(p, data, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "write failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "bytes": len(data)})
}

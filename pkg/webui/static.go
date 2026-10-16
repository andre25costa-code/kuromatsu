package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFiles embed.FS

// staticHandler serves index.html, app.css and app.js under /ui/.
func staticHandler() http.Handler {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic("webui: embedded static directory missing: " + err.Error())
	}
	return http.StripPrefix("/ui", http.FileServerFS(sub))
}

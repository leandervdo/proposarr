package web

import (
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

const notBuiltPage = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Proposarr</title>
<style>body{font:16px/1.5 system-ui,sans-serif;background:#0f1115;color:#e6e6e6;display:grid;place-items:center;min-height:100vh;margin:0}
main{max-width:34rem;padding:2rem}code{background:#1c2029;padding:.1rem .35rem;border-radius:4px}</style></head>
<body><main><h1>Proposarr is running</h1>
<p>The web UI has not been built into this binary. Build it with <code>pnpm install &amp;&amp; pnpm build</code> in <code>web/</code>, then rebuild Proposarr.</p>
<p>The API is available under <code>/api</code>.</p></main></body>
</html>
`

// static serves the single-page app, falling back to index.html for client routes.
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.o.UI == nil || !isFile(s.o.UI, "index.html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		io.WriteString(w, notBuiltPage)
		return
	}

	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name != "" && name != "index.html" && isFile(s.o.UI, name) {
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.ServeFileFS(w, r, s.o.UI, name)
		return
	}
	if strings.HasPrefix(name, "assets/") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, s.o.UI, "index.html")
}

func isFile(fsys fs.FS, name string) bool {
	fi, err := fs.Stat(fsys, name)
	return err == nil && !fi.IsDir()
}

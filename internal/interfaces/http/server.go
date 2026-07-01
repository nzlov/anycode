package http

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/nzlov/anycode/internal/infra/config"
	"github.com/nzlov/anycode/internal/interfaces/http/static"
)

func NewHandler(cfg config.Config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.Handle("GET /api/healthz", bearerAuth(cfg.AccessKey, http.HandlerFunc(healthz)))
	mux.Handle("/", newSPAHandler())

	return mux
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func bearerAuth(accessKey string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accessKey == "" {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+accessKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type spaHandler struct {
	fsys fs.FS
}

func newSPAHandler() http.Handler {
	fsys, err := fs.Sub(static.Files, static.DistDir)
	if err != nil {
		panic(err)
	}
	return spaHandler{fsys: fsys}
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	if stat, err := fs.Stat(h.fsys, name); err == nil && !stat.IsDir() {
		http.ServeFileFS(w, r, h.fsys, name)
		return
	}
	http.ServeFileFS(w, r, h.fsys, "index.html")
}

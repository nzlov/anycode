package http

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"strings"

	"github.com/99designs/gqlgen/graphql/handler"
	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
	authdomain "github.com/nzlov/anycode/internal/domain/auth"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/infra/config"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph/generated"
	"github.com/nzlov/anycode/internal/interfaces/http/static"
)

type HandlerOption func(*handlerOptions)

type handlerOptions struct {
	graphqlHandler http.Handler
	attachments    attachmentapp.UseCase
	playground     bool
}

func WithGraphQLUseCases(useCases graph.UseCases) HandlerOption {
	return func(opts *handlerOptions) {
		resolver := graph.NewResolver(useCases)
		schema := generated.NewExecutableSchema(generated.Config{Resolvers: resolver})
		opts.graphqlHandler = handler.NewDefaultServer(schema)
	}
}

func WithGraphQLHandler(handler http.Handler) HandlerOption {
	return func(opts *handlerOptions) {
		opts.graphqlHandler = handler
	}
}

func WithPlayground() HandlerOption {
	return func(opts *handlerOptions) {
		opts.playground = true
	}
}

func WithAttachmentUseCase(useCase attachmentapp.UseCase) HandlerOption {
	return func(opts *handlerOptions) {
		opts.attachments = useCase
	}
}

func NewHandler(cfg config.Config, options ...HandlerOption) http.Handler {
	opts := handlerOptions{
		graphqlHandler: http.HandlerFunc(graphqlNotConfigured),
	}
	for _, option := range options {
		option(&opts)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.Handle("GET /api/healthz", bearerAuth(cfg.AccessKey, http.HandlerFunc(healthz)))
	mux.Handle("/graphql", bearerAuth(cfg.AccessKey, withPrincipal(cfg.AccessKey, opts.graphqlHandler)))
	attachmentHandler := newAttachmentHandler(opts.attachments)
	mux.Handle("GET /attachments/{id}/preview", bearerAuth(cfg.AccessKey, attachmentHandler.preview()))
	mux.Handle("GET /attachments/{id}/download", bearerAuth(cfg.AccessKey, attachmentHandler.download()))
	if opts.playground {
		mux.Handle("GET /playground", bearerAuth(cfg.AccessKey, http.HandlerFunc(playgroundHandler)))
	}
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

func withPrincipal(accessKey string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accessKey == "" {
			next.ServeHTTP(w, r)
			return
		}
		sum := sha256.Sum256([]byte(accessKey))
		principal := authdomain.AccessPrincipal{
			KeyHash: hex.EncodeToString(sum[:]),
			Kind:    "http_bearer",
		}
		next.ServeHTTP(w, r.WithContext(graph.WithPrincipal(r.Context(), principal)))
	})
}

func graphqlNotConfigured(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "graphql not configured", http.StatusServiceUnavailable)
}

type attachmentHandler struct {
	useCase attachmentapp.UseCase
}

func newAttachmentHandler(useCase attachmentapp.UseCase) attachmentHandler {
	return attachmentHandler{useCase: useCase}
}

func (h attachmentHandler) preview() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.serve(w, r, attachmentapp.OpenPreview)
	})
}

func (h attachmentHandler) download() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.serve(w, r, attachmentapp.OpenDownload)
	})
}

func (h attachmentHandler) serve(w http.ResponseWriter, r *http.Request, mode attachmentapp.OpenMode) {
	if h.useCase == nil {
		http.Error(w, "attachment service unavailable", http.StatusServiceUnavailable)
		return
	}
	stream, err := h.useCase.OpenAttachment(r.Context(), sessiondomain.AttachmentID(r.PathValue("id")), mode)
	if err != nil {
		writeAttachmentError(w, err)
		return
	}
	defer stream.Reader.Close()

	if stream.MimeType != "" {
		w.Header().Set("Content-Type", stream.MimeType)
	}
	disposition := "inline"
	if mode == attachmentapp.OpenDownload {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": stream.Filename}))
	if _, err := io.Copy(w, stream.Reader); err != nil {
		return
	}
}

func writeAttachmentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, attachmentapp.ErrNotPreviewable):
		http.Error(w, "attachment is not previewable", http.StatusUnsupportedMediaType)
	case errors.Is(err, attachmentapp.ErrAttachmentNotFound):
		http.Error(w, "attachment not found", http.StatusNotFound)
	default:
		http.Error(w, "attachment unavailable", http.StatusInternalServerError)
	}
}

func playgroundHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><html><head><title>AnyCode GraphQL</title></head><body><main><h1>AnyCode GraphQL</h1><input id="token" placeholder="Bearer token"><br><textarea id="query" rows="12" cols="80">{ __typename }</textarea><br><button id="run">Run</button><pre id="result"></pre></main><script>document.getElementById("run").onclick=async()=>{const token=document.getElementById("token").value.trim();const headers={"Content-Type":"application/json"};if(token)headers.Authorization=token.startsWith("Bearer ")?token:"Bearer "+token;const res=await fetch("/graphql",{method:"POST",headers,body:JSON.stringify({query:document.getElementById("query").value})});document.getElementById("result").textContent=await res.text();};</script></body></html>`))
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
	if _, err := fs.Stat(h.fsys, "index.html"); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(fallbackIndexHTML))
		return
	}
	http.ServeFileFS(w, r, h.fsys, "index.html")
}

const fallbackIndexHTML = `<!doctype html><html><head><title>AnyCode</title></head><body><div id=q-app></div></body></html>`

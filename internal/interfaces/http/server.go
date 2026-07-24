package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/nzlov/anycode/internal/application/apperror"
	artifactapp "github.com/nzlov/anycode/internal/application/artifact"
	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	settingapp "github.com/nzlov/anycode/internal/application/setting"
	authdomain "github.com/nzlov/anycode/internal/domain/auth"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	terminaldomain "github.com/nzlov/anycode/internal/domain/terminal"
	"github.com/nzlov/anycode/internal/infra/config"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph/generated"
	"github.com/nzlov/anycode/internal/interfaces/http/static"
	"github.com/vektah/gqlparser/v2/ast"
)

type HandlerOption func(*handlerOptions)

type handlerOptions struct {
	graphqlHandler  http.Handler
	attachments     attachmentapp.UseCase
	settings        settingapp.UseCase
	artifacts       artifactapp.UseCase
	sessions        sessionapp.UseCase
	terminal        terminaldomain.Runtime
	accessKey       string
	playground      bool
	previewMaxBytes int64
}

func WithGraphQLUseCases(useCases graph.UseCases) HandlerOption {
	return func(opts *handlerOptions) {
		resolver := graph.NewResolver(useCases)
		schema := generated.NewExecutableSchema(generated.Config{Resolvers: resolver})
		opts.graphqlHandler = newGraphQLServer(schema, opts.accessKey)
		opts.sessions = useCases.Sessions
		opts.artifacts = useCases.Artifacts
		opts.settings = useCases.Settings
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

func WithTerminalRuntime(runtime terminaldomain.Runtime) HandlerOption {
	return func(opts *handlerOptions) {
		opts.terminal = runtime
	}
}

func NewHandler(cfg config.Config, options ...HandlerOption) http.Handler {
	opts := handlerOptions{
		graphqlHandler:  http.HandlerFunc(graphqlNotConfigured),
		accessKey:       cfg.AccessKey,
		previewMaxBytes: cfg.ArtifactPreviewMaxBytes,
	}
	for _, option := range options {
		option(&opts)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	mux.Handle("GET /api/healthz", bearerAuth(cfg.AccessKey, http.HandlerFunc(healthz)))
	mux.Handle("/graphql", graphqlAuth(cfg.AccessKey, withPrincipal(cfg.AccessKey, opts.graphqlHandler)))
	mux.Handle("GET /api/terminals/{id}/ws", newTerminalWebSocketHandler(opts.sessions, opts.terminal, cfg.AccessKey))
	attachmentHandler := newAttachmentHandler(opts.attachments, opts.previewMaxBytes)
	mux.Handle("GET /files/{id}/preview", bearerAuth(cfg.AccessKey, attachmentHandler.preview()))
	mux.Handle("GET /files/{id}/download", bearerAuth(cfg.AccessKey, attachmentHandler.download()))
	mux.Handle("GET /api/appearance/wallpapers/{id}", bearerAuth(cfg.AccessKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveAppearanceWallpaper(w, r, opts.settings)
	})))
	mux.Handle("GET /api/appearance/nasa-wallpaper", bearerAuth(cfg.AccessKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveNASAWallpaper(w, r, opts.settings)
	})))
	if opts.playground {
		mux.Handle("GET /playground", bearerAuth(cfg.AccessKey, http.HandlerFunc(playgroundHandler)))
	}
	mux.Handle("/", newPWAHandler())

	return mux
}

func serveAppearanceWallpaper(w http.ResponseWriter, r *http.Request, settings settingapp.UseCase) {
	if settings == nil {
		writeApplicationError(w, http.StatusServiceUnavailable, apperror.New(apperror.CodeInternal, apperror.CategoryInfraError, "appearance service unavailable").WithRetryable(true))
		return
	}
	stream, err := settings.OpenAppearanceWallpaper(r.Context(), r.PathValue("id"))
	if err != nil {
		writeApplicationError(w, http.StatusNotFound, apperror.New(apperror.CodeNotFound, apperror.CategoryValidationError, "wallpaper not found"))
		return
	}
	defer stream.Reader.Close()
	w.Header().Set("Content-Type", stream.MimeType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": stream.Filename}))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, stream.Reader)
}

func serveNASAWallpaper(w http.ResponseWriter, r *http.Request, settings settingapp.UseCase) {
	if settings == nil {
		writeApplicationError(w, http.StatusServiceUnavailable, apperror.New(apperror.CodeInternal, apperror.CategoryInfraError, "appearance service unavailable").WithRetryable(true))
		return
	}
	stream, err := settings.OpenNASAWallpaper(r.Context())
	if err != nil {
		appErr, ok := apperror.From(err)
		if !ok {
			appErr = apperror.Wrap(err, apperror.CodeInternal, apperror.CategoryInfraError, "load NASA wallpaper failed").WithRetryable(true)
		}
		writeApplicationError(w, http.StatusBadGateway, appErr)
		return
	}
	defer stream.Reader.Close()
	w.Header().Set("Content-Type", stream.MimeType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": stream.Filename}))
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, stream.Reader)
}

func newGraphQLServer(schema graphql.ExecutableSchema, accessKey string) http.Handler {
	srv := handler.New(schema)
	srv.AddTransport(transport.Websocket{
		PingPongInterval: 10 * time.Second,
		InitFunc:         websocketInitFunc(accessKey),
	})
	srv.AddTransport(transport.Options{})
	srv.AddTransport(transport.GET{})
	srv.AddTransport(transport.POST{})
	srv.AddTransport(transport.MultipartForm{})
	srv.SetQueryCache(lru.New[*ast.QueryDocument](1000))
	srv.SetErrorPresenter(graph.ErrorPresenter)
	srv.Use(extension.Introspection{})
	srv.Use(extension.AutomaticPersistedQuery{
		Cache: lru.New[string](100),
	})
	return srv
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func graphqlAuth(accessKey string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accessKey == "" || isWebSocketUpgrade(r) {
			next.ServeHTTP(w, r)
			return
		}
		if !validBearer(accessKey, r.Header.Get("Authorization")) {
			writeApplicationError(w, http.StatusUnauthorized, apperror.New(apperror.CodeAuthFailed, apperror.CategoryAuthError, "unauthorized"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerAuth(accessKey string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accessKey == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !validBearer(accessKey, r.Header.Get("Authorization")) {
			writeApplicationError(w, http.StatusUnauthorized, apperror.New(apperror.CodeAuthFailed, apperror.CategoryAuthError, "unauthorized"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func websocketInitFunc(accessKey string) transport.WebsocketInitFunc {
	return func(ctx context.Context, payload transport.InitPayload) (context.Context, *transport.InitPayload, error) {
		if accessKey == "" {
			return ctx, nil, nil
		}
		if !validBearer(accessKey, payload.Authorization()) {
			return ctx, nil, apperror.New(apperror.CodeAuthFailed, apperror.CategoryAuthError, "unauthorized")
		}
		return graph.WithPrincipal(ctx, accessPrincipal(accessKey, "websocket_connection_init")), nil, nil
	}
}

func validBearer(accessKey string, authorization string) bool {
	return authorization == "Bearer "+accessKey
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func withPrincipal(accessKey string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accessKey == "" {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(graph.WithPrincipal(r.Context(), accessPrincipal(accessKey, "http_bearer"))))
	})
}

func accessPrincipal(accessKey string, kind string) authdomain.AccessPrincipal {
	return authdomain.NewAccessPrincipal(accessKey, kind)
}

func graphqlNotConfigured(w http.ResponseWriter, _ *http.Request) {
	writeApplicationError(w, http.StatusServiceUnavailable, apperror.New(apperror.CodeInternal, apperror.CategoryInfraError, "graphql not configured").WithRetryable(true))
}

type attachmentHandler struct {
	useCase         attachmentapp.UseCase
	previewMaxBytes int64
}

func newAttachmentHandler(useCase attachmentapp.UseCase, previewMaxBytes ...int64) attachmentHandler {
	limit := int64(128 << 20)
	if len(previewMaxBytes) > 0 && previewMaxBytes[0] > 0 {
		limit = previewMaxBytes[0]
	}
	return attachmentHandler{useCase: useCase, previewMaxBytes: limit}
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
		writeApplicationError(w, http.StatusServiceUnavailable, apperror.New(apperror.CodeAttachmentFailed, apperror.CategoryInfraError, "attachment service unavailable").WithRetryable(true))
		return
	}
	stream, err := h.useCase.OpenAttachment(r.Context(), sessiondomain.AttachmentID(r.PathValue("id")), mode)
	if err != nil {
		writeAttachmentError(w, err)
		return
	}
	defer stream.Reader.Close()
	if mode == attachmentapp.OpenPreview && stream.Size > h.previewMaxBytes {
		writeApplicationError(w, http.StatusRequestEntityTooLarge, apperror.New(apperror.CodeAttachmentFailed, apperror.CategoryValidationError, "file is too large to preview"))
		return
	}

	if stream.MimeType != "" {
		w.Header().Set("Content-Type", stream.MimeType)
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	disposition := "inline"
	if mode == attachmentapp.OpenDownload {
		disposition = "attachment"
	} else {
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'")
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": stream.Filename}))
	if stream.Seeker != nil {
		http.ServeContent(w, r, stream.Filename, stream.ModifiedAt, stream.Seeker)
		return
	}
	if _, err := io.Copy(w, stream.Reader); err != nil {
		return
	}
}

func writeAttachmentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, attachmentapp.ErrNotPreviewable):
		writeApplicationError(w, http.StatusUnsupportedMediaType, apperror.New(apperror.CodeAttachmentFailed, apperror.CategoryValidationError, "attachment is not previewable"))
	case errors.Is(err, attachmentapp.ErrAttachmentNotFound):
		writeApplicationError(w, http.StatusNotFound, apperror.New(apperror.CodeNotFound, apperror.CategoryValidationError, "attachment not found"))
	default:
		writeApplicationError(w, http.StatusInternalServerError, apperror.Wrap(err, apperror.CodeAttachmentFailed, apperror.CategoryInfraError, "attachment unavailable").WithRetryable(true))
	}
}

func writeApplicationError(w http.ResponseWriter, status int, appErr *apperror.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	response := map[string]any{
		"code":       appErr.Code,
		"category":   string(appErr.Category),
		"message":    appErr.PublicMessage(),
		"retryable":  appErr.Retryable,
		"userAction": appErr.UserAction,
	}
	if details := appErr.PublicDetails(); len(details) > 0 {
		response["details"] = details
	}
	_ = json.NewEncoder(w).Encode(response)
}

func playgroundHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><html><head><title>AnyCode GraphQL</title></head><body><main><h1>AnyCode GraphQL</h1><input id="token" placeholder="Bearer token"><br><textarea id="query" rows="12" cols="80">{ __typename }</textarea><br><button id="run">Run</button><pre id="result"></pre></main><script>document.getElementById("run").onclick=async()=>{const token=document.getElementById("token").value.trim();const headers={"Content-Type":"application/json"};if(token)headers.Authorization=token.startsWith("Bearer ")?token:"Bearer "+token;const res=await fetch("/graphql",{method:"POST",headers,body:JSON.stringify({query:document.getElementById("query").value})});document.getElementById("result").textContent=await res.text();};</script></body></html>`))
}

type pwaHandler struct {
	fsys fs.FS
}

func newPWAHandler() http.Handler {
	fsys, err := fs.Sub(static.Files, static.PWADir)
	if err != nil {
		panic(err)
	}
	return pwaHandler{fsys: fsys}
}

func (h pwaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

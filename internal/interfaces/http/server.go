package http

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/executor"
	authdomain "github.com/nzlov/anycode/internal/domain/auth"
	"github.com/nzlov/anycode/internal/infra/config"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph/generated"
	"github.com/nzlov/anycode/internal/interfaces/http/static"
)

type HandlerOption func(*handlerOptions)

type handlerOptions struct {
	graphqlHandler http.Handler
	playground     bool
}

func WithGraphQLUseCases(useCases graph.UseCases) HandlerOption {
	return func(opts *handlerOptions) {
		resolver := graph.NewResolver(useCases)
		schema := generated.NewExecutableSchema(generated.Config{Resolvers: resolver})
		opts.graphqlHandler = graphqlHTTPHandler{executor: executor.New(schema)}
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

type graphqlHTTPHandler struct {
	executor graphql.GraphExecutor
}

func (h graphqlHTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.executor == nil {
		graphqlNotConfigured(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := graphql.StartOperationTrace(r.Context())
	params := graphql.RawParams{Headers: r.Header}
	params.ReadTime.Start = graphql.Now()
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		http.Error(w, "invalid graphql json body", http.StatusBadRequest)
		return
	}
	params.ReadTime.End = graphql.Now()

	opCtx, errs := h.executor.CreateOperationContext(ctx, &params)
	w.Header().Set("Content-Type", "application/json")
	if len(errs) > 0 {
		resp := h.executor.DispatchError(graphql.WithOperationContext(ctx, opCtx), errs)
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	responses, ctx := h.executor.DispatchOperation(ctx, opCtx)
	resp := responses(ctx)
	if resp == nil {
		resp = &graphql.Response{}
	}
	_ = json.NewEncoder(w).Encode(resp)
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

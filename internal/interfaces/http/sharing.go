package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/nzlov/anycode/internal/application/apperror"
	sharingapp "github.com/nzlov/anycode/internal/application/sharing"
	"github.com/nzlov/anycode/internal/domain/question"
	"github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph/generated"
)

type shareContextKey struct{}

func shareJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	_ = json.NewEncoder(w).Encode(v)
}
func shareError(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	message := "分享操作失败，请刷新后重试"
	if errors.Is(err, session.ErrShareAccess) {
		code = http.StatusForbidden
		message = "分享访问已失效或无权访问"
	}
	if errors.Is(err, session.ErrShareConflict) {
		code = http.StatusConflict
		message = "链接已领取或消息已审核"
	}
	writeApplicationError(w, code, apperror.New(apperror.CodePermissionDenied, apperror.CategoryValidationError, message))
}
func shareCredential(r *http.Request) string {
	return strings.TrimPrefix(requestAuthorization(r), "Bearer ")
}

func registerSharingRoutes(mux *http.ServeMux, accessKey string, opts handlerOptions) {
	s := opts.useCases.Sharing
	if s == nil {
		return
	}
	admin := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, bearerAuth(accessKey, h)) }
	admin("POST /api/sessions/{id}/shares", func(w http.ResponseWriter, r *http.Request) {
		if accessKey == "" {
			writeApplicationError(w, http.StatusBadRequest, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "请先配置服务访问密钥，再创建受限分享"))
			return
		}
		link, token, err := s.Create(r.Context(), session.ID(r.PathValue("id")))
		if err != nil {
			shareError(w, err)
			return
		}
		shareJSON(w, map[string]any{"share": link, "token": token})
	})
	admin("GET /api/sessions/{id}/shares", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.List(r.Context(), session.ID(r.PathValue("id")))
		if err != nil {
			shareError(w, err)
			return
		}
		shareJSON(w, v)
	})
	admin("DELETE /api/session-shares/{shareID}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Revoke(r.Context(), r.PathValue("shareID")); err != nil {
			shareError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	admin("GET /api/sessions/{id}/share-messages", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.Messages(r.Context(), session.ID(r.PathValue("id")))
		if err != nil {
			shareError(w, err)
			return
		}
		shareJSON(w, v)
	})
	admin("POST /api/share-messages/{messageID}/review", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Approved *bool `json:"approved"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil || input.Approved == nil {
			http.Error(w, "invalid review", 400)
			return
		}
		v, err := s.Review(r.Context(), r.PathValue("messageID"), *input.Approved)
		if err != nil {
			shareError(w, err)
			return
		}
		shareJSON(w, v)
	})
	admin("GET /api/share-messages/{messageID}/files/{fileID}", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.OpenReviewAttachment(r.Context(), r.PathValue("messageID"), r.PathValue("fileID"))
		if err != nil {
			shareError(w, err)
			return
		}
		defer v.Reader.Close()
		w.Header().Set("Content-Type", v.MimeType)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": v.Filename}))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, no-store")
		_, _ = io.Copy(w, v.Reader)
	})
	mux.HandleFunc("POST /api/shared/{shareID}/claim", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Token string `json:"token"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil {
			http.Error(w, "invalid claim", 400)
			return
		}
		link, credential, err := s.Claim(r.Context(), r.PathValue("shareID"), input.Token)
		if err != nil {
			shareError(w, err)
			return
		}
		shareJSON(w, map[string]any{"share": link, "credential": credential})
	})
	guest := func(pattern string, h http.Handler) {
		mux.Handle(pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			link, err := s.Authorize(r.Context(), r.PathValue("shareID"), shareCredential(r))
			if err != nil {
				shareError(w, err)
				return
			}
			w.Header().Set("Cache-Control", "private, no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), shareContextKey{}, link)))
		}))
	}
	guest("GET /api/shared/{shareID}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { shareJSON(w, r.Context().Value(shareContextKey{})) }))
	guest("GET /api/shared/{shareID}/messages", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		link := r.Context().Value(shareContextKey{}).(session.ShareLink)
		messages, err := s.Messages(r.Context(), link.SessionID)
		if err != nil {
			shareError(w, err)
			return
		}
		own := make([]session.SharedMessage, 0)
		for _, v := range messages {
			if v.ShareID == link.ID {
				own = append(own, v)
			}
		}
		shareJSON(w, own)
	}))
	guest("POST /api/shared/{shareID}/messages", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 128<<20)
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			http.Error(w, "invalid message or attachments too large", 400)
			return
		}
		defer r.MultipartForm.RemoveAll()
		var config session.Config
		if json.Unmarshal([]byte(r.FormValue("config")), &config) != nil {
			http.Error(w, "invalid config", 400)
			return
		}
		var uploads []sharingapp.Upload
		for _, f := range r.MultipartForm.File["files"] {
			reader, err := f.Open()
			if err != nil {
				shareError(w, err)
				return
			}
			defer reader.Close()
			uploads = append(uploads, sharingapp.Upload{Filename: f.Filename, MimeType: f.Header.Get("Content-Type"), Size: f.Size, Reader: reader})
		}
		v, err := s.Submit(r.Context(), r.PathValue("shareID"), shareCredential(r), r.FormValue("body"), config, uploads)
		if err != nil {
			shareError(w, err)
			return
		}
		shareJSON(w, v)
	}))
	resolver := graph.NewResolver(opts.useCases)
	gql := handler.New(generated.NewExecutableSchema(generated.Config{Resolvers: resolver}))
	gql.AddTransport(transport.POST{}) // Shared access is request-scoped; no unrestricted WebSocket subscriptions.
	gql.SetErrorPresenter(graph.ErrorPresenter)
	gql.AroundFields(func(ctx context.Context, next graphql.Resolver) (any, error) {
		field := graphql.GetFieldContext(ctx)
		if field.Object == "Query" || field.Object == "Mutation" || field.Object == "Subscription" {
			link := ctx.Value(shareContextKey{}).(session.ShareLink)
			if err := authorizeShareField(ctx, opts, link, field); err != nil {
				return nil, err
			}
		}
		return next(ctx)
	})
	guest("POST /api/shared/{shareID}/graphql", gql)
	registerSharedFileRoutes(mux, guest, opts, s, accessKey)
}

func authorizeShareField(ctx context.Context, opts handlerOptions, link session.ShareLink, field *graphql.FieldContext) error {
	args := field.Field.ArgumentMap(graphql.GetOperationContext(ctx).Variables)
	name := field.Field.Name
	var id any
	switch name {
	case "session", "stopSession":
		id = args["id"]
	case "pendingQuestionRequests":
		id = args["sessionId"]
	case "sessionTranscript", "sessionTranscriptEvent", "sessionDiff", "sessionCommitHistory", "sessionFiles", "resolveSessionArtifacts":
		input, _ := args["input"].(map[string]any)
		id = input["sessionId"]
	case "submitQuestionRequest":
		input, _ := args["input"].(map[string]any)
		requestID, _ := input["requestId"].(string)
		if opts.useCases.Questions != nil {
			request, err := opts.useCases.Questions.GetRequest(ctx, question.RequestID(requestID))
			if err == nil {
				id = string(request.SessionID)
			}
		}
	}
	if id != string(link.SessionID) {
		return apperror.New(apperror.CodePermissionDenied, apperror.CategoryAuthError, "分享仅允许访问指定会话及已授权操作")
	}
	return nil
}

func registerSharedFileRoutes(mux *http.ServeMux, guest func(string, http.Handler), opts handlerOptions, s *sharingapp.Service, accessKey string) {
	tokens := newFileAccessTokens(accessKey)
	// Reuse file handlers only after binding both the share and the requested resource.
	for _, kind := range []string{"preview", "download"} {
		base := "/api/shared/{shareID}/files/{id}/" + kind
		guest("POST "+base+"-token", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			link := r.Context().Value(shareContextKey{}).(session.ShareLink)
			if err := s.CheckFile(r.Context(), link, r.PathValue("id")); err != nil {
				shareError(w, err)
				return
			}
			tokens.writeURL(w, url.URL{Path: strings.TrimSuffix(r.URL.Path, "-token")})
		}))
		mux.Handle("GET "+base, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "private, no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			link, err := s.Active(r.Context(), r.PathValue("shareID"))
			if err == nil {
				err = s.CheckFile(r.Context(), link, r.PathValue("id"))
			}
			if err != nil || !tokens.valid(fileAccessResource(r.URL), r.URL.Query().Get("token")) {
				shareError(w, session.ErrShareAccess)
				return
			}
			h := newAttachmentHandler(opts.attachments, opts.previewMaxBytes)
			if kind == "preview" {
				h.preview().ServeHTTP(w, r)
			} else {
				h.download().ServeHTTP(w, r)
			}
		}))
	}
	for _, kind := range []string{"workspace-file", "diff-media"} {
		base := "/api/shared/{shareID}/api/sessions/{id}/" + kind
		if kind == "workspace-file" {
			guest("HEAD "+base, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				link := r.Context().Value(shareContextKey{}).(session.ShareLink)
				if string(link.SessionID) != r.PathValue("id") {
					shareError(w, session.ErrShareAccess)
					return
				}
				workspaceFileHandler{useCase: opts.workspaceFiles, previewMaxBytes: opts.previewMaxBytes}.ServeHTTP(w, r)
			}))
		}
		for _, mode := range []string{"preview", "download"} {
			if kind == "diff-media" && mode == "download" {
				continue
			}
			guest("POST "+base+"/"+mode+"-token", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				link := r.Context().Value(shareContextKey{}).(session.ShareLink)
				if string(link.SessionID) != r.PathValue("id") {
					shareError(w, session.ErrShareAccess)
					return
				}
				query := r.URL.Query()
				if mode == "download" {
					query.Set("download", "1")
				}
				tokens.writeURL(w, url.URL{Path: strings.TrimSuffix(r.URL.Path, "/"+mode+"-token"), RawQuery: query.Encode()})
			}))
		}
		mux.Handle("GET "+base, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "private, no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			link, err := s.Active(r.Context(), r.PathValue("shareID"))
			if err != nil || string(link.SessionID) != r.PathValue("id") || !tokens.valid(fileAccessResource(r.URL), r.URL.Query().Get("token")) {
				shareError(w, session.ErrShareAccess)
				return
			}
			if kind == "workspace-file" {
				workspaceFileHandler{useCase: opts.workspaceFiles, previewMaxBytes: opts.previewMaxBytes}.ServeHTTP(w, r)
			} else {
				diffMediaHandler{useCase: opts.diff, previewMaxBytes: opts.previewMaxBytes}.ServeHTTP(w, r)
			}
		}))
	}
}

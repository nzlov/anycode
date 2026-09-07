package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nzlov/anycode/internal/application/apperror"
	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
)

const fileAccessTokenTTL = time.Minute

type fileAccessTokens struct {
	key []byte
	now func() time.Time
	ttl time.Duration
}

func newFileAccessTokens(accessKey string) fileAccessTokens {
	key := sha256.Sum256([]byte("anycode:file-access:v1\x00" + accessKey))
	return fileAccessTokens{key: key[:], now: time.Now, ttl: fileAccessTokenTTL}
}

func (t fileAccessTokens) issue(resource string) string {
	expiresAt := t.now().Add(t.ttl).Unix()
	payload := fileAccessTokenPayload(resource, expiresAt)
	signature := hmac.New(sha256.New, t.key)
	_, _ = signature.Write([]byte(payload))
	return "v1." + strconv.FormatInt(expiresAt, 10) + "." + base64.RawURLEncoding.EncodeToString(signature.Sum(nil))
}

func (t fileAccessTokens) valid(resource string, token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return false
	}
	expiresAt, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || expiresAt < t.now().Unix() {
		return false
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	signature := hmac.New(sha256.New, t.key)
	_, _ = signature.Write([]byte(fileAccessTokenPayload(resource, expiresAt)))
	return hmac.Equal(provided, signature.Sum(nil))
}

func fileAccessTokenPayload(resource string, expiresAt int64) string {
	return "file\x00" + resource + "\x00" + strconv.FormatInt(expiresAt, 10)
}

func fileAccessAuth(accessKey string, tokens fileAccessTokens, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accessKey == "" || validBearer(accessKey, r.Header.Get("Authorization")) || tokens.valid(fileAccessResource(r.URL), r.URL.Query().Get("token")) {
			if r.URL.Query().Has("token") {
				w.Header().Set("Cache-Control", "private, no-store")
				w.Header().Set("Referrer-Policy", "no-referrer")
			}
			next.ServeHTTP(w, r)
			return
		}
		writeApplicationError(w, http.StatusUnauthorized, apperror.New(apperror.CodeAuthFailed, apperror.CategoryAuthError, "unauthorized"))
	})
}

func fileAccessResource(target *url.URL) string {
	query := target.Query()
	query.Del("token")
	return target.EscapedPath() + "?" + query.Encode()
}

func (t fileAccessTokens) writeURL(w http.ResponseWriter, target url.URL) {
	query := target.Query()
	query.Set("token", t.issue(fileAccessResource(&target)))
	target.RawQuery = query.Encode()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"url": target.String()})
}

func (h attachmentHandler) accessToken(tokens fileAccessTokens, mode attachmentapp.OpenMode) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokens.writeURL(w, url.URL{Path: "/files/" + r.PathValue("id") + "/" + string(mode)})
	})
}

func (h workspaceFileHandler) downloadToken(tokens fileAccessTokens) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := url.Values{"path": []string{r.URL.Query().Get("path")}, "download": []string{"1"}}
		tokens.writeURL(w, url.URL{
			Path:     "/api/sessions/" + r.PathValue("id") + "/workspace-file",
			RawQuery: query.Encode(),
		})
	})
}

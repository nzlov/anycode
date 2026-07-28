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
)

const filePreviewTokenTTL = time.Minute

type filePreviewTokens struct {
	key []byte
	now func() time.Time
	ttl time.Duration
}

func newFilePreviewTokens(accessKey string) filePreviewTokens {
	key := sha256.Sum256([]byte("anycode:file-preview:v1\x00" + accessKey))
	return filePreviewTokens{key: key[:], now: time.Now, ttl: filePreviewTokenTTL}
}

func (t filePreviewTokens) issue(fileID string) string {
	expiresAt := t.now().Add(t.ttl).Unix()
	payload := filePreviewTokenPayload(fileID, expiresAt)
	signature := hmac.New(sha256.New, t.key)
	_, _ = signature.Write([]byte(payload))
	return "v1." + strconv.FormatInt(expiresAt, 10) + "." + base64.RawURLEncoding.EncodeToString(signature.Sum(nil))
}

func (t filePreviewTokens) valid(fileID string, token string) bool {
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
	_, _ = signature.Write([]byte(filePreviewTokenPayload(fileID, expiresAt)))
	return hmac.Equal(provided, signature.Sum(nil))
}

func filePreviewTokenPayload(fileID string, expiresAt int64) string {
	return "preview\x00" + fileID + "\x00" + strconv.FormatInt(expiresAt, 10)
}

func filePreviewAuth(accessKey string, tokens filePreviewTokens, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accessKey == "" || validBearer(accessKey, r.Header.Get("Authorization")) || tokens.valid(r.PathValue("id"), r.URL.Query().Get("token")) {
			next.ServeHTTP(w, r)
			return
		}
		writeApplicationError(w, http.StatusUnauthorized, apperror.New(apperror.CodeAuthFailed, apperror.CategoryAuthError, "unauthorized"))
	})
}

func (h attachmentHandler) previewToken(tokens filePreviewTokens) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fileID := r.PathValue("id")
		previewURL := "/files/" + url.PathEscape(fileID) + "/preview"
		query := url.Values{"token": []string{tokens.issue(fileID)}}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"url": previewURL + "?" + query.Encode()})
	})
}

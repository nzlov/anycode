package cloudflared

import (
	"context"
	"crypto/sha256"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	domain "github.com/nzlov/anycode/internal/domain/tunnel"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestAuthQuerySetsCookieAndRedirectsWithoutToken(t *testing.T) {
	runtime, item := testRuntime("secret-token")
	request := newRequest("https://example.trycloudflare.com/app?x=1&anycode_auth=secret-token")
	response := recordResponse(runtime, request)

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/app?x=1" {
		t.Fatalf("location = %q", location)
	}
	if strings.Contains(response.Header.Get("Location"), "secret-token") {
		t.Fatal("redirect location leaked the auth token")
	}
	cookies := response.Cookies()
	if len(cookies) != 1 || cookies[0].Name != authCookie || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("cookies = %#v", cookies)
	}
	if cookies[0].Value != runtime.cookieValue(item) {
		t.Fatal("unexpected auth cookie value")
	}
}

func TestCookieAllowsProxyAndIsNotForwardedToOrigin(t *testing.T) {
	runtime, item := testRuntime("secret-token")
	var originCookie string
	var originHost string
	item.proxy.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		originCookie = request.Header.Get("Cookie")
		originHost = request.Host
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{"Set-Cookie": {
				authCookie + "=origin-value; Path=/",
				"application=value; Path=/",
			}},
			Body:    io.NopCloser(strings.NewReader("ok")),
			Request: request,
		}, nil
	})
	request := newRequest("https://example.trycloudflare.com/app")
	request.AddCookie(&http.Cookie{Name: authCookie, Value: runtime.cookieValue(item)})
	request.AddCookie(&http.Cookie{Name: "application", Value: "value"})
	response := recordResponse(runtime, request)

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if strings.Contains(originCookie, authCookie) || !strings.Contains(originCookie, "application=value") {
		t.Fatalf("origin cookie = %q", originCookie)
	}
	if originHost != "example.trycloudflare.com" {
		t.Fatalf("origin host = %q", originHost)
	}
	setCookies := response.Header.Values("Set-Cookie")
	if len(setCookies) != 1 || !strings.HasPrefix(setCookies[0], "application=") {
		t.Fatalf("set-cookie = %#v", setCookies)
	}
}

func TestMissingOrWrongAuthIsUnauthorized(t *testing.T) {
	runtime, _ := testRuntime("secret-token")
	for _, rawURL := range []string{
		"https://example.trycloudflare.com/",
		"https://example.trycloudflare.com/?anycode_auth=wrong",
	} {
		response := recordResponse(runtime, newRequest(rawURL))
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s status = %d", rawURL, response.StatusCode)
		}
	}
}

func testRuntime(auth string) (*Runtime, *entry) {
	target, _ := url.Parse("http://127.0.0.1:4173")
	item := &entry{
		tunnel: domain.Tunnel{
			ID: "tunnel-1", SessionID: "session-1", Port: 4173,
			Status: domain.StatusRunning, URL: "https://example.trycloudflare.com", Hostname: "example.trycloudflare.com",
		},
		routeHost: "tunnel-1.internal", authHash: sha256.Sum256([]byte(auth)),
		proxy: newOriginProxy(target),
	}
	runtime := &Runtime{entries: map[domain.ID]*entry{item.tunnel.ID: item}}
	copy(runtime.cookieKey[:], []byte("01234567890123456789012345678901"))
	return runtime, item
}

func newRequest(rawURL string) *http.Request {
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, nil)
	request.Host = "tunnel-1.internal"
	return request
}

func recordResponse(handler http.Handler, request *http.Request) *http.Response {
	recorder := &responseRecorder{header: http.Header{}}
	handler.ServeHTTP(recorder, request)
	return &http.Response{
		StatusCode: recorder.status, Header: recorder.header,
		Body: io.NopCloser(strings.NewReader(recorder.body.String())), Request: request,
	}
}

type responseRecorder struct {
	header http.Header
	status int
	body   strings.Builder
}

func (r *responseRecorder) Header() http.Header { return r.header }
func (r *responseRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}
func (r *responseRecorder) Write(value []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.body.Write(value)
}

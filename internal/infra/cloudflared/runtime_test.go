package cloudflared

import (
	"context"
	"crypto/sha256"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	domain "github.com/nzlov/anycode/internal/domain/tunnel"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestStartUsesSupportedCloudflaredArguments(t *testing.T) {
	origin, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer origin.Close()

	tempDir := t.TempDir()
	argsPath := filepath.Join(tempDir, "args")
	binPath := filepath.Join(tempDir, "cloudflared")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"$ANYCODE_TEST_CLOUDFLARED_ARGS\"\n" +
		"printf '%s\\n' 'https://argument-test.trycloudflare.com'\n" +
		"trap 'exit 0' TERM INT\n" +
		"while :; do sleep 1; done\n"
	if err := os.WriteFile(binPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANYCODE_TEST_CLOUDFLARED_ARGS", argsPath)

	runtime, err := New(binPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := runtime.CloseAll(ctx); err != nil {
			t.Errorf("close runtime: %v", err)
		}
	})

	started, err := runtime.Start(context.Background(), domain.StartInput{
		Tunnel: domain.Tunnel{
			ID: "tunnel-1", SessionID: "session-1",
			Port: origin.Addr().(*net.TCPAddr).Port,
		},
		Auth: "secret-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.URL != "https://argument-test.trycloudflare.com" {
		t.Fatalf("url = %q", started.URL)
	}
	rawArgs, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Fields(string(rawArgs))
	want := []string{
		"tunnel",
		"--url", runtime.proxyURL,
		"--http-host-header", "tunnel-1.internal",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("arguments = %#v, want %#v", args, want)
	}
}

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

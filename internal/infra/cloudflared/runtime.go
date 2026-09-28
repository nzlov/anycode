package cloudflared

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	domain "github.com/nzlov/anycode/internal/domain/tunnel"
)

const (
	startupTimeout = 30 * time.Second
	stopTimeout    = 5 * time.Second
	authQuery      = "anycode_auth"
	authCookie     = "__Host-anycode_tunnel_auth"
)

var quickTunnelURLPattern = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

type Runtime struct {
	mu            sync.RWMutex
	entries       map[domain.ID]*entry
	bin           string
	proxyServer   *http.Server
	proxyListener net.Listener
	proxyURL      string
	cookieKey     [32]byte
	runtimeHome   string
	closed        bool
}

type entry struct {
	switching    bool // protected by Runtime.mu
	localContext context.Context
	cancelLocal  context.CancelFunc
	tunnel       domain.Tunnel
	routeHost    string
	authHash     [32]byte
	proxy        *httputil.ReverseProxy
	cmd          *exec.Cmd
	done         chan struct{}

	mu      sync.RWMutex
	waitErr error
	stop    bool
}

func New(bin string) (*Runtime, error) {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		bin = "cloudflared"
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen tunnel proxy: %w", err)
	}
	runtimeHome, err := os.MkdirTemp("", "anycode-cloudflared-")
	if err != nil {
		listener.Close()
		return nil, fmt.Errorf("create cloudflared runtime directory: %w", err)
	}
	runtime := &Runtime{
		entries:       map[domain.ID]*entry{},
		bin:           bin,
		proxyListener: listener,
		proxyURL:      "http://" + listener.Addr().String(),
		runtimeHome:   runtimeHome,
	}
	if _, err := rand.Read(runtime.cookieKey[:]); err != nil {
		listener.Close()
		os.RemoveAll(runtimeHome)
		return nil, fmt.Errorf("generate tunnel cookie key: %w", err)
	}
	runtime.proxyServer = &http.Server{
		Handler:           runtime,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		_ = runtime.proxyServer.Serve(listener)
	}()
	return runtime, nil
}

func (r *Runtime) Start(ctx context.Context, input domain.StartInput) (domain.Tunnel, error) {
	return r.start(ctx, input, input.Tunnel.ID)
}

func (r *Runtime) start(ctx context.Context, input domain.StartInput, key domain.ID) (domain.Tunnel, error) {
	if r == nil {
		return domain.Tunnel{}, errors.New("cloudflared runtime is nil")
	}
	if input.Tunnel.ID == "" || input.Tunnel.SessionID == "" {
		return domain.Tunnel{}, errors.New("tunnel id and session id are required")
	}
	if input.Auth == "" {
		return domain.Tunnel{}, errors.New("tunnel auth is required")
	}
	if input.Tunnel.Mode == "" {
		input.Tunnel.Mode = domain.ModeCF
	}
	if !input.Tunnel.Mode.Valid() {
		return domain.Tunnel{}, errors.New("invalid tunnel mode")
	}
	if err := probeOrigin(ctx, input.Tunnel.Port); err != nil {
		return domain.Tunnel{}, err
	}
	target, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(input.Tunnel.Port))
	routeHost := string(key) + ".internal"
	item := &entry{
		tunnel:    input.Tunnel,
		routeHost: routeHost,
		authHash:  sha256.Sum256([]byte(input.Auth)),
		proxy:     newOriginProxy(target),
		done:      make(chan struct{}),
	}

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return domain.Tunnel{}, errors.New("cloudflared runtime is closed")
	}
	if key != input.Tunnel.ID && r.entries[input.Tunnel.ID] == nil {
		r.mu.Unlock()
		return domain.Tunnel{}, errors.New("tunnel was closed during startup")
	}
	if _, exists := r.entries[key]; exists {
		r.mu.Unlock()
		return domain.Tunnel{}, fmt.Errorf("tunnel %s already exists", input.Tunnel.ID)
	}
	if input.Tunnel.Mode == domain.ModeLocal {
		item.localContext, item.cancelLocal = context.WithCancel(context.Background())
		item.tunnel.URL = "/tunnel/" + url.PathEscape(string(input.Tunnel.ID)) + "/"
		item.tunnel.AccessURL = item.tunnel.URL + "?anycode_auth=" + url.QueryEscape(input.Auth)
		item.tunnel.Status = domain.StatusRunning
	}
	r.entries[key] = item
	r.mu.Unlock()

	if input.Tunnel.Mode == domain.ModeLocal {
		return item.tunnel, nil
	}

	cmd := exec.Command(r.bin,
		"tunnel",
		"--url", r.proxyURL,
		"--http-host-header", routeHost,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Env = replaceEnv(os.Environ(), "HOME", r.runtimeHome)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		r.remove(key, item)
		return domain.Tunnel{}, fmt.Errorf("open cloudflared stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		r.remove(key, item)
		return domain.Tunnel{}, fmt.Errorf("open cloudflared stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		r.remove(key, item)
		return domain.Tunnel{}, fmt.Errorf("start cloudflared: %w", err)
	}
	item.mu.Lock()
	item.cmd = cmd
	stopRequested := item.stop
	item.mu.Unlock()
	lines := make(chan string, 32)
	scanOutput(stdout, lines)
	scanOutput(stderr, lines)
	go r.wait(key, item, cmd)
	if stopRequested {
		_ = stopProcess(context.Background(), item)
		return domain.Tunnel{}, errors.New("tunnel was closed during startup")
	}

	timer := time.NewTimer(startupTimeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = r.closeEntry(context.Background(), key, item)
			return domain.Tunnel{}, fmt.Errorf("start tunnel: %w", ctx.Err())
		case <-timer.C:
			_ = r.closeEntry(context.Background(), key, item)
			return domain.Tunnel{}, errors.New("cloudflared did not report a Quick Tunnel URL")
		case <-item.done:
			r.remove(key, item)
			return domain.Tunnel{}, fmt.Errorf("cloudflared exited before startup: %w", item.err())
		case line := <-lines:
			publicURL := quickTunnelURLPattern.FindString(line)
			if publicURL == "" {
				continue
			}
			parsed, err := url.Parse(publicURL)
			if err != nil || parsed.Hostname() == "" {
				continue
			}
			r.mu.Lock()
			current, exists := r.entries[key]
			if exists && current == item {
				item.tunnel.URL = publicURL
				item.tunnel.Hostname = parsed.Hostname()
				item.tunnel.AccessURL = strings.TrimRight(publicURL, "/") + "/?anycode_auth=" + url.QueryEscape(input.Auth)
				item.tunnel.Status = domain.StatusRunning
			}
			r.mu.Unlock()
			if !exists || current != item {
				_ = stopProcess(context.Background(), item)
				return domain.Tunnel{}, errors.New("tunnel was closed during startup")
			}
			return item.tunnel, nil
		}
	}
}

// SwitchMode starts the replacement before retiring the current route. The
// temporary registry key keeps session cleanup effective during CF startup.
func (r *Runtime) SwitchMode(ctx context.Context, id domain.ID, mode domain.Mode) (domain.Tunnel, error) {
	if !mode.Valid() {
		return domain.Tunnel{}, errors.New("invalid tunnel mode")
	}
	r.mu.Lock()
	old := r.entries[id]
	if old == nil || old.tunnel.Status != domain.StatusRunning {
		r.mu.Unlock()
		return domain.Tunnel{}, errors.New("running tunnel not found")
	}
	if old.switching {
		r.mu.Unlock()
		return domain.Tunnel{}, errors.New("tunnel mode switch is already in progress")
	}
	if old.tunnel.Mode == mode {
		result := old.tunnel
		r.mu.Unlock()
		return result, nil
	}
	old.switching = true
	next := old.tunnel
	r.mu.Unlock()
	defer func() { r.mu.Lock(); old.switching = false; r.mu.Unlock() }()
	token := make([]byte, 12)
	if _, err := rand.Read(token); err != nil {
		return domain.Tunnel{}, err
	}
	key := domain.ID(string(id) + "-switch-" + strings.ToLower(base64.RawURLEncoding.EncodeToString(token)))
	access, err := url.Parse(next.AccessURL)
	if err != nil {
		return domain.Tunnel{}, err
	}
	next.Mode, next.Status = mode, domain.StatusStarting
	next.URL, next.AccessURL, next.Hostname = "", "", ""
	started, err := r.start(ctx, domain.StartInput{Tunnel: next, Auth: access.Query().Get(authQuery)}, key)
	if err != nil {
		return domain.Tunnel{}, err
	}
	r.mu.Lock()
	replacement := r.entries[key]
	if ctx.Err() != nil || r.entries[id] != old || replacement == nil {
		r.mu.Unlock()
		if replacement != nil {
			_ = r.closeEntry(context.Background(), key, replacement)
		}
		return domain.Tunnel{}, errors.New("tunnel was closed or switch canceled during startup")
	}
	delete(r.entries, key)
	r.entries[id] = replacement
	r.mu.Unlock()
	_ = stopProcess(context.Background(), old)
	return started, nil
}

func (r *Runtime) List(ctx context.Context) ([]domain.Tunnel, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]domain.Tunnel, 0, len(r.entries))
	for key, item := range r.entries {
		if key != item.tunnel.ID {
			continue
		}
		result = append(result, item.tunnel)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, nil
}

func (r *Runtime) Close(ctx context.Context, id domain.ID) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	var items []*entry
	for key, item := range r.entries {
		if item.tunnel.ID == id {
			delete(r.entries, key)
			items = append(items, item)
		}
	}
	r.mu.Unlock()
	var result error
	for _, item := range items {
		result = errors.Join(result, stopProcess(ctx, item))
	}
	return result
}

func (r *Runtime) CloseSession(ctx context.Context, sessionID domain.SessionID) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	items := make([]*entry, 0)
	for id, item := range r.entries {
		if item.tunnel.SessionID == sessionID {
			delete(r.entries, id)
			items = append(items, item)
		}
	}
	r.mu.Unlock()
	var result error
	for _, item := range items {
		result = errors.Join(result, stopProcess(ctx, item))
	}
	return result
}

func (r *Runtime) CloseAll(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	r.closed = true
	items := make([]*entry, 0, len(r.entries))
	for id, item := range r.entries {
		delete(r.entries, id)
		items = append(items, item)
	}
	r.mu.Unlock()
	var result error
	for _, item := range items {
		result = errors.Join(result, stopProcess(ctx, item))
	}
	if r.proxyServer != nil {
		result = errors.Join(result, r.proxyServer.Shutdown(ctx))
	}
	if r.runtimeHome != "" {
		result = errors.Join(result, os.RemoveAll(r.runtimeHome))
	}
	return result
}

func (r *Runtime) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	routeHost := strings.ToLower(request.Host)
	if host, _, err := net.SplitHostPort(routeHost); err == nil {
		routeHost = host
	}
	r.mu.RLock()
	var item *entry
	for _, candidate := range r.entries {
		if candidate.tunnel.Mode != domain.ModeLocal && candidate.routeHost == routeHost && candidate.tunnel.Status == domain.StatusRunning {
			item = candidate
			break
		}
	}
	r.mu.RUnlock()
	if item == nil {
		http.NotFound(w, request)
		return
	}

	if provided := request.URL.Query().Get(authQuery); provided != "" {
		if !item.validAuth(provided) {
			writeUnauthorized(w)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name: authCookie, Value: r.cookieValue(item), Path: "/", HttpOnly: true,
			Secure: true, SameSite: http.SameSiteLaxMode,
		})
		cleanURL := *request.URL
		query := cleanURL.Query()
		query.Del(authQuery)
		cleanURL.RawQuery = query.Encode()
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		http.Redirect(w, request, cleanURL.RequestURI(), http.StatusSeeOther)
		return
	}
	cookie, err := request.Cookie(authCookie)
	if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(r.cookieValue(item))) != 1 {
		writeUnauthorized(w)
		return
	}
	removeCookie(request, authCookie)
	request.Host = item.tunnel.Hostname
	request.Header.Set("X-Forwarded-Host", item.tunnel.Hostname)
	item.proxy.ServeHTTP(w, request)
}

// ServeLocalHTTP exposes local previews under the AnyCode URL. Each preview
// has its own credential and cookie path; the AnyCode API credential is never forwarded.
func (r *Runtime) ServeLocalHTTP(w http.ResponseWriter, request *http.Request) {
	remainder := strings.TrimPrefix(request.URL.Path, "/tunnel/")
	id, _, found := strings.Cut(remainder, "/")
	if !found || id == "" {
		http.NotFound(w, request)
		return
	}
	r.mu.RLock()
	item := r.entries[domain.ID(id)]
	if item != nil && (item.tunnel.Mode != domain.ModeLocal || item.tunnel.Status != domain.StatusRunning) {
		item = nil
	}
	r.mu.RUnlock()
	if item == nil {
		http.NotFound(w, request)
		return
	}
	prefix := "/tunnel/" + id + "/"
	cookieName := "anycode_tunnel_" + id
	if provided := request.URL.Query().Get(authQuery); provided != "" {
		if !item.validAuth(provided) {
			writeUnauthorized(w)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: r.cookieValue(item), Path: prefix,
			HttpOnly: true, Secure: request.TLS != nil || request.Header.Get("X-Forwarded-Proto") == "https", SameSite: http.SameSiteLaxMode})
		cleanURL := *request.URL
		query := cleanURL.Query()
		query.Del(authQuery)
		cleanURL.RawQuery = query.Encode()
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		http.Redirect(w, request, cleanURL.RequestURI(), http.StatusSeeOther)
		return
	}
	cookie, err := request.Cookie(cookieName)
	if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(r.cookieValue(item))) != 1 {
		writeUnauthorized(w)
		return
	}
	ctx, cancel := context.WithCancel(request.Context())
	stop := context.AfterFunc(item.localContext, cancel)
	defer stop()
	defer cancel()
	request = request.Clone(ctx)
	request.Header.Del("Authorization")
	for _, cookie := range request.Cookies() {
		if strings.HasPrefix(cookie.Name, "anycode_tunnel_") || cookie.Name == authCookie {
			removeCookie(request, cookie.Name)
		}
	}
	request.Header.Del("X-Api-Key")
	request.URL.Path = "/" + strings.TrimPrefix(request.URL.Path, prefix)
	request.URL.RawPath = ""
	request.Header.Set("X-Forwarded-Host", request.Host)
	request.Header.Set("X-Forwarded-Prefix", strings.TrimSuffix(prefix, "/"))
	// Keep redirects and application cookies within this preview's route.
	proxy := *item.proxy
	director := proxy.Director
	proto := "http"
	if request.TLS != nil || request.Header.Get("X-Forwarded-Proto") == "https" {
		proto = "https"
	}
	proxy.Director = func(req *http.Request) { director(req); req.Header.Set("X-Forwarded-Proto", proto) }
	proxy.ModifyResponse = func(response *http.Response) error {
		if location, err := url.Parse(response.Header.Get("Location")); err == nil && location.Host == "" && strings.HasPrefix(location.Path, "/") {
			location.Path = prefix + strings.TrimPrefix(location.Path, "/")
			response.Header.Set("Location", location.String())
		}
		cookies := response.Cookies()
		response.Header.Del("Set-Cookie")
		for _, cookie := range cookies {
			if strings.HasPrefix(cookie.Name, "anycode_tunnel_") || cookie.Name == authCookie {
				continue
			}
			cookie.Domain = ""
			cookie.Path = prefix + strings.TrimPrefix(cookie.Path, "/")
			response.Header.Add("Set-Cookie", cookie.String())
		}
		response.Header.Del("Service-Worker-Allowed")
		response.Header.Set("Referrer-Policy", "no-referrer")
		return nil
	}
	proxy.ServeHTTP(w, request)
}

func (r *Runtime) cookieValue(item *entry) string {
	mac := hmac.New(sha256.New, r.cookieKey[:])
	mac.Write([]byte(item.routeHost))
	mac.Write(item.authHash[:])
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (r *Runtime) wait(id domain.ID, item *entry, cmd *exec.Cmd) {
	err := cmd.Wait()
	item.mu.Lock()
	item.waitErr = err
	item.mu.Unlock()
	close(item.done)
	r.remove(id, item)
}

func (r *Runtime) remove(id domain.ID, item *entry) {
	r.mu.Lock()
	if r.entries[id] == item {
		delete(r.entries, id)
	}
	if r.entries[item.tunnel.ID] == item {
		delete(r.entries, item.tunnel.ID)
	}
	r.mu.Unlock()
}

func (r *Runtime) closeEntry(ctx context.Context, id domain.ID, item *entry) error {
	r.remove(id, item)
	return stopProcess(ctx, item)
}

func (e *entry) err() error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.waitErr == nil {
		return errors.New("process exited")
	}
	return e.waitErr
}

func (e *entry) validAuth(value string) bool {
	hash := sha256.Sum256([]byte(value))
	return subtle.ConstantTimeCompare(hash[:], e.authHash[:]) == 1
}

func probeOrigin(ctx context.Context, port int) error {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp4", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return fmt.Errorf("test program is not listening on 127.0.0.1:%d: %w", port, err)
	}
	return connection.Close()
}

func scanOutput(reader interface{ Read([]byte) (int, error) }, lines chan<- string) {
	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			default:
			}
		}
	}()
}

func stopProcess(ctx context.Context, item *entry) error {
	if item == nil {
		return nil
	}
	if item.cancelLocal != nil {
		item.cancelLocal()
	}
	item.mu.Lock()
	cmd := item.cmd
	if cmd == nil || cmd.Process == nil {
		item.stop = true
		item.mu.Unlock()
		return nil
	}
	item.mu.Unlock()
	select {
	case <-item.done:
		return nil
	default:
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	timer := time.NewTimer(stopTimeout)
	defer timer.Stop()
	select {
	case <-item.done:
		return nil
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return ctx.Err()
	case <-timer.C:
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		select {
		case <-item.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func newOriginProxy(target *url.URL) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(request *http.Request) {
		director(request)
		request.Header.Set("X-Forwarded-Proto", "https")
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		cookies := response.Header.Values("Set-Cookie")
		response.Header.Del("Set-Cookie")
		for _, cookie := range cookies {
			if !strings.HasPrefix(cookie, authCookie+"=") {
				response.Header.Add("Set-Cookie", cookie)
			}
		}
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		http.Error(w, "Tunnel origin unavailable: "+html.EscapeString(err.Error()), http.StatusBadGateway)
	}
	return proxy
}

func removeCookie(request *http.Request, name string) {
	cookies := request.Cookies()
	request.Header.Del("Cookie")
	for _, cookie := range cookies {
		if cookie.Name != name {
			request.AddCookie(cookie)
		}
	}
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte("Tunnel authentication required. Open the complete AnyCode tunnel URL."))
}

func replaceEnv(values []string, key string, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(values)+1)
	for _, item := range values {
		if !strings.HasPrefix(item, prefix) {
			result = append(result, item)
		}
	}
	return append(result, prefix+value)
}

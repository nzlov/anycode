package webpush

import (
	"net/http"
	"testing"
)

func TestHTTPClientForProxyConfiguresSupportedProxy(t *testing.T) {
	client, err := httpClientForProxy("socks5://proxy.example:1080")
	if err != nil {
		t.Fatalf("httpClientForProxy() error = %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T", client.Transport)
	}
	request, err := http.NewRequest(http.MethodPost, "https://push.example/message", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	got, err := transport.Proxy(request)
	if err != nil || got.String() != "socks5://proxy.example:1080" {
		t.Fatalf("proxy = %v, error = %v", got, err)
	}
}

func TestHTTPClientForProxyLeavesEmptyProxyUnconfigured(t *testing.T) {
	client, err := httpClientForProxy("")
	if err != nil || client != nil {
		t.Fatalf("httpClientForProxy(\"\") = %#v, %v", client, err)
	}
}

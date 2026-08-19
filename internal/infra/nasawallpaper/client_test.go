package nasawallpaper

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestOpenFetchesNASAImageOfTheDayAsset(t *testing.T) {
	requests := 0
	client := New()
	client.httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.String() != imageURL {
			t.Fatalf("unexpected request URL: %s", request.URL)
		}
		return response(request, http.StatusOK, "image/jpeg", "jpeg-data"), nil
	})}

	wallpaper, err := client.Open(context.Background())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer wallpaper.Reader.Close()
	data, err := io.ReadAll(wallpaper.Reader)
	if err != nil || requests != 1 || wallpaper.MimeType != "image/jpeg" || string(data) != "jpeg-data" {
		t.Fatalf("wallpaper = requests:%d type:%q data:%q error:%v", requests, wallpaper.MimeType, data, err)
	}
}

func TestOpenRejectsUnsupportedImageContentType(t *testing.T) {
	client := New()
	client.httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return response(request, http.StatusOK, "image/svg+xml", "<svg/>"), nil
	})}

	if _, err := client.Open(context.Background()); err == nil || !strings.Contains(err.Error(), "unsupported content type") {
		t.Fatalf("Open() error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func response(request *http.Request, status int, contentType string, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

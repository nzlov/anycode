package nasawallpaper

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestResolveImageURLPrefersGalleryImageAndFallsBackToMetadata(t *testing.T) {
	baseURL, err := url.Parse(pageURL)
	if err != nil {
		t.Fatal(err)
	}
	page := `<meta content="https://www.nasa.gov/fallback.jpg" property="og:image">
		<div class="hds-gallery-item-single hds-gallery-image"><a><img alt="x" src="/daily.png"></a></div>`
	if got := resolveImageURL([]byte(page), baseURL); got == nil || got.String() != "https://www.nasa.gov/daily.png" {
		t.Fatalf("resolveImageURL() = %v", got)
	}

	fallback := `<meta content='/fallback.jpg' name='twitter:image'>`
	if got := resolveImageURL([]byte(fallback), baseURL); got == nil || got.String() != "https://www.nasa.gov/fallback.jpg" {
		t.Fatalf("resolveImageURL() fallback = %v", got)
	}
}

func TestResolveImageURLRejectsNonNASAAndInsecureURLs(t *testing.T) {
	baseURL, _ := url.Parse(pageURL)
	for _, page := range []string{
		`<div class="hds-gallery-image"><img src="https://example.com/image.jpg"></div>`,
		`<meta property="og:image" content="http://www.nasa.gov/image.jpg">`,
	} {
		if got := resolveImageURL([]byte(page), baseURL); got != nil {
			t.Fatalf("resolveImageURL(%q) = %v", page, got)
		}
	}
}

func TestOpenFetchesResolvedNASAImage(t *testing.T) {
	baseURL, _ := url.Parse(pageURL)
	client := &Client{
		pageURL: baseURL,
		httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			switch request.URL.Path {
			case "/image-of-the-day/":
				return response(request, http.StatusOK, "text/html", `<div class="hds-gallery-image"><img src="/daily.png"></div>`), nil
			case "/daily.png":
				return response(request, http.StatusOK, "image/png", "png-data"), nil
			default:
				t.Fatalf("unexpected request URL: %s", request.URL)
				return nil, nil
			}
		})},
	}

	wallpaper, err := client.Open(context.Background())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer wallpaper.Reader.Close()
	data, err := io.ReadAll(wallpaper.Reader)
	if err != nil || wallpaper.MimeType != "image/png" || string(data) != "png-data" {
		t.Fatalf("wallpaper = type:%q data:%q error:%v", wallpaper.MimeType, data, err)
	}
}

func TestOpenRejectsUnsupportedImageContentType(t *testing.T) {
	baseURL, _ := url.Parse(pageURL)
	client := &Client{
		pageURL: baseURL,
		httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path == "/image-of-the-day/" {
				return response(request, http.StatusOK, "text/html", `<meta property="og:image" content="/daily.svg">`), nil
			}
			return response(request, http.StatusOK, "image/svg+xml", "<svg/>"), nil
		})},
	}

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

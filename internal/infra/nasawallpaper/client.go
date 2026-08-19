package nasawallpaper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	settingdomain "github.com/nzlov/anycode/internal/domain/setting"
)

const (
	imageURL      = "https://svs.gsfc.nasa.gov/vis/a030000/a031300/a031364/image_of_the_day_720p.jpg"
	maxImageBytes = 40 << 20
)

type Client struct {
	httpClient *http.Client
}

func New() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) Open(ctx context.Context) (settingdomain.RemoteWallpaper, error) {
	if c == nil || c.httpClient == nil {
		return settingdomain.RemoteWallpaper{}, fmt.Errorf("NASA wallpaper client is unavailable")
	}

	image, err := c.fetch(ctx, imageURL, maxImageBytes, "NASA image")
	if err != nil {
		return settingdomain.RemoteWallpaper{}, err
	}
	mimeType, _, err := mime.ParseMediaType(image.contentType)
	if err != nil || !validImageMimeType(mimeType) {
		return settingdomain.RemoteWallpaper{}, fmt.Errorf("NASA image returned unsupported content type %q", image.contentType)
	}
	return settingdomain.RemoteWallpaper{
		MimeType: strings.ToLower(mimeType),
		Reader:   io.NopCloser(bytes.NewReader(image.data)),
	}, nil
}

type responseData struct {
	data        []byte
	contentType string
}

func (c *Client) fetch(ctx context.Context, target string, limit int64, label string) (responseData, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return responseData{}, fmt.Errorf("create %s request: %w", label, err)
	}
	request.Header.Set("User-Agent", "AnyCode NASA wallpaper")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return responseData{}, fmt.Errorf("fetch %s: %w", label, err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return responseData{}, fmt.Errorf("fetch %s: HTTP %d", label, response.StatusCode)
	}
	if response.Request == nil || !validNASAURL(response.Request.URL) {
		return responseData{}, fmt.Errorf("fetch %s: redirected outside nasa.gov", label)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return responseData{}, fmt.Errorf("read %s: %w", label, err)
	}
	if int64(len(data)) > limit {
		return responseData{}, fmt.Errorf("read %s: response exceeds %d bytes", label, limit)
	}
	return responseData{data: data, contentType: response.Header.Get("Content-Type")}, nil
}

func validNASAURL(target *url.URL) bool {
	if target == nil || target.Scheme != "https" || target.User != nil {
		return false
	}
	host := strings.ToLower(target.Hostname())
	return host == "nasa.gov" || strings.HasSuffix(host, ".nasa.gov")
}

func validImageMimeType(value string) bool {
	switch strings.ToLower(value) {
	case "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

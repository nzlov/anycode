package nasawallpaper

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	settingdomain "github.com/nzlov/anycode/internal/domain/setting"
)

const (
	pageURL       = "https://www.nasa.gov/image-of-the-day/"
	maxPageBytes  = 2 << 20
	maxImageBytes = 40 << 20
)

var (
	imageTagPattern         = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	metaTagPattern          = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	attributePattern        = regexp.MustCompile(`(?is)([a-z][a-z0-9_:.\-]*)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	galleryClassMarker      = regexp.MustCompile(`(?i)\bhds-gallery-image\b`)
	galleryContainerPattern = regexp.MustCompile(`(?is)class\s*=\s*(?:"[^"]*\bhds-gallery-image\b[^"]*"|'[^']*\bhds-gallery-image\b[^']*')`)
)

type Client struct {
	httpClient *http.Client
	pageURL    *url.URL
}

func New() *Client {
	parsedPageURL, _ := url.Parse(pageURL)
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		pageURL:    parsedPageURL,
	}
}

func (c *Client) Open(ctx context.Context) (settingdomain.RemoteWallpaper, error) {
	if c == nil || c.httpClient == nil || c.pageURL == nil {
		return settingdomain.RemoteWallpaper{}, fmt.Errorf("NASA wallpaper client is unavailable")
	}

	page, err := c.fetch(ctx, c.pageURL.String(), maxPageBytes, "NASA page")
	if err != nil {
		return settingdomain.RemoteWallpaper{}, err
	}
	imageURL := resolveImageURL(page.data, c.pageURL)
	if imageURL == nil {
		return settingdomain.RemoteWallpaper{}, fmt.Errorf("NASA page did not include an image URL")
	}

	image, err := c.fetch(ctx, imageURL.String(), maxImageBytes, "NASA image")
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

func resolveImageURL(page []byte, baseURL *url.URL) *url.URL {
	markup := string(page)
	for _, tag := range imageTagPattern.FindAllString(markup, -1) {
		attributes := parseAttributes(tag)
		if galleryClassMarker.MatchString(attributes["class"]) {
			if resolved := absoluteNASAURL(attributes["src"], baseURL); resolved != nil {
				return resolved
			}
		}
	}
	if marker := galleryContainerPattern.FindStringIndex(markup); marker != nil {
		if tag := imageTagPattern.FindString(markup[marker[1]:]); tag != "" {
			if resolved := absoluteNASAURL(parseAttributes(tag)["src"], baseURL); resolved != nil {
				return resolved
			}
		}
	}
	for _, tag := range metaTagPattern.FindAllString(markup, -1) {
		attributes := parseAttributes(tag)
		key := strings.ToLower(attributes["property"])
		if key == "" {
			key = strings.ToLower(attributes["name"])
		}
		if key == "og:image" || key == "twitter:image" {
			if resolved := absoluteNASAURL(attributes["content"], baseURL); resolved != nil {
				return resolved
			}
		}
	}
	return nil
}

func parseAttributes(tag string) map[string]string {
	attributes := make(map[string]string)
	for _, match := range attributePattern.FindAllStringSubmatch(tag, -1) {
		value := match[2]
		if value == "" {
			value = match[3]
		}
		attributes[strings.ToLower(match[1])] = html.UnescapeString(value)
	}
	return attributes
}

func absoluteNASAURL(value string, baseURL *url.URL) *url.URL {
	if strings.TrimSpace(value) == "" || baseURL == nil {
		return nil
	}
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	resolved := baseURL.ResolveReference(parsed)
	if !validNASAURL(resolved) {
		return nil
	}
	return resolved
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

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

type Scope struct {
	Kind string
	ID   string
}
type Definition struct {
	Transport string            `json:"transport"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}
type Entry struct {
	Scope      Scope
	Name       string
	Definition *Definition
	Enabled    *bool
}
type Service struct {
	Name       string
	Definition Definition
	Enabled    bool
	Source     string
	Overridden bool
}
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}
type Content struct {
	Type     string
	Text     string
	Data     []byte
	MIMEType string
}
type Result struct {
	IsError bool
	Content []Content
}
type Repository interface {
	List(context.Context, Scope) ([]Entry, error)
	Save(context.Context, Entry) error
	Delete(context.Context, Scope, string) error
}
type Runtime interface {
	Discover(context.Context, string, string, Definition, string) ([]Tool, string, error)
	Call(context.Context, string, string, Definition, string, string, json.RawMessage) (Result, error)
}

var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func ValidName(name string) bool { return namePattern.MatchString(name) }
func (d Definition) Validate() error {
	switch d.Transport {
	case "stdio":
		if strings.TrimSpace(d.Command) == "" || strings.ContainsRune(d.Command, 0) || d.URL != "" || len(d.Headers) > 0 {
			return errors.New("stdio 服务需要命令，不能设置 HTTP 地址或请求头")
		}
	case "http":
		u, e := url.Parse(d.URL)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
			return errors.New("请输入有效的 HTTP/HTTPS MCP 地址")
		}
		if d.Command != "" || len(d.Args) > 0 || len(d.Env) > 0 {
			return errors.New("HTTP 服务不能设置命令、参数或环境变量")
		}
	default:
		return errors.New("不支持的 MCP 连接方式")
	}
	for k, v := range d.Env {
		if k == "" || strings.ContainsAny(k, "=\x00") || strings.ContainsRune(v, 0) {
			return errors.New("环境变量格式无效")
		}
	}
	for k, v := range d.Headers {
		if strings.TrimSpace(k) == "" || strings.ContainsAny(k+v, "\r\n\x00") {
			return errors.New("HTTP 请求头格式无效")
		}
	}
	return nil
}

// Resolve applies complete definitions and then switches, in scope order.
func Resolve(layers ...[]Entry) []Service {
	services := map[string]Service{}
	for i, entries := range layers {
		for _, e := range entries {
			s, exists := services[e.Name]
			if e.Definition != nil {
				s = Service{Name: e.Name, Definition: *e.Definition, Enabled: true, Source: e.Scope.Kind}
				exists = true
			}
			if !exists {
				continue
			}
			if e.Enabled != nil {
				s.Enabled = *e.Enabled
			}
			if i == len(layers)-1 {
				s.Overridden = true
			}
			services[e.Name] = s
		}
	}
	result := make([]Service, 0, len(services))
	for _, s := range services {
		result = append(result, s)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

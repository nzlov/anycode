package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"maps"
	"reflect"
	"strings"
	"sync"

	"github.com/nzlov/anycode/internal/application/apperror"
	domain "github.com/nzlov/anycode/internal/domain/mcp"
	process "github.com/nzlov/anycode/internal/domain/process"
	project "github.com/nzlov/anycode/internal/domain/project"
	session "github.com/nzlov/anycode/internal/domain/session"
)

const SecretMask = "••••••••"

type ProjectFinder interface {
	Find(context.Context, project.ID) (project.Project, error)
}
type SessionFinder interface {
	Find(context.Context, session.ID) (session.Session, error)
}
type Service struct {
	repo     domain.Repository
	runtime  domain.Runtime
	projects ProjectFinder
	sessions SessionFinder
	mu       sync.Mutex
}
type View struct {
	Name       string
	Enabled    bool
	Source     string
	Overridden bool
	Definition *domain.Definition
}

func New(repo domain.Repository, runtime domain.Runtime, projects ProjectFinder, sessions SessionFinder) *Service {
	return &Service{repo: repo, runtime: runtime, projects: projects, sessions: sessions}
}
func (s *Service) resolve(ctx context.Context, scope domain.Scope) ([]domain.Service, string, error) {
	scopes := []domain.Scope{{Kind: "global"}}
	workdir := ""
	switch scope.Kind {
	case "global":
		if scope.ID != "" {
			return nil, "", apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "全局作用域不能指定 ID")
		}
	case "project":
		p, e := s.projects.Find(ctx, project.ID(scope.ID))
		if e != nil {
			return nil, "", e
		}
		workdir = p.Path.Value
		scopes = append(scopes, scope)
	case "session":
		card, e := s.sessions.Find(ctx, session.ID(scope.ID))
		if e != nil {
			return nil, "", e
		}
		if card.Status == session.StatusClosed {
			return nil, "", apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "卡片已关闭")
		}
		workdir = card.WorktreePath
		p, e := s.projects.Find(ctx, project.ID(card.ProjectID))
		if e != nil {
			return nil, "", e
		}
		if workdir == "" {
			workdir = p.Path.Value
		}
		scopes = append(scopes, domain.Scope{Kind: "project", ID: string(card.ProjectID)}, scope)
	default:
		return nil, "", apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "无效的 MCP 作用域")
	}
	layers := make([][]domain.Entry, 0, len(scopes))
	for _, v := range scopes {
		entries, e := s.repo.List(ctx, v)
		if e != nil {
			return nil, "", e
		}
		layers = append(layers, entries)
	}
	return domain.Resolve(layers...), workdir, nil
}
func (s *Service) List(ctx context.Context, scope domain.Scope) ([]View, error) {
	services, _, err := s.resolve(ctx, scope)
	if err != nil {
		return nil, err
	}
	out := make([]View, 0, len(services))
	for _, v := range services {
		view := View{Name: v.Name, Enabled: v.Enabled, Source: v.Source, Overridden: v.Overridden}
		if scope.Kind != "session" {
			d := v.Definition
			d.Env = masked(d.Env)
			d.Headers = masked(d.Headers)
			view.Definition = &d
		}
		out = append(out, view)
	}
	return out, nil
}
func masked(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	out := map[string]string{}
	for k := range values {
		out[k] = SecretMask
	}
	return out
}
func (s *Service) Save(ctx context.Context, scope domain.Scope, name string, d *domain.Definition, enabled *bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !domain.ValidName(name) {
		return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "服务名称须为 1–64 位字母、数字、下划线或短横线")
	}
	effective, _, err := s.resolve(ctx, scope)
	if err != nil {
		return err
	}
	if scope.Kind == "session" && d != nil {
		return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "卡片只能开关 MCP 服务")
	}
	local, err := s.repo.List(ctx, scope)
	if err != nil {
		return err
	}
	entry := domain.Entry{Scope: scope, Name: name}
	for _, e := range local {
		if e.Name == name {
			entry = e
			break
		}
	}
	exists := false
	inherited := domain.Definition{}
	for _, v := range effective {
		if v.Name == name {
			exists = true
			inherited = v.Definition
			break
		}
	}
	if d != nil {
		copy := *d
		copy.Env = maps.Clone(d.Env)
		copy.Headers = maps.Clone(d.Headers)
		d = &copy
		// Preserve explicitly masked credentials when editing a definition.
		for _, pair := range []struct{ values, old map[string]string }{{d.Env, inherited.Env}, {d.Headers, inherited.Headers}} {
			for k, v := range pair.values {
				if v == SecretMask {
					old, ok := pair.old[k]
					if !ok {
						return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "请填写新凭据")
					}
					pair.values[k] = old
				}
			}
		}
		if err := d.Validate(); err != nil {
			return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, err.Error())
		}
		entry.Definition = d
	} else if !exists {
		return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "MCP 服务不存在")
	}
	if enabled != nil {
		entry.Enabled = enabled
	}
	if entry.Definition == nil && scope.Kind == "global" {
		return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "需要 MCP 连接配置")
	}
	return s.repo.Save(ctx, entry)
}
func (s *Service) Delete(ctx context.Context, scope domain.Scope, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, _, err := s.resolve(ctx, scope); err != nil {
		return err
	}
	return s.repo.Delete(ctx, scope, name)
}
func (s *Service) Check(ctx context.Context, scope domain.Scope, name string) (int, error) {
	if scope.Kind == "session" {
		return 0, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "请在全局或项目设置中检测连接")
	}
	services, workdir, err := s.resolve(ctx, scope)
	if err != nil {
		return 0, err
	}
	for _, v := range services {
		if v.Name == name {
			tools, _, err := s.runtime.Discover(ctx, "check/"+scope.Kind+"/"+scope.ID, name, v.Definition, workdir)
			if err != nil {
				return 0, apperror.Wrap(err, apperror.CodeValidationFailed, apperror.CategoryValidationError, "连接检测失败，请检查服务配置和认证信息")
			}
			return len(tools), nil
		}
	}
	return 0, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "MCP 服务不存在")
}
func (s *Service) HandleDynamicTool(ctx context.Context, call process.DynamicToolCall) (process.DynamicToolResult, error) {
	var input struct {
		Server    string          `json:"server"`
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(call.Arguments, &input); err != nil {
		return process.DynamicToolResult{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "MCP 参数无效")
	}
	services, workdir, err := s.resolve(ctx, domain.Scope{Kind: "session", ID: string(call.SessionID)})
	if err != nil {
		return process.DynamicToolResult{}, err
	}
	if call.Tool == "mcp_discover" {
		type found struct {
			Server       string        `json:"server"`
			Instructions string        `json:"instructions,omitempty"`
			Tools        []domain.Tool `json:"tools"`
			Error        string        `json:"error,omitempty"`
		}
		out := []found{}
		for _, v := range services {
			if !v.Enabled || (input.Server != "" && input.Server != v.Name) {
				continue
			}
			tools, instructions, e := s.runtime.Discover(ctx, string(call.SessionID), v.Name, v.Definition, workdir)
			item := found{Server: v.Name, Tools: tools, Instructions: instructions}
			if e != nil {
				item.Error = e.Error()
			}
			out = append(out, item)
		}
		// Recheck switches after network I/O so a concurrent disable cannot publish its catalog.
		current, _, err := s.resolve(ctx, domain.Scope{Kind: "session", ID: string(call.SessionID)})
		if err != nil {
			return process.DynamicToolResult{}, err
		}
		allowed := map[string]domain.Definition{}
		for _, v := range current {
			if v.Enabled {
				allowed[v.Name] = v.Definition
			}
		}
		filtered := out[:0]
		for _, v := range out {
			if definition, ok := allowed[v.Server]; ok {
				for _, original := range services {
					if original.Name == v.Server && reflect.DeepEqual(original.Definition, definition) {
						filtered = append(filtered, v)
						break
					}
				}
			}
		}
		data, err := json.Marshal(filtered)
		return process.DynamicToolResult{Success: err == nil, Content: []process.DynamicToolContent{{Type: "inputText", Text: string(data)}}}, err
	}
	if call.Tool != "mcp_call" {
		return process.DynamicToolResult{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "未知 MCP 动态工具")
	}
	for _, v := range services {
		if v.Enabled && v.Name == input.Server {
			if strings.TrimSpace(input.Tool) == "" {
				return process.DynamicToolResult{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "需要工具名称")
			}
			if len(input.Arguments) == 0 {
				input.Arguments = json.RawMessage(`{}`)
			}
			result, err := s.runtime.Call(ctx, string(call.SessionID), v.Name, v.Definition, workdir, input.Tool, input.Arguments)
			if err != nil {
				return process.DynamicToolResult{}, err
			}
			out := process.DynamicToolResult{Success: !result.IsError}
			for _, c := range result.Content {
				item := process.DynamicToolContent{Type: "inputText", Text: c.Text}
				if c.Type == "image" {
					item.Type = "inputImage"
					item.ImageURL = "data:" + c.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(c.Data)
				} else if c.Type == "audio" {
					item.Type = "inputAudio"
					item.AudioURL = "data:" + c.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(c.Data)
				}
				out.Content = append(out.Content, item)
			}
			return out, nil
		}
	}
	return process.DynamicToolResult{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "MCP 服务不可用，请重新发现当前可用工具")
}

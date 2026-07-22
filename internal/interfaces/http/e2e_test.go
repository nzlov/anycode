package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	projectapp "github.com/nzlov/anycode/internal/application/project"
	workflowapp "github.com/nzlov/anycode/internal/application/workflow"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	"github.com/nzlov/anycode/internal/infra/config"
	"github.com/nzlov/anycode/internal/infra/entstore"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph"
)

func TestSaveWorkflowDefinitionAcceptsPrimitiveConditionValue(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	store, err := entstore.Open(ctx, entstore.OpenOptions{DatabaseURL: filepath.Join(dataDir, "anycode.db")})
	if err != nil {
		t.Fatalf("open entstore: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close entstore: %v", err)
		}
	})
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate entstore: %v", err)
	}

	projects := projectapp.New(store.Projects(), smokeDirectoryBrowser{}, smokeGitInspector{})
	workflows := workflowapp.New(store.Workflows())
	handler := NewHandler(config.Config{AccessKey: "secret"}, WithGraphQLUseCases(graph.UseCases{
		Projects: projects, Workflows: workflows,
	}))
	projectID := smokeGraphQL[string](t, handler, `mutation($input: CreateProjectInput!) {
		createProject(input: $input) { id }
	}`, map[string]any{"input": map[string]any{
		"path": filepath.Join(dataDir, "repo"), "name": "Smoke Project",
	}}, "createProject.id")

	for _, tt := range []struct {
		name  string
		value any
		want  any
	}{
		{name: "true", value: true, want: true},
		{name: "false", value: false, want: false},
		{name: "string", value: "passed", want: "passed"},
		{name: "number", value: 7.5, want: float64(7.5)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			saveResult := smokeGraphQL[map[string]any](t, handler, `mutation($input: SaveWorkflowDefinitionInput!) {
				saveWorkflowDefinition(input: $input) { id graph { edges { condition { value } } } }
			}`, map[string]any{"input": map[string]any{
				"projectId": projectID,
				"name":      "Primitive condition flow " + tt.name,
				"graph": map[string]any{
					"nodes": []map[string]any{
						{"id": "build", "type": "codex", "title": "Build", "position": map[string]any{"x": 0, "y": 0}},
						{"id": "ship", "type": "close", "title": "Ship", "position": map[string]any{"x": 100, "y": 0}},
					},
					"edges": []map[string]any{{
						"from": "build", "to": "ship", "priority": 0,
						"condition": map[string]any{"field": "results.status", "op": "eq", "value": tt.value},
					}},
				},
			}}, "saveWorkflowDefinition")
			condition := smokePath(t, saveResult, "graph.edges.0.condition").(map[string]any)
			if !reflect.DeepEqual(condition["value"], tt.want) {
				t.Fatalf("saved condition value = %#v, want %#v", condition["value"], tt.want)
			}
			workflowID := saveResult["id"].(string)
			readValue := smokeGraphQL[any](t, handler, `query($id: ID!) {
				workflowDefinition(id: $id) { graph { edges { condition { value } } } }
			}`, map[string]any{"id": workflowID}, "workflowDefinition.graph.edges.0.condition.value")
			if !reflect.DeepEqual(readValue, tt.want) {
				t.Fatalf("read condition value = %#v, want %#v", readValue, tt.want)
			}
		})
	}
}

func smokeGraphQL[T any](t *testing.T, handler http.Handler, query string, variables map[string]any, path string) T {
	t.Helper()
	reqBody, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var response struct {
		Data   map[string]any   `json:"data"`
		Errors []map[string]any `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || len(response.Errors) > 0 {
		t.Fatalf("graphql response status=%d errors=%#v body=%s", rec.Code, response.Errors, rec.Body.String())
	}
	value := smokePath(t, response.Data, path)
	typed, ok := value.(T)
	if !ok {
		t.Fatalf("graphql path %q = %#v (%T)", path, value, value)
	}
	return typed
}

func smokePath(t *testing.T, data map[string]any, path string) any {
	t.Helper()
	var current any = data
	start := 0
	for i := 0; i <= len(path); i++ {
		if i != len(path) && path[i] != '.' {
			continue
		}
		key := path[start:i]
		if items, ok := current.([]any); ok {
			index, err := strconv.Atoi(key)
			if err != nil || index < 0 || index >= len(items) {
				t.Fatalf("invalid graphql path %q", path)
			}
			current = items[index]
		} else {
			object, ok := current.(map[string]any)
			if !ok {
				t.Fatalf("invalid graphql path %q", path)
			}
			current = object[key]
		}
		start = i + 1
	}
	return current
}

type smokeGitInspector struct{}

func (smokeGitInspector) Detect(context.Context, string) (projectdomain.GitState, error) {
	return projectdomain.GitState{IsRepository: false}, nil
}
func (smokeGitInspector) Branches(context.Context, string) ([]projectdomain.GitBranch, error) {
	return nil, nil
}
func (smokeGitInspector) HeadCommit(context.Context, string, string) (string, error) { return "", nil }

type smokeDirectoryBrowser struct{}

func (smokeDirectoryBrowser) List(_ context.Context, path string) (projectdomain.DirectoryListing, error) {
	return projectdomain.DirectoryListing{Path: path}, nil
}

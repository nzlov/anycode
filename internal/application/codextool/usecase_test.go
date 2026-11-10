package codextool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	artifactapp "github.com/nzlov/anycode/internal/application/artifact"
	mindmapapp "github.com/nzlov/anycode/internal/application/mindmap"
	questionapp "github.com/nzlov/anycode/internal/application/question"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	tunnelapp "github.com/nzlov/anycode/internal/application/tunnel"
	mindmapdomain "github.com/nzlov/anycode/internal/domain/mindmap"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	questiondomain "github.com/nzlov/anycode/internal/domain/question"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	tunneldomain "github.com/nzlov/anycode/internal/domain/tunnel"
	"github.com/nzlov/anycode/internal/infra/filestore"
)

func TestQuestionsUsesCallIDAndReturnsAnswers(t *testing.T) {
	selected := questiondomain.OptionID("continue")
	sessions := &fakeSessions{result: questionapp.RequestDTO{
		ID: "call-1", SessionID: "session-1", Status: questiondomain.RequestAnswered,
		Questions: []questiondomain.Question{{
			ID: "call-1:0", SelectedOptionID: &selected, Answer: map[string]any{"approved": true},
		}},
	}}
	service := New(sessions, nil)

	result, err := service.HandleDynamicTool(context.Background(), processdomain.DynamicToolCall{
		CallID: "call-1", SessionID: "session-1", Tool: questionsTool,
		Arguments: json.RawMessage(`{"questions":[{"body":"Continue?","options":[{"id":"continue","label":"Continue","payload":{"action":"continue"}}]}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if sessions.input.RequestID != "call-1" || sessions.input.SessionID != "session-1" || len(sessions.input.Questions) != 1 {
		t.Fatalf("questions input = %#v", sessions.input)
	}
	question := sessions.input.Questions[0]
	if question.Body != "Continue?" || question.Type != "choice" || len(question.Options) != 1 || question.Options[0].Payload["action"] != "continue" {
		t.Fatalf("question = %#v", question)
	}
	if !result.Success || len(result.Content) != 1 || result.Content[0].Type != "inputText" {
		t.Fatalf("result = %#v", result)
	}
	var payload struct {
		RequestID string `json:"requestId"`
		Answers   []struct {
			QuestionID     string         `json:"questionId"`
			SelectedOption string         `json:"selectedOptionId"`
			Payload        map[string]any `json:"payload"`
		} `json:"answers"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.RequestID != "call-1" || len(payload.Answers) != 1 || payload.Answers[0].QuestionID != "call-1:0" || payload.Answers[0].SelectedOption != "continue" || payload.Answers[0].Payload["approved"] != true {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestQuestionsResolvesOptionalFilePathsForEachQuestion(t *testing.T) {
	ctx := context.Background()
	store := filestore.New(t.TempDir())
	root, err := store.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "styles"), 0o755); err != nil {
		t.Fatal(err)
	}
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "styles", "style.png"), imageData.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	sessions := &fakeSessions{result: questionapp.RequestDTO{
		ID: "call-1", SessionID: "session-1", Status: questiondomain.RequestAnswered,
	}}
	service := New(sessions, artifactapp.New(store))

	_, err = service.HandleDynamicTool(ctx, processdomain.DynamicToolCall{
		CallID: "call-1", SessionID: "session-1", Tool: questionsTool,
		Arguments: json.RawMessage(`{"questions":[{"body":"Use this style?","files":["styles/style.png","styles/style.png"]},{"body":"Continue?"}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	files := sessions.input.Questions[0].Files
	if len(files) != 1 || files[0].ID == "" || files[0].Filename != "style.png" || files[0].MimeType != "image/png" || files[0].PreviewKind != "image" || files[0].Size != int64(imageData.Len()) {
		t.Fatalf("question files = %#v", files)
	}
	stored, err := store.FindArtifact(ctx, sessiondomain.SessionFileID(files[0].ID))
	if err != nil || stored.SessionID != "session-1" || stored.LogicalPath != "styles/style.png" {
		t.Fatalf("resolved file = %#v, %v", stored, err)
	}
	if len(sessions.input.Questions[1].Files) != 0 {
		t.Fatalf("question without files = %#v", sessions.input.Questions[1])
	}
}

func TestQuestionsRejectsInvalidFileReferencesBeforeRequesting(t *testing.T) {
	ctx := context.Background()
	store := filestore.New(t.TempDir())
	root, err := store.EnsureArtifactDir(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "existing.txt"), []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("existing.txt", filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	otherRoot, err := store.EnsureArtifactDir(ctx, "session-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherRoot, "other.txt"), []byte("other session"), 0o644); err != nil {
		t.Fatal(err)
	}
	otherFiles, _, err := store.ListArtifacts(ctx, sessiondomain.ArtifactQuery{SessionID: "session-2"})
	if err != nil {
		t.Fatal(err)
	}
	tooMany := make([]string, artifactapp.MaxResolveArtifactPaths+1)
	for index := range tooMany {
		tooMany[index] = "existing.txt"
	}
	cases := map[string][]string{
		"missing":            {"existing.txt", "missing.txt"},
		"absolute":           {filepath.Join(root, "existing.txt")},
		"windows absolute":   {`C:\private.txt`},
		"parent":             {"../session-2/other.txt"},
		"dot segment":        {"folder/../existing.txt"},
		"empty":              {""},
		"symlink":            {"link.txt"},
		"directory":          {"folder"},
		"other session path": {"other.txt"},
		"file ID":            {string(otherFiles[0].ID)},
		"too many":           tooMany,
	}
	for name, paths := range cases {
		t.Run(name, func(t *testing.T) {
			sessions := &fakeSessions{}
			service := New(sessions, artifactapp.New(store))
			arguments, err := json.Marshal(map[string]any{"questions": []any{map[string]any{"body": "Use these files?", "files": paths}}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.HandleDynamicTool(ctx, processdomain.DynamicToolCall{
				CallID: "call-1", SessionID: "session-1", Tool: questionsTool, Arguments: arguments,
			})
			if err == nil || !strings.Contains(err.Error(), "resolve question files") {
				t.Fatalf("error = %v", err)
			}
			if sessions.input.RequestID != "" {
				t.Fatalf("invalid files created question request: %#v", sessions.input)
			}
		})
	}
}

func TestQuestionsRejectsMissingQuestions(t *testing.T) {
	service := New(&fakeSessions{}, nil)
	_, err := service.HandleDynamicTool(context.Background(), processdomain.DynamicToolCall{
		CallID: "call-1", SessionID: "session-1", Tool: questionsTool, Arguments: json.RawMessage(`{"questions":[]}`),
	})
	if err == nil || !strings.Contains(err.Error(), "questions are required") {
		t.Fatalf("error = %v", err)
	}
}

func TestQuestionsRejectsMissingBody(t *testing.T) {
	service := New(&fakeSessions{}, nil)
	_, err := service.HandleDynamicTool(context.Background(), processdomain.DynamicToolCall{
		CallID: "call-1", SessionID: "session-1", Tool: questionsTool,
		Arguments: json.RawMessage(`{"questions":[{"title":"Legacy title"}]}`),
	})
	if err == nil || !strings.Contains(err.Error(), "question body is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestDynamicToolRejectsUnknownTool(t *testing.T) {
	service := New(nil, nil)
	for _, name := range []string{"unknown", "publish_artifact"} {
		if _, err := service.HandleDynamicTool(context.Background(), processdomain.DynamicToolCall{Tool: name}); err == nil {
			t.Fatalf("tool %q error = nil", name)
		}
	}
}

func TestTunnelToolsUseCallingSessionOwnership(t *testing.T) {
	tunnels := &fakeTunnels{
		created: tunnelapp.CreateResult{
			Tunnel: tunnelapp.DTO{
				ID: "tunnel-1", SessionID: "session-1", Name: "Vite preview", Port: 4173,
				Hostname: "example.trycloudflare.com", URL: "https://example.trycloudflare.com",
				AccessURL: "https://example.trycloudflare.com/?anycode_auth=secret-token",
				Status:    tunneldomain.StatusRunning,
			},
			AccessURL: "https://example.trycloudflare.com/?anycode_auth=secret-token",
		},
	}
	service := New(nil, nil, WithTunnels(tunnels))

	created, err := service.HandleDynamicTool(context.Background(), processdomain.DynamicToolCall{
		SessionID: "session-1", Tool: tunnelCreateTool, Arguments: json.RawMessage(`{"name":"Vite preview","port":4173}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if tunnels.createInput.SessionID != "session-1" || tunnels.createInput.Name != "Vite preview" || tunnels.createInput.Port != 4173 {
		t.Fatalf("create input = %#v", tunnels.createInput)
	}
	if !strings.Contains(created.Content[0].Text, `"url":"https://example.trycloudflare.com/?anycode_auth=secret-token"`) {
		t.Fatalf("create result = %#v", created)
	}
	if !strings.Contains(created.Content[0].Text, `"name":"Vite preview"`) {
		t.Fatalf("create result name = %#v", created)
	}

	if _, err := service.HandleDynamicTool(context.Background(), processdomain.DynamicToolCall{
		SessionID: "session-1", Tool: tunnelCloseTool, Arguments: json.RawMessage(`{"id":"tunnel-1"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if tunnels.closeSession != "session-1" || tunnels.closeID != "tunnel-1" {
		t.Fatalf("close ownership = session %q id %q", tunnels.closeSession, tunnels.closeID)
	}
}

func TestMindMapToolsSearchAndUpdateCurrentGraph(t *testing.T) {
	maps := &fakeMindMaps{graph: mindmapapp.GraphDTO{
		ProjectID: "project-1", SessionID: "session-1",
		Nodes: []mindmapapp.NodeDTO{{ID: mindmapdomain.RootNodeID, Title: "AnyCode"}},
	}, searchResult: mindmapapp.SearchResultDTO{
		ProjectID: "project-1", SessionID: "session-1", Query: "tool", TagRevision: "revision-1", TotalMatches: 1,
		Matches: []mindmapapp.NodeMatchDTO{{Node: mindmapapp.NodeDTO{
			ID: "tools", Title: "Dynamic tools", Content: "Agent tools",
			Files: []mindmapdomain.NodeFile{{File: "internal/tools.go", Method: "Run", StartLine: 10, EndLine: 20}},
		}, MatchedFields: []string{"title", "content"}}},
		RelatedNodes: []mindmapapp.NodeDTO{{ID: "agent", Title: "Agent", Content: "not rendered"}},
		Edges:        []mindmapapp.EdgeDTO{{ID: "agent-tools", SourceID: "agent", TargetID: "tools", Label: "uses"}},
		NodeTags:     map[mindmapdomain.NodeID][]string{"tools": {"Backend", "Tools"}},
	}}
	service := New(nil, nil, WithMindMaps(maps))
	call := processdomain.DynamicToolCall{ProcessRunID: "run-1", SessionID: "session-1"}

	call.Tool = string(processdomain.DynamicToolMindMapSearch)
	call.Arguments = json.RawMessage(`{"query":"tool","limit":5}`)
	searchResult, err := service.HandleDynamicTool(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	wantSearchResult := `# mind_map_search
project: project-1
session: session-1
query: tool
tagRevision: revision-1
matches: 1/1
related: 1/1
edges: 1/1
truncated: false

## Matches
- tools — Dynamic tools
  matched: title, content
  tags: Backend, Tools
  content: Agent tools
  files:
  - internal/tools.go:10-20 — Run

## Related
- agent — Agent

## Edges
- agent-tools: agent -> tools — uses`
	if maps.query != "tool" || maps.limit != 5 || len(searchResult.Content) != 1 || searchResult.Content[0].Type != "inputText" || searchResult.Content[0].Text != wantSearchResult {
		t.Fatalf("search = maps:%#v result:%#v", maps, searchResult)
	}
	call.Tool = string(processdomain.DynamicToolMindMapTags)
	call.Arguments = json.RawMessage(`{}`)
	maps.tagList.Tags = []mindmapapp.NodeDTO{{ID: "tag:backend", Title: "Backend", Content: "unused"}}
	tagResult, err := service.HandleDynamicTool(context.Background(), call)
	if err != nil || tagResult.Content[0].Text != `{"tagRevision":"revision-1","tags":["Backend"]}` {
		t.Fatalf("tags result = %#v err = %v", tagResult, err)
	}

	call.Tool = string(processdomain.DynamicToolMindMapUpdate)
	call.Arguments = json.RawMessage(`{"tagRevision":"revision-1","operations":[{"kind":"upsert_node","id":"feature","title":"Feature","content":"Details","tags":["Backend"],"files":[{"file":"internal/feature.go","method":"Run","startLine":1,"endLine":9}]}]}`)
	updateResult, err := service.HandleDynamicTool(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(updateResult.Content[0].Text, `"nodes"`) || !strings.Contains(updateResult.Content[0].Text, `"appliedOperations":1`) {
		t.Fatalf("update result = %#v", updateResult)
	}
	if len(maps.operations) != 1 || maps.operations[0].ID != "feature" || maps.operations[0].Title == nil || *maps.operations[0].Title != "Feature" || maps.operations[0].Content == nil || *maps.operations[0].Content != "Details" || maps.operations[0].Files == nil || len(*maps.operations[0].Files) != 1 || (*maps.operations[0].Files)[0].StartLine != 1 {
		t.Fatalf("operations = %#v", maps.operations)
	}
	if maps.operations[0].Tags == nil || len(*maps.operations[0].Tags) != 1 || (*maps.operations[0].Tags)[0] != "Backend" {
		t.Fatalf("managed node intent = %#v", maps.operations[0])
	}
	if maps.tagRevision != "revision-1" {
		t.Fatalf("tag revision = %q", maps.tagRevision)
	}
}

func TestMindMapSearchResultOmitsEmptySections(t *testing.T) {
	result := mindMapSearchResult(mindmapapp.SearchResultDTO{
		ProjectID: "project-1", SessionID: "session-1", Query: "missing", TagRevision: "revision-1",
	})
	want := "# mind_map_search\nproject: project-1\nsession: session-1\nquery: missing\ntagRevision: revision-1\nmatches: 0/0\nrelated: 0/0\nedges: 0/0\ntruncated: false"
	if len(result.Content) != 1 || result.Content[0].Text != want {
		t.Fatalf("result = %#v", result)
	}
}

func TestMindMapSearchResultEnforcesOverallOutputBudget(t *testing.T) {
	matches := make([]mindmapapp.NodeMatchDTO, 50)
	for index := range matches {
		matches[index] = mindmapapp.NodeMatchDTO{Node: mindmapapp.NodeDTO{
			ID: mindmapdomain.NodeID(fmt.Sprintf("node-%02d", index)), Title: "Large node", Content: strings.Repeat("x", 20_000),
		}, MatchedFields: []string{"content"}}
	}
	result := mindMapSearchResult(mindmapapp.SearchResultDTO{
		ProjectID: "project-1", SessionID: "session-1", Query: "large", TagRevision: "revision-1",
		Matches: matches, TotalMatches: len(matches),
	})

	text := result.Content[0].Text
	if len(text) > mindMapSearchMaxOutputBytes || !strings.Contains(text, "truncated: true") || !strings.Contains(text, "details: omitted to fit output budget") {
		t.Fatalf("budgeted search result length=%d result=%q", len(text), text)
	}
}

type fakeSessions struct {
	input  sessionapp.RequestQuestionsInput
	result questionapp.RequestDTO
	err    error
}

func (f *fakeSessions) RequestQuestions(_ context.Context, input sessionapp.RequestQuestionsInput) (questionapp.RequestDTO, error) {
	f.input = input
	return f.result, f.err
}

type fakeTunnels struct {
	createInput  tunnelapp.CreateInput
	created      tunnelapp.CreateResult
	items        []tunnelapp.DTO
	closeSession tunneldomain.SessionID
	closeID      tunneldomain.ID
}

type fakeMindMaps struct {
	processRunID string
	sessionID    mindmapdomain.SessionID
	query        string
	limit        int
	operations   []mindmapapp.OperationInput
	tagRevision  string
	graph        mindmapapp.GraphDTO
	tagList      mindmapapp.TagListDTO
	searchResult mindmapapp.SearchResultDTO
}

func (f *fakeMindMaps) ListTagsForProcess(_ context.Context, processRunID string, sessionID mindmapdomain.SessionID) (mindmapapp.TagListDTO, error) {
	f.processRunID = processRunID
	f.sessionID = sessionID
	if f.tagList.Revision == "" {
		f.tagList.Revision = "revision-1"
	}
	return f.tagList, nil
}

func (f *fakeMindMaps) SearchForProcess(_ context.Context, processRunID string, sessionID mindmapdomain.SessionID, query string, limit int) (mindmapapp.SearchResultDTO, error) {
	f.processRunID = processRunID
	f.sessionID = sessionID
	f.query = query
	f.limit = limit
	return f.searchResult, nil
}

func (f *fakeMindMaps) UpdateForProcess(_ context.Context, processRunID string, sessionID mindmapdomain.SessionID, tagRevision string, operations []mindmapapp.OperationInput) (mindmapapp.GraphDTO, error) {
	f.processRunID = processRunID
	f.sessionID = sessionID
	f.tagRevision = tagRevision
	f.operations = operations
	return f.graph, nil
}

func (f *fakeTunnels) Create(_ context.Context, input tunnelapp.CreateInput) (tunnelapp.CreateResult, error) {
	f.createInput = input
	return f.created, nil
}

func (f *fakeTunnels) List(context.Context) ([]tunnelapp.DTO, error) { return f.items, nil }

func (f *fakeTunnels) CloseOwned(_ context.Context, sessionID tunneldomain.SessionID, id tunneldomain.ID) error {
	f.closeSession = sessionID
	f.closeID = id
	return nil
}

package codextool

import (
	"context"
	"encoding/json"
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

func TestQuestionsResolvesOptionalFilesForEachQuestion(t *testing.T) {
	artifacts := &fakeArtifacts{resolved: []sessiondomain.SessionFile{{
		ID: "artifact-1", SessionID: "session-1", Filename: "style.png", MimeType: "image/png",
		Size: 42, PreviewKind: sessiondomain.PreviewKindImage,
	}}}
	sessions := &fakeSessions{result: questionapp.RequestDTO{
		ID: "call-1", SessionID: "session-1", Status: questiondomain.RequestAnswered,
	}}
	service := New(sessions, artifacts)

	_, err := service.HandleDynamicTool(context.Background(), processdomain.DynamicToolCall{
		CallID: "call-1", SessionID: "session-1", Tool: questionsTool,
		Arguments: json.RawMessage(`{"questions":[{"body":"Use this style?","files":["artifact-1"]}]}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if artifacts.resolveSessionID != "session-1" || len(artifacts.resolveIDs) != 1 || artifacts.resolveIDs[0] != "artifact-1" {
		t.Fatalf("resolve input = session %q ids %#v", artifacts.resolveSessionID, artifacts.resolveIDs)
	}
	files := sessions.input.Questions[0].Files
	if len(files) != 1 || files[0].ID != "artifact-1" || files[0].Filename != "style.png" || files[0].PreviewKind != "image" {
		t.Fatalf("question files = %#v", files)
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

func TestPublishArtifactReturnsMetadataAndImageContent(t *testing.T) {
	artifacts := &fakeArtifacts{
		artifact: sessiondomain.SessionFile{
			ID: "artifact-1", SessionID: "session-1", LogicalPath: "report/chart.png", Filename: "chart.png",
			MimeType: "image/png", ArtifactKind: sessiondomain.ArtifactKindImage, PreviewKind: sessiondomain.PreviewKindImage, Size: 3,
		},
		content:    artifactapp.ToolContent{Type: "image", MIMEType: "image/png", Data: []byte{1, 2, 3}},
		hasContent: true,
	}
	service := New(nil, artifacts)

	result, err := service.HandleDynamicTool(context.Background(), processdomain.DynamicToolCall{
		SessionID: "session-1", Tool: publishArtifactTool, Arguments: json.RawMessage(`{"path":"report/chart.png"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if artifacts.input.SessionID != "session-1" || artifacts.input.Path != "report/chart.png" {
		t.Fatalf("publish input = %#v", artifacts.input)
	}
	if len(result.Content) != 2 || result.Content[0].Type != "inputText" || result.Content[1].Type != "inputImage" || result.Content[1].ImageURL != "data:image/png;base64,AQID" {
		t.Fatalf("result = %#v", result)
	}
}

func TestDynamicToolRejectsUnknownToolAndParentPath(t *testing.T) {
	service := New(nil, &fakeArtifacts{})
	if _, err := service.HandleDynamicTool(context.Background(), processdomain.DynamicToolCall{Tool: "unknown"}); err == nil {
		t.Fatal("unknown tool error = nil")
	}
	if _, err := service.HandleDynamicTool(context.Background(), processdomain.DynamicToolCall{
		SessionID: "session-1", Tool: publishArtifactTool, Arguments: json.RawMessage(`{"path":"../secret"}`),
	}); err == nil {
		t.Fatal("parent path error = nil")
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
		ProjectID: "project-1", SessionID: "session-1", Query: "tool", TotalMatches: 1,
		Matches: []mindmapapp.NodeMatchDTO{{Node: mindmapapp.NodeDTO{
			ID: "tools", Title: "Dynamic tools", Content: "Agent tools",
			Files: []mindmapdomain.NodeFile{{File: "internal/tools.go", Method: "Run", StartLine: 10, EndLine: 20}},
		}, MatchedFields: []string{"title", "content"}}},
		RelatedNodes: []mindmapapp.NodeDTO{{ID: "agent", Title: "Agent"}},
		Edges:        []mindmapapp.EdgeDTO{{ID: "agent-tools", SourceID: "agent", TargetID: "tools", Label: "uses"}},
	}}
	service := New(nil, nil, WithMindMaps(maps))
	call := processdomain.DynamicToolCall{ProcessRunID: "run-1", SessionID: "session-1"}

	call.Tool = string(processdomain.DynamicToolMindMapSearch)
	call.Arguments = json.RawMessage(`{"query":"tool","limit":5}`)
	searchResult, err := service.HandleDynamicTool(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	if maps.query != "tool" || maps.limit != 5 || !strings.Contains(searchResult.Content[0].Text, `"matchedFields":["title","content"]`) || !strings.Contains(searchResult.Content[0].Text, `"agent-tools"`) || !strings.Contains(searchResult.Content[0].Text, `"startLine":10`) {
		t.Fatalf("search = maps:%#v result:%#v", maps, searchResult)
	}
	call.Tool = string(processdomain.DynamicToolMindMapTags)
	call.Arguments = json.RawMessage(`{}`)
	tagResult, err := service.HandleDynamicTool(context.Background(), call)
	if err != nil || !strings.Contains(tagResult.Content[0].Text, `"tagRevision":"revision-1"`) {
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

type fakeSessions struct {
	input  sessionapp.RequestQuestionsInput
	result questionapp.RequestDTO
	err    error
}

func (f *fakeSessions) RequestQuestions(_ context.Context, input sessionapp.RequestQuestionsInput) (questionapp.RequestDTO, error) {
	f.input = input
	return f.result, f.err
}

type fakeArtifacts struct {
	input            artifactapp.PublishInput
	artifact         sessiondomain.SessionFile
	resolved         []sessiondomain.SessionFile
	resolveSessionID sessiondomain.ID
	resolveIDs       []sessiondomain.SessionFileID
	content          artifactapp.ToolContent
	hasContent       bool
	err              error
}

func (f *fakeArtifacts) ResolveIDs(_ context.Context, sessionID sessiondomain.ID, ids []sessiondomain.SessionFileID) ([]sessiondomain.SessionFile, error) {
	f.resolveSessionID = sessionID
	f.resolveIDs = ids
	return f.resolved, f.err
}

func (f *fakeArtifacts) Publish(_ context.Context, input artifactapp.PublishInput) (sessiondomain.SessionFile, error) {
	f.input = input
	return f.artifact, f.err
}

func (f *fakeArtifacts) ReadToolContent(context.Context, sessiondomain.SessionFileID) (artifactapp.ToolContent, bool, error) {
	return f.content, f.hasContent, f.err
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

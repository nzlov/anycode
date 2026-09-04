package codextool

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

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

const (
	questionsTool                   = "questions"
	publishArtifactTool             = "publish_artifact"
	tunnelCreateTool                = "tunnel_create"
	tunnelListTool                  = "tunnel_list"
	tunnelCloseTool                 = "tunnel_close"
	mindMapSearchMaxOutputBytes     = 16 * 1024
	mindMapSearchHeaderReserveBytes = 2 * 1024
)

type SessionUseCase interface {
	RequestQuestions(ctx context.Context, input sessionapp.RequestQuestionsInput) (questionapp.RequestDTO, error)
}

type ArtifactUseCase interface {
	Publish(ctx context.Context, input artifactapp.PublishInput) (sessiondomain.SessionFile, error)
	ResolveIDs(ctx context.Context, sessionID sessiondomain.ID, ids []sessiondomain.SessionFileID) ([]sessiondomain.SessionFile, error)
	ReadToolContent(ctx context.Context, id sessiondomain.SessionFileID) (artifactapp.ToolContent, bool, error)
}

type TunnelUseCase interface {
	Create(ctx context.Context, input tunnelapp.CreateInput) (tunnelapp.CreateResult, error)
	List(ctx context.Context) ([]tunnelapp.DTO, error)
	CloseOwned(ctx context.Context, sessionID tunneldomain.SessionID, id tunneldomain.ID) error
}

type MindMapUseCase interface {
	SearchForProcess(ctx context.Context, processRunID string, sessionID mindmapdomain.SessionID, query string, limit int) (mindmapapp.SearchResultDTO, error)
	ListTagsForProcess(ctx context.Context, processRunID string, sessionID mindmapdomain.SessionID) (mindmapapp.TagListDTO, error)
	UpdateForProcess(ctx context.Context, processRunID string, sessionID mindmapdomain.SessionID, tagRevision string, operations []mindmapapp.OperationInput) (mindmapapp.GraphDTO, error)
}

type Service struct {
	sessions  SessionUseCase
	artifacts ArtifactUseCase
	tunnels   TunnelUseCase
	mindMaps  MindMapUseCase
}

type Option func(*Service)

func WithTunnels(tunnels TunnelUseCase) Option {
	return func(s *Service) { s.tunnels = tunnels }
}

func WithMindMaps(mindMaps MindMapUseCase) Option {
	return func(s *Service) { s.mindMaps = mindMaps }
}

func New(sessions SessionUseCase, artifacts ArtifactUseCase, options ...Option) *Service {
	service := &Service{sessions: sessions, artifacts: artifacts}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) HandleDynamicTool(ctx context.Context, call processdomain.DynamicToolCall) (processdomain.DynamicToolResult, error) {
	switch call.Tool {
	case questionsTool:
		return s.questions(ctx, call)
	case publishArtifactTool:
		return s.publishArtifact(ctx, call)
	case tunnelCreateTool:
		return s.createTunnel(ctx, call)
	case tunnelListTool:
		return s.listTunnels(ctx, call)
	case tunnelCloseTool:
		return s.closeTunnel(ctx, call)
	case string(processdomain.DynamicToolMindMapSearch):
		return s.searchMindMap(ctx, call)
	case string(processdomain.DynamicToolMindMapTags):
		return s.listMindMapTags(ctx, call)
	case string(processdomain.DynamicToolMindMapUpdate):
		return s.updateMindMap(ctx, call)
	default:
		return processdomain.DynamicToolResult{}, fmt.Errorf("unknown dynamic tool %q", call.Tool)
	}
}

func (s *Service) listMindMapTags(ctx context.Context, call processdomain.DynamicToolCall) (processdomain.DynamicToolResult, error) {
	if s == nil || s.mindMaps == nil {
		return processdomain.DynamicToolResult{}, errors.New("mind map service is unavailable")
	}
	result, err := s.mindMaps.ListTagsForProcess(ctx, string(call.ProcessRunID), mindmapdomain.SessionID(call.SessionID))
	if err != nil {
		return processdomain.DynamicToolResult{}, err
	}
	tags := make([]string, 0, len(result.Tags))
	for _, tag := range result.Tags {
		tags = append(tags, tag.Title)
	}
	payload, err := json.Marshal(map[string]any{
		"tagRevision": result.Revision, "tags": tags,
	})
	if err != nil {
		return processdomain.DynamicToolResult{}, fmt.Errorf("encode mind map tags: %w", err)
	}
	return textResult(payload), nil
}

func (s *Service) searchMindMap(ctx context.Context, call processdomain.DynamicToolCall) (processdomain.DynamicToolResult, error) {
	if s == nil || s.mindMaps == nil {
		return processdomain.DynamicToolResult{}, errors.New("mind map service is unavailable")
	}
	var input struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(call.Arguments, &input); err != nil {
		return processdomain.DynamicToolResult{}, fmt.Errorf("decode mind_map_search arguments: %w", err)
	}
	result, err := s.mindMaps.SearchForProcess(ctx, string(call.ProcessRunID), mindmapdomain.SessionID(call.SessionID), input.Query, input.Limit)
	if err != nil {
		return processdomain.DynamicToolResult{}, err
	}
	return mindMapSearchResult(result), nil
}

func (s *Service) updateMindMap(ctx context.Context, call processdomain.DynamicToolCall) (processdomain.DynamicToolResult, error) {
	if s == nil || s.mindMaps == nil {
		return processdomain.DynamicToolResult{}, errors.New("mind map service is unavailable")
	}
	var input struct {
		TagRevision string `json:"tagRevision"`
		Operations  []struct {
			Kind     mindmapdomain.ChangeKind  `json:"kind"`
			ID       string                    `json:"id"`
			Title    *string                   `json:"title"`
			Content  *string                   `json:"content"`
			Files    *[]mindmapdomain.NodeFile `json:"files"`
			Tags     *[]string                 `json:"tags"`
			SourceID *mindmapdomain.NodeID     `json:"sourceId"`
			TargetID *mindmapdomain.NodeID     `json:"targetId"`
			Label    *string                   `json:"label"`
		} `json:"operations"`
	}
	if err := json.Unmarshal(call.Arguments, &input); err != nil {
		return processdomain.DynamicToolResult{}, fmt.Errorf("decode mind_map_update arguments: %w", err)
	}
	if strings.TrimSpace(input.TagRevision) == "" {
		return processdomain.DynamicToolResult{}, errors.New("mind map tag revision is required; run mind_map_search or mind_map_tags before updating")
	}
	operations := make([]mindmapapp.OperationInput, 0, len(input.Operations))
	for _, operation := range input.Operations {
		operations = append(operations, mindmapapp.OperationInput{
			Kind: operation.Kind, ID: operation.ID, Title: operation.Title, Content: operation.Content,
			Files: operation.Files, Tags: operation.Tags,
			SourceID: operation.SourceID, TargetID: operation.TargetID, Label: operation.Label,
		})
	}
	graph, err := s.mindMaps.UpdateForProcess(ctx, string(call.ProcessRunID), mindmapdomain.SessionID(call.SessionID), input.TagRevision, operations)
	if err != nil {
		return processdomain.DynamicToolResult{}, err
	}
	return mindMapUpdateResult(graph, len(operations))
}

func mindMapUpdateResult(graph mindmapapp.GraphDTO, appliedOperations int) (processdomain.DynamicToolResult, error) {
	payload, err := json.Marshal(map[string]any{
		"projectId": graph.ProjectID, "sessionId": graph.SessionID, "updatedAt": graph.UpdatedAt,
		"appliedOperations": appliedOperations, "nodeCount": len(graph.Nodes), "edgeCount": len(graph.Edges),
	})
	if err != nil {
		return processdomain.DynamicToolResult{}, fmt.Errorf("encode mind map update result: %w", err)
	}
	return textResult(payload), nil
}

func mindMapSearchResult(result mindmapapp.SearchResultDTO) processdomain.DynamicToolResult {
	bodyBudget := mindMapSearchMaxOutputBytes - mindMapSearchHeaderReserveBytes
	usedBodyBytes := 0
	detailsTruncated := false
	matchItems := make([]string, 0, len(result.Matches))
	for _, match := range result.Matches {
		item := renderMindMapMarkdownNode(match.Node, match.MatchedFields, result.NodeTags[match.Node.ID], true)
		headingBytes := 0
		if len(matchItems) == 0 {
			headingBytes = len("\n## Matches\n")
		}
		if usedBodyBytes+headingBytes+len(item) > bodyBudget {
			item = renderMindMapMarkdownNode(match.Node, match.MatchedFields, result.NodeTags[match.Node.ID], false)
			item += "  details: omitted to fit output budget\n"
			detailsTruncated = true
		}
		if usedBodyBytes+headingBytes+len(item) > bodyBudget {
			break
		}
		matchItems = append(matchItems, item)
		usedBodyBytes += headingBytes + len(item)
	}

	relatedItems := make([]string, 0, len(result.RelatedNodes))
	for _, node := range result.RelatedNodes {
		item := fmt.Sprintf("- %s — %s\n", node.ID, node.Title)
		headingBytes := 0
		if len(relatedItems) == 0 {
			headingBytes = len("\n## Related\n")
		}
		if usedBodyBytes+headingBytes+len(item) > bodyBudget {
			break
		}
		relatedItems = append(relatedItems, item)
		usedBodyBytes += headingBytes + len(item)
	}

	edgeItems := make([]string, 0, len(result.Edges))
	for _, edge := range result.Edges {
		var item strings.Builder
		fmt.Fprintf(&item, "- %s: %s -> %s", edge.ID, edge.SourceID, edge.TargetID)
		if edge.Label != "" {
			fmt.Fprintf(&item, " — %s", edge.Label)
		}
		item.WriteByte('\n')
		headingBytes := 0
		if len(edgeItems) == 0 {
			headingBytes = len("\n## Edges\n")
		}
		if usedBodyBytes+headingBytes+item.Len() > bodyBudget {
			break
		}
		edgeItems = append(edgeItems, item.String())
		usedBodyBytes += headingBytes + item.Len()
	}

	truncated := result.Truncated || detailsTruncated || len(matchItems) < len(result.Matches) ||
		len(relatedItems) < len(result.RelatedNodes) || len(edgeItems) < len(result.Edges)
	var output strings.Builder
	output.WriteString("# mind_map_search\n")
	fmt.Fprintf(&output, "project: %s\nsession: %s\n", result.ProjectID, result.SessionID)
	writeMindMapMarkdownField(&output, "query", result.Query, "")
	fmt.Fprintf(&output, "tagRevision: %s\n", result.TagRevision)
	fmt.Fprintf(&output, "matches: %d/%d\nrelated: %d/%d\nedges: %d/%d\ntruncated: %t\n",
		len(matchItems), result.TotalMatches, len(relatedItems), len(result.RelatedNodes), len(edgeItems), len(result.Edges), truncated)

	if len(matchItems) > 0 {
		output.WriteString("\n## Matches\n")
		for _, item := range matchItems {
			output.WriteString(item)
		}
	}

	if len(relatedItems) > 0 {
		output.WriteString("\n## Related\n")
		for _, item := range relatedItems {
			output.WriteString(item)
		}
	}

	if len(edgeItems) > 0 {
		output.WriteString("\n## Edges\n")
		for _, item := range edgeItems {
			output.WriteString(item)
		}
	}
	return textResult([]byte(strings.TrimSuffix(output.String(), "\n")))
}

func renderMindMapMarkdownNode(node mindmapapp.NodeDTO, matchedFields, tags []string, includeDetails bool) string {
	var output strings.Builder
	fmt.Fprintf(&output, "- %s — %s\n", node.ID, node.Title)
	if len(matchedFields) > 0 {
		fmt.Fprintf(&output, "  matched: %s\n", strings.Join(matchedFields, ", "))
	}
	if len(tags) > 0 {
		fmt.Fprintf(&output, "  tags: %s\n", strings.Join(tags, ", "))
	}
	if !includeDetails {
		return output.String()
	}
	if node.Content != "" {
		writeMindMapMarkdownField(&output, "content", node.Content, "  ")
	}
	if len(node.Files) == 0 {
		return output.String()
	}
	output.WriteString("  files:\n")
	for _, file := range node.Files {
		fmt.Fprintf(&output, "  - %s:%d-%d — %s\n", file.File, file.StartLine, file.EndLine, file.Method)
	}
	return output.String()
}

func writeMindMapMarkdownField(output *strings.Builder, name string, value string, indent string) {
	lines := strings.Split(value, "\n")
	fmt.Fprintf(output, "%s%s: %s\n", indent, name, lines[0])
	for _, line := range lines[1:] {
		fmt.Fprintf(output, "%s  %s\n", indent, line)
	}
}

func (s *Service) createTunnel(ctx context.Context, call processdomain.DynamicToolCall) (processdomain.DynamicToolResult, error) {
	if s == nil || s.tunnels == nil {
		return processdomain.DynamicToolResult{}, errors.New("tunnel service is unavailable")
	}
	var input struct {
		Name string `json:"name"`
		Port int    `json:"port"`
	}
	if err := json.Unmarshal(call.Arguments, &input); err != nil {
		return processdomain.DynamicToolResult{}, fmt.Errorf("decode tunnel_create arguments: %w", err)
	}
	created, err := s.tunnels.Create(ctx, tunnelapp.CreateInput{
		SessionID: tunneldomain.SessionID(call.SessionID), Name: input.Name, Port: input.Port,
	})
	if err != nil {
		return processdomain.DynamicToolResult{}, err
	}
	payload, err := json.Marshal(map[string]any{
		"id": created.Tunnel.ID, "name": created.Tunnel.Name, "url": created.AccessURL, "publicUrl": created.Tunnel.URL,
		"hostname": created.Tunnel.Hostname, "port": created.Tunnel.Port, "status": created.Tunnel.Status,
	})
	if err != nil {
		return processdomain.DynamicToolResult{}, fmt.Errorf("encode tunnel_create result: %w", err)
	}
	return textResult(payload), nil
}

func (s *Service) listTunnels(ctx context.Context, call processdomain.DynamicToolCall) (processdomain.DynamicToolResult, error) {
	if s == nil || s.tunnels == nil {
		return processdomain.DynamicToolResult{}, errors.New("tunnel service is unavailable")
	}
	items, err := s.tunnels.List(ctx)
	if err != nil {
		return processdomain.DynamicToolResult{}, err
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item.SessionID != tunneldomain.SessionID(call.SessionID) {
			continue
		}
		result = append(result, map[string]any{
			"id": item.ID, "name": item.Name, "url": item.AccessURL, "publicUrl": item.URL, "hostname": item.Hostname,
			"port": item.Port, "status": item.Status, "createdAt": item.CreatedAt,
		})
	}
	payload, err := json.Marshal(map[string]any{"tunnels": result})
	if err != nil {
		return processdomain.DynamicToolResult{}, fmt.Errorf("encode tunnel_list result: %w", err)
	}
	return textResult(payload), nil
}

func (s *Service) closeTunnel(ctx context.Context, call processdomain.DynamicToolCall) (processdomain.DynamicToolResult, error) {
	if s == nil || s.tunnels == nil {
		return processdomain.DynamicToolResult{}, errors.New("tunnel service is unavailable")
	}
	var input struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(call.Arguments, &input); err != nil {
		return processdomain.DynamicToolResult{}, fmt.Errorf("decode tunnel_close arguments: %w", err)
	}
	id := tunneldomain.ID(strings.TrimSpace(input.ID))
	if id == "" {
		return processdomain.DynamicToolResult{}, errors.New("tunnel id is required")
	}
	if err := s.tunnels.CloseOwned(ctx, tunneldomain.SessionID(call.SessionID), id); err != nil {
		return processdomain.DynamicToolResult{}, err
	}
	payload, _ := json.Marshal(map[string]any{"id": id, "closed": true})
	return textResult(payload), nil
}

func (s *Service) questions(ctx context.Context, call processdomain.DynamicToolCall) (processdomain.DynamicToolResult, error) {
	if s == nil || s.sessions == nil {
		return processdomain.DynamicToolResult{}, errors.New("session service is unavailable")
	}
	if strings.TrimSpace(call.CallID) == "" || call.SessionID == "" {
		return processdomain.DynamicToolResult{}, errors.New("questions call id and session id are required")
	}
	var input questionsInput
	if err := json.Unmarshal(call.Arguments, &input); err != nil {
		return processdomain.DynamicToolResult{}, fmt.Errorf("decode questions arguments: %w", err)
	}
	questions, err := s.buildQuestions(ctx, sessiondomain.ID(call.SessionID), input.Questions)
	if err != nil {
		return processdomain.DynamicToolResult{}, err
	}
	request, err := s.sessions.RequestQuestions(ctx, sessionapp.RequestQuestionsInput{
		RequestID: questiondomain.RequestID(call.CallID),
		SessionID: sessiondomain.ID(call.SessionID),
		Questions: questions,
	})
	if err != nil {
		return processdomain.DynamicToolResult{}, err
	}
	payload, err := json.Marshal(questionResult(request))
	if err != nil {
		return processdomain.DynamicToolResult{}, fmt.Errorf("encode questions result: %w", err)
	}
	return textResult(payload), nil
}

func (s *Service) publishArtifact(ctx context.Context, call processdomain.DynamicToolCall) (processdomain.DynamicToolResult, error) {
	if s == nil || s.artifacts == nil {
		return processdomain.DynamicToolResult{}, errors.New("artifact service is unavailable")
	}
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(call.Arguments, &input); err != nil {
		return processdomain.DynamicToolResult{}, fmt.Errorf("decode publish_artifact arguments: %w", err)
	}
	path := strings.TrimSpace(input.Path)
	clean := filepath.Clean(path)
	if path == "" || filepath.IsAbs(path) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return processdomain.DynamicToolResult{}, errors.New("artifact path must be relative to ANYCODE_ARTIFACT_DIR")
	}
	artifact, err := s.artifacts.Publish(ctx, artifactapp.PublishInput{SessionID: sessiondomain.ID(call.SessionID), Path: path})
	if err != nil {
		return processdomain.DynamicToolResult{}, err
	}
	payload, err := json.Marshal(map[string]any{
		"id":           string(artifact.ID),
		"logicalPath":  artifact.LogicalPath,
		"filename":     artifact.Filename,
		"mimeType":     artifact.MimeType,
		"artifactKind": string(artifact.ArtifactKind),
		"previewKind":  string(artifact.PreviewKind),
		"size":         artifact.Size,
	})
	if err != nil {
		return processdomain.DynamicToolResult{}, fmt.Errorf("encode publish_artifact result: %w", err)
	}
	result := textResult(payload)
	media, ok, err := s.artifacts.ReadToolContent(ctx, artifact.ID)
	if err != nil {
		return processdomain.DynamicToolResult{}, err
	}
	if !ok {
		return result, nil
	}
	dataURL := "data:" + media.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(media.Data)
	switch media.Type {
	case "image":
		result.Content = append(result.Content, processdomain.DynamicToolContent{Type: "inputImage", ImageURL: dataURL})
	case "audio":
		result.Content = append(result.Content, processdomain.DynamicToolContent{Type: "inputAudio", AudioURL: dataURL})
	}
	return result, nil
}

type questionsInput struct {
	Questions []questionInput `json:"questions"`
}

type questionInput struct {
	Body    string        `json:"body"`
	Type    string        `json:"type"`
	Files   []string      `json:"files"`
	Options []optionInput `json:"options"`
}

type optionInput struct {
	ID          string         `json:"id"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Payload     map[string]any `json:"payload"`
}

func (s *Service) buildQuestions(ctx context.Context, sessionID sessiondomain.ID, inputs []questionInput) ([]questiondomain.Question, error) {
	if len(inputs) == 0 {
		return nil, errors.New("questions are required")
	}
	questions := make([]questiondomain.Question, 0, len(inputs))
	for _, input := range inputs {
		body := strings.TrimSpace(input.Body)
		if body == "" {
			return nil, errors.New("question body is required")
		}
		questionType := strings.TrimSpace(input.Type)
		if questionType == "" {
			questionType = "choice"
		}
		files, err := s.resolveQuestionFiles(ctx, sessionID, input.Files)
		if err != nil {
			return nil, err
		}
		options := make([]questiondomain.Option, 0, len(input.Options))
		for _, inputOption := range input.Options {
			id := strings.TrimSpace(inputOption.ID)
			if id == "" {
				id = strings.TrimSpace(inputOption.Label)
			}
			if id == "" {
				return nil, errors.New("question option id or label is required")
			}
			options = append(options, questiondomain.Option{
				ID:          questiondomain.OptionID(id),
				Label:       inputOption.Label,
				Description: inputOption.Description,
				Payload:     inputOption.Payload,
			})
		}
		questions = append(questions, questiondomain.Question{
			Body: body, Type: questionType, Files: files, Options: options, Status: string(questiondomain.RequestPending),
		})
	}
	return questions, nil
}

func (s *Service) resolveQuestionFiles(ctx context.Context, sessionID sessiondomain.ID, ids []string) ([]questiondomain.File, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if s.artifacts == nil {
		return nil, errors.New("artifact service is unavailable")
	}
	artifactIDs := make([]sessiondomain.SessionFileID, len(ids))
	for i, id := range ids {
		artifactIDs[i] = sessiondomain.SessionFileID(strings.TrimSpace(id))
	}
	artifacts, err := s.artifacts.ResolveIDs(ctx, sessionID, artifactIDs)
	if err != nil {
		return nil, fmt.Errorf("resolve question files: %w", err)
	}
	files := make([]questiondomain.File, 0, len(artifacts))
	for _, artifact := range artifacts {
		files = append(files, questiondomain.File{
			ID: string(artifact.ID), Filename: artifact.Filename, MimeType: artifact.MimeType,
			Size: artifact.Size, PreviewKind: string(artifact.PreviewKind),
		})
	}
	return files, nil
}

func questionResult(request questionapp.RequestDTO) map[string]any {
	answers := make([]map[string]any, 0, len(request.Questions))
	for _, question := range request.Questions {
		answer := map[string]any{
			"questionId":   string(question.ID),
			"customAnswer": question.CustomAnswer,
			"payload":      question.Answer,
		}
		if question.SelectedOptionID != nil {
			answer["selectedOptionId"] = string(*question.SelectedOptionID)
		}
		answers = append(answers, answer)
	}
	return map[string]any{"requestId": string(request.ID), "answers": answers}
}

func textResult(payload []byte) processdomain.DynamicToolResult {
	return processdomain.DynamicToolResult{
		Success: true,
		Content: []processdomain.DynamicToolContent{{Type: "inputText", Text: string(payload)}},
	}
}

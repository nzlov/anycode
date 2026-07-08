package graph

import (
	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
	diffapp "github.com/nzlov/anycode/internal/application/diff"
	eventapp "github.com/nzlov/anycode/internal/application/event"
	"github.com/nzlov/anycode/internal/application/port"
	projectapp "github.com/nzlov/anycode/internal/application/project"
	questionapp "github.com/nzlov/anycode/internal/application/question"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	workflowapp "github.com/nzlov/anycode/internal/application/workflow"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	"github.com/nzlov/anycode/internal/domain/gitdiff"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	questiondomain "github.com/nzlov/anycode/internal/domain/question"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	workflowdomain "github.com/nzlov/anycode/internal/domain/workflow"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph/model"
)

func mapProject(dto projectapp.DTO) *model.Project {
	return &model.Project{
		ID:                string(dto.ID),
		Name:              dto.Name,
		Path:              dto.Path,
		IsGit:             dto.IsGit,
		DefaultWorkflowID: stringPtr(dto.DefaultWorkflowID),
		GitState:          mapGitState(dto.GitState),
		CreatedAt:         dto.CreatedAt,
		UpdatedAt:         dto.UpdatedAt,
	}
}

func mapGitState(state projectdomain.GitState) *model.GitState {
	branches := make([]*model.GitBranch, 0, len(state.Branches))
	for _, branch := range state.Branches {
		branches = append(branches, &model.GitBranch{Name: branch.Name, IsCurrent: branch.IsCurrent})
	}
	return &model.GitState{
		IsRepository:  state.IsRepository,
		CurrentBranch: state.CurrentBranch,
		Branches:      branches,
		ErrorCode:     state.ErrorCode,
		ErrorMessage:  state.ErrorMessage,
	}
}

func mapDirectoryPage(dto projectapp.DirectoryPageDTO) *model.DirectoryPage {
	entries := make([]*model.DirectoryEntry, 0, len(dto.Entries))
	for _, entry := range dto.Entries {
		entries = append(entries, &model.DirectoryEntry{
			Name:      entry.Name,
			Path:      entry.Path,
			IsDir:     entry.IsDir,
			IsGit:     entry.IsGit,
			CanRead:   entry.CanRead,
			ErrorCode: entry.ErrorCode,
		})
	}
	return &model.DirectoryPage{Path: dto.Path, Parent: dto.Parent, Entries: entries}
}

func mapSession(dto sessionapp.DTO) *model.Session {
	return &model.Session{
		ID:               string(dto.ID),
		ProjectID:        string(dto.ProjectID),
		Requirement:      dto.Requirement,
		Mode:             string(dto.Mode),
		Status:           string(dto.Status),
		Priority:         string(dto.Priority),
		BaseBranch:       dto.BaseBranch,
		WorktreeBranch:   dto.WorktreeBranch,
		WorktreePath:     dto.WorktreePath,
		CodexSessionID:   dto.CodexSessionID,
		Config:           mapSessionConfig(dto.Config),
		AvailableActions: dto.AvailableActions,
		LastRunAt:        dto.LastRunAt,
		CreatedAt:        dto.CreatedAt,
		UpdatedAt:        dto.UpdatedAt,
	}
}

func mapSessionCardPage(page port.Page[sessionapp.CardDTO]) *model.SessionCardPage {
	items := make([]*model.SessionCard, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, mapSessionCard(item))
	}
	return &model.SessionCardPage{Items: items, PageInfo: mapPageInfo(page.Page, page.PageSize, page.Total, page.NextCursor)}
}

func mapSessionCard(dto sessionapp.CardDTO) *model.SessionCard {
	attachments := make([]*model.SessionAttachment, 0, len(dto.Attachments))
	for _, attachment := range dto.Attachments {
		attachments = append(attachments, mapSessionAttachment(attachment))
	}
	return &model.SessionCard{
		ID:                 string(dto.ID),
		ProjectID:          string(dto.ProjectID),
		ProjectName:        dto.ProjectName,
		Requirement:        dto.Requirement,
		RequirementSummary: dto.RequirementSummary,
		Mode:               string(dto.Mode),
		Status:             string(dto.Status),
		Priority:           string(dto.Priority),
		BaseBranch:         dto.BaseBranch,
		WorktreeBranch:     dto.WorktreeBranch,
		CurrentNodeTitle:   dto.CurrentNodeTitle,
		PendingQuestion:    dto.PendingQuestion,
		TodoList:           mapTodoList(dto.TodoList),
		Attachments:        attachments,
		AvailableActions:   dto.AvailableActions,
		LastRunAt:          dto.LastRunAt,
		CreatedAt:          dto.CreatedAt,
		UpdatedAt:          dto.UpdatedAt,
	}
}

func mapTodoList(todoList sessiondomain.TodoList) *model.TodoList {
	if todoList.Total() == 0 {
		return nil
	}
	items := make([]*model.TodoItem, 0, len(todoList.Items))
	for _, item := range todoList.Items {
		items = append(items, &model.TodoItem{
			Text:      item.Text,
			Completed: item.Completed,
		})
	}
	return &model.TodoList{
		Completed: todoList.Completed(),
		Total:     todoList.Total(),
		Items:     items,
	}
}

func mapSessionDetail(dto sessionapp.DetailDTO) *model.SessionDetail {
	attachments := make([]*model.SessionAttachment, 0, len(dto.Attachments))
	for _, attachment := range dto.Attachments {
		attachments = append(attachments, mapSessionAttachment(attachment))
	}
	appends := make([]*model.PromptAppend, 0, len(dto.PromptAppends))
	for _, appendDTO := range dto.PromptAppends {
		appends = append(appends, mapPromptAppend(appendDTO))
	}
	return &model.SessionDetail{
		ID:               string(dto.ID),
		ProjectID:        string(dto.ProjectID),
		Requirement:      dto.Requirement,
		Mode:             string(dto.Mode),
		Status:           string(dto.Status),
		Priority:         string(dto.Priority),
		CloseReason:      stringPtr(dto.CloseReason),
		BaseBranch:       dto.BaseBranch,
		WorktreeBranch:   dto.WorktreeBranch,
		CurrentNodeTitle: dto.CurrentNodeTitle,
		WorktreePath:     dto.WorktreePath,
		CodexSessionID:   dto.CodexSessionID,
		Config:           mapSessionConfig(dto.Config),
		Attachments:      attachments,
		PromptAppends:    appends,
		AvailableActions: dto.AvailableActions,
		CanResume:        dto.CanResume,
		LastRunAt:        dto.LastRunAt,
		CreatedAt:        dto.CreatedAt,
		UpdatedAt:        dto.UpdatedAt,
	}
}

func mapSessionConfig(config sessiondomain.Config) *model.SessionConfig {
	return &model.SessionConfig{
		CodexModel:      config.CodexModel,
		ReasoningEffort: config.ReasoningEffort,
		PermissionMode:  config.PermissionMode,
	}
}

func mapSessionAttachment(attachment sessiondomain.SessionAttachment) *model.SessionAttachment {
	return &model.SessionAttachment{
		ID:          string(attachment.ID),
		SessionID:   string(attachment.SessionID),
		Kind:        attachment.Kind,
		Filename:    attachment.Filename,
		MimeType:    attachment.MimeType,
		Size:        attachment.Size,
		Previewable: attachment.Previewable,
		CreatedAt:   attachment.CreatedAt,
	}
}

func mapPromptAppend(dto sessionapp.PromptAppendDTO) *model.PromptAppend {
	attachments := make([]*model.SessionAttachment, 0, len(dto.Attachments))
	for _, attachment := range dto.Attachments {
		attachments = append(attachments, mapSessionAttachment(attachment))
	}
	return &model.PromptAppend{
		ID:          dto.ID,
		SessionID:   string(dto.SessionID),
		Body:        dto.Body,
		Attachments: attachments,
		CreatedAt:   dto.CreatedAt,
	}
}

func mapAttachment(dto attachmentapp.AttachmentDTO) *model.Attachment {
	return &model.Attachment{
		ID:          dto.ID,
		Filename:    dto.Filename,
		MimeType:    dto.MimeType,
		Size:        dto.Size,
		Previewable: dto.Previewable,
	}
}

func mapEventPage(page port.Page[eventapp.DTO]) *model.SessionEventPage {
	items := make([]*model.SessionEvent, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, mapEvent(item))
	}
	return &model.SessionEventPage{Items: items, PageInfo: mapPageInfo(page.Page, page.PageSize, page.Total, page.NextCursor)}
}

func mapEvent(dto eventapp.DTO) *model.SessionEvent {
	return &model.SessionEvent{
		ID:        string(dto.ID),
		Scope:     mapEventScope(dto.Scope),
		SessionID: stringPtr(dto.SessionID),
		Type:      dto.Type,
		Payload:   dto.Payload,
		CreatedAt: dto.CreatedAt,
	}
}

func mapEventScope(scope eventdomain.Scope) *model.EventScope {
	return &model.EventScope{
		SessionID: stringPtr(scope.SessionID),
		ProjectID: scope.ProjectID,
	}
}

func mapSessionDiff(dto diffapp.SessionDiffDTO) *model.SessionDiff {
	allDiff := make([]*model.FileDiff, 0, len(dto.AllDiff))
	for _, fileDiff := range dto.AllDiff {
		allDiff = append(allDiff, mapFileDiff(fileDiff))
	}
	return &model.SessionDiff{
		Mode:      dto.Mode,
		FilePath:  dto.FilePath,
		Files:     mapDiffFilePage(dto.Files),
		FileDiff:  mapFileDiffPtr(dto.FileDiff),
		AllDiff:   allDiff,
		Available: dto.Available,
	}
}

func mapCommitHistory(dto diffapp.CommitHistoryDTO) *model.SessionCommitHistory {
	return &model.SessionCommitHistory{
		Commits:   mapCommitRecordPage(dto.Commits),
		Available: dto.Available,
	}
}

func mapCommitRecordPage(page port.Page[gitdiff.CommitRecord]) *model.CommitRecordPage {
	items := make([]*model.CommitRecord, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, &model.CommitRecord{
			Hash:        item.Hash,
			ShortHash:   item.ShortHash,
			Subject:     item.Subject,
			AuthorName:  item.AuthorName,
			AuthorEmail: item.AuthorEmail,
			CreatedAt:   item.CreatedAt,
		})
	}
	return &model.CommitRecordPage{Items: items, PageInfo: mapPageInfo(page.Page, page.PageSize, page.Total, page.NextCursor)}
}

func mapDiffFilePage(page port.Page[gitdiff.DiffFile]) *model.DiffFilePage {
	items := make([]*model.DiffFile, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, mapDiffFile(item))
	}
	return &model.DiffFilePage{Items: items, PageInfo: mapPageInfo(page.Page, page.PageSize, page.Total, page.NextCursor)}
}

func mapFileDiffPtr(diff *gitdiff.FileDiff) *model.FileDiff {
	if diff == nil {
		return nil
	}
	return mapFileDiff(*diff)
}

func mapFileDiff(diff gitdiff.FileDiff) *model.FileDiff {
	hunks := make([]*model.DiffHunk, 0, len(diff.Hunks))
	for _, hunk := range diff.Hunks {
		lines := make([]*model.DiffLine, 0, len(hunk.Lines))
		for _, line := range hunk.Lines {
			lines = append(lines, &model.DiffLine{Kind: line.Kind, Content: line.Content})
		}
		hunks = append(hunks, &model.DiffHunk{
			Header:   hunk.Header,
			OldStart: hunk.OldStart,
			NewStart: hunk.NewStart,
			Lines:    lines,
		})
	}
	return &model.FileDiff{File: mapDiffFile(diff.File), Hunks: hunks}
}

func mapDiffFile(file gitdiff.DiffFile) *model.DiffFile {
	return &model.DiffFile{Path: file.Path, Status: file.Status, Additions: file.Additions, Deletions: file.Deletions}
}

func mapWorkflowDefinition(dto workflowapp.DefinitionDTO) *model.WorkflowDefinition {
	return &model.WorkflowDefinition{
		ID:        string(dto.ID),
		ProjectID: string(dto.ProjectID),
		Name:      dto.Name,
		Version:   dto.Version,
		Graph:     mapWorkflowGraph(dto.Graph),
		Active:    dto.Active,
	}
}

func buildWorkflowGraph(input *model.WorkflowGraphInput) workflowdomain.Graph {
	if input == nil {
		return workflowdomain.Graph{}
	}
	nodes := make([]workflowdomain.Node, 0, len(input.Nodes))
	for _, node := range input.Nodes {
		if node == nil {
			continue
		}
		nodes = append(nodes, workflowdomain.Node{
			ID:           node.ID,
			Type:         node.Type,
			Title:        node.Title,
			Prompt:       stringValue(node.Prompt, ""),
			Position:     buildWorkflowNodePosition(node.Position),
			OutputFields: buildWorkflowOutputFields(node.OutputFields),
			Approval:     buildApprovalConfig(node.Approval),
			Retry:        buildRetryConfig(node.Retry),
			Merge:        buildMergeConfig(node.Merge),
		})
	}
	edges := make([]workflowdomain.Edge, 0, len(input.Edges))
	for _, edge := range input.Edges {
		if edge == nil {
			continue
		}
		edges = append(edges, workflowdomain.Edge{
			From:      edge.From,
			To:        edge.To,
			Priority:  intValue(edge.Priority, 0),
			Condition: buildWorkflowCondition(edge.Condition),
		})
	}
	return workflowdomain.Graph{Nodes: nodes, Edges: edges}
}

func buildWorkflowNodePosition(input *model.WorkflowNodePositionInput) workflowdomain.Position {
	if input == nil {
		return workflowdomain.Position{}
	}
	return workflowdomain.Position{X: input.X, Y: input.Y}
}

func buildApprovalConfig(input *model.ApprovalConfigInput) workflowdomain.ApprovalConfig {
	if input == nil {
		return workflowdomain.ApprovalConfig{}
	}
	return workflowdomain.ApprovalConfig{BeforeRun: boolValue(input.BeforeRun), AfterRun: boolValue(input.AfterRun)}
}

func buildRetryConfig(input *model.RetryConfigInput) workflowdomain.RetryConfig {
	if input == nil {
		return workflowdomain.RetryConfig{}
	}
	return workflowdomain.RetryConfig{MaxAttempts: intValue(input.MaxAttempts, 0)}
}

func buildMergeConfig(input *model.MergeConfigInput) *workflowdomain.MergeConfig {
	if input == nil {
		return nil
	}
	return &workflowdomain.MergeConfig{Strategy: input.Strategy}
}

func buildWorkflowOutputFields(input []*model.WorkflowOutputFieldInput) []workflowdomain.OutputField {
	fields := make([]workflowdomain.OutputField, 0, len(input))
	for _, field := range input {
		if field == nil {
			continue
		}
		fields = append(fields, workflowdomain.OutputField{
			Key:         field.Key,
			Description: stringValue(field.Description, ""),
			ValueType:   stringValue(field.ValueType, ""),
		})
	}
	return fields
}

func buildWorkflowCondition(input *model.WorkflowConditionInput) workflowdomain.Condition {
	if input == nil {
		return workflowdomain.Condition{}
	}
	all := make([]workflowdomain.Condition, 0, len(input.All))
	for _, child := range input.All {
		all = append(all, buildWorkflowCondition(child))
	}
	any := make([]workflowdomain.Condition, 0, len(input.Any))
	for _, child := range input.Any {
		any = append(any, buildWorkflowCondition(child))
	}
	return workflowdomain.Condition{
		Mode:  stringValue(input.Mode, ""),
		Field: stringValue(input.Field, ""),
		Op:    stringValue(input.Op, ""),
		Value: input.Value,
		Expr:  stringValue(input.Expr, ""),
		All:   all,
		Any:   any,
		Not:   buildWorkflowConditionPtr(input.Not),
	}
}

func buildWorkflowConditionPtr(input *model.WorkflowConditionInput) *workflowdomain.Condition {
	if input == nil {
		return nil
	}
	condition := buildWorkflowCondition(input)
	return &condition
}

func mapWorkflowGraph(graph workflowdomain.Graph) *model.WorkflowGraph {
	nodes := make([]*model.WorkflowNode, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodes = append(nodes, &model.WorkflowNode{
			ID:           node.ID,
			Type:         node.Type,
			Title:        node.Title,
			Prompt:       node.Prompt,
			Position:     &model.WorkflowNodePosition{X: node.Position.X, Y: node.Position.Y},
			OutputFields: mapWorkflowOutputFields(node.OutputFields),
			Approval:     &model.ApprovalConfig{BeforeRun: node.Approval.BeforeRun, AfterRun: node.Approval.AfterRun},
			Retry:        &model.RetryConfig{MaxAttempts: node.Retry.MaxAttempts},
			Merge:        mapMergeConfig(node.Merge),
		})
	}
	edges := make([]*model.WorkflowEdge, 0, len(graph.Edges))
	for _, edge := range graph.Edges {
		edges = append(edges, &model.WorkflowEdge{
			From:      edge.From,
			To:        edge.To,
			Priority:  edge.Priority,
			Condition: mapWorkflowCondition(edge.Condition),
		})
	}
	return &model.WorkflowGraph{Nodes: nodes, Edges: edges}
}

func mapWorkflowRun(dto workflowapp.RunDTO) *model.WorkflowRun {
	return &model.WorkflowRun{
		ID:            string(dto.ID),
		SessionID:     string(dto.SessionID),
		Status:        string(dto.Status),
		CurrentNodeID: dto.CurrentNodeID,
		Context:       dto.Context.Values,
	}
}

func mapSessionWorkflowRun(dto sessionapp.WorkflowRunDTO) *model.WorkflowRun {
	return &model.WorkflowRun{
		ID:            string(dto.ID),
		SessionID:     string(dto.SessionID),
		Status:        dto.Status,
		CurrentNodeID: dto.CurrentNodeID,
		Context:       dto.Context,
	}
}

func mapQuestionBatch(dto questionapp.BatchDTO) *model.QuestionBatch {
	questions := make([]*model.Question, 0, len(dto.Questions))
	for _, question := range dto.Questions {
		questions = append(questions, mapQuestion(question))
	}
	return &model.QuestionBatch{
		ID:        string(dto.ID),
		SessionID: string(dto.SessionID),
		Status:    string(dto.Status),
		Questions: questions,
	}
}

func buildQuestionAnswers(inputs []*model.QuestionAnswerInput) []questiondomain.Answer {
	answers := make([]questiondomain.Answer, 0, len(inputs))
	for _, input := range inputs {
		if input == nil {
			continue
		}
		answers = append(answers, questiondomain.Answer{
			QuestionID:       questiondomain.QuestionID(input.QuestionID),
			SelectedOptionID: questionOptionIDPtr(input.SelectedOptionID),
			CustomAnswer:     stringValue(input.CustomAnswer, ""),
			Payload:          input.Payload,
		})
	}
	return answers
}

func questionOptionIDPtr(value *string) *questiondomain.OptionID {
	if value == nil {
		return nil
	}
	id := questiondomain.OptionID(*value)
	return &id
}

func mapQuestion(question questiondomain.Question) *model.Question {
	options := make([]*model.QuestionOption, 0, len(question.Options))
	for _, option := range question.Options {
		options = append(options, &model.QuestionOption{
			ID:          string(option.ID),
			Label:       option.Label,
			Description: option.Description,
			Payload:     nonNilMap(option.Payload),
		})
	}
	return &model.Question{
		ID:               string(question.ID),
		BatchID:          string(question.BatchID),
		Title:            question.Title,
		Body:             question.Body,
		Type:             question.Type,
		Options:          options,
		AllowCustom:      question.AllowCustom,
		SelectedOptionID: stringPtr(question.SelectedOptionID),
		CustomAnswer:     question.CustomAnswer,
		Answer:           nonNilMap(question.Answer),
		Status:           question.Status,
	}
}

func nonNilMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func mapPageInfo(page int, pageSize int, total int, nextCursor string) *model.PageInfo {
	return &model.PageInfo{Page: page, PageSize: pageSize, Total: total, NextCursor: nextCursor}
}

func stringPtr[T ~string](value *T) *string {
	if value == nil {
		return nil
	}
	s := string(*value)
	return &s
}

func intValue(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func stringValue(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func mapMergeConfig(config *workflowdomain.MergeConfig) *model.MergeConfig {
	if config == nil {
		return nil
	}
	return &model.MergeConfig{Strategy: config.Strategy}
}

func mapWorkflowOutputFields(fields []workflowdomain.OutputField) []*model.WorkflowOutputField {
	output := make([]*model.WorkflowOutputField, 0, len(fields))
	for _, field := range fields {
		output = append(output, &model.WorkflowOutputField{
			Key:         field.Key,
			Description: field.Description,
			ValueType:   field.ValueType,
		})
	}
	return output
}

func mapWorkflowCondition(condition workflowdomain.Condition) *model.WorkflowCondition {
	all := make([]*model.WorkflowCondition, 0, len(condition.All))
	for _, child := range condition.All {
		all = append(all, mapWorkflowCondition(child))
	}
	any := make([]*model.WorkflowCondition, 0, len(condition.Any))
	for _, child := range condition.Any {
		any = append(any, mapWorkflowCondition(child))
	}
	return &model.WorkflowCondition{
		Mode:  condition.Mode,
		Field: condition.Field,
		Op:    condition.Op,
		Value: condition.Value,
		Expr:  condition.Expr,
		All:   all,
		Any:   any,
		Not:   mapWorkflowConditionPtr(condition.Not),
	}
}

func mapWorkflowConditionPtr(condition *workflowdomain.Condition) *model.WorkflowCondition {
	if condition == nil {
		return nil
	}
	return mapWorkflowCondition(*condition)
}

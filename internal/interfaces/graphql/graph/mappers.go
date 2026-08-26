package graph

import (
	"strings"
	"time"

	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
	diffapp "github.com/nzlov/anycode/internal/application/diff"
	mindmapapp "github.com/nzlov/anycode/internal/application/mindmap"
	notificationapp "github.com/nzlov/anycode/internal/application/notification"
	"github.com/nzlov/anycode/internal/application/port"
	projectapp "github.com/nzlov/anycode/internal/application/project"
	questionapp "github.com/nzlov/anycode/internal/application/question"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	sessioneventapp "github.com/nzlov/anycode/internal/application/sessionevent"
	settingapp "github.com/nzlov/anycode/internal/application/setting"
	statisticsapp "github.com/nzlov/anycode/internal/application/statistics"
	timelineapp "github.com/nzlov/anycode/internal/application/timeline"
	workflowapp "github.com/nzlov/anycode/internal/application/workflow"
	"github.com/nzlov/anycode/internal/domain/gitdiff"
	mindmapdomain "github.com/nzlov/anycode/internal/domain/mindmap"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	questiondomain "github.com/nzlov/anycode/internal/domain/question"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	settingdomain "github.com/nzlov/anycode/internal/domain/setting"
	workflowdomain "github.com/nzlov/anycode/internal/domain/workflow"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph/model"
)

// GLUE: GraphQL transport models map the statistics read DTO at the interface boundary.
func mapStatisticsDashboard(value statisticsapp.DashboardDTO) *model.StatisticsDashboard {
	return &model.StatisticsDashboard{
		Today: mapStatisticsMetrics(value.Today),
		Total: mapStatisticsMetrics(value.Total),
		ByDay: mapStatisticsTimeline(value.ByDay),
	}
}

func mapStatisticsTimeline(values []statisticsapp.TimelineBucketDTO) []*model.StatisticsTimelineBucket {
	result := make([]*model.StatisticsTimelineBucket, 0, len(values))
	for _, value := range values {
		projects := make([]*model.StatisticsProjectMetrics, 0, len(value.Projects))
		for _, project := range value.Projects {
			projects = append(projects, &model.StatisticsProjectMetrics{
				Key: project.Key, Label: project.Label, Metrics: mapStatisticsMetrics(project.Metrics),
			})
		}
		result = append(result, &model.StatisticsTimelineBucket{
			Key: value.Key, Label: value.Label, Projects: projects,
		})
	}
	return result
}

func mapStatisticsMetrics(value statisticsapp.MetricsDTO) *model.StatisticsMetrics {
	return &model.StatisticsMetrics{
		CreatedCards: value.CreatedCards,
		ClosedCards:  value.ClosedCards,
		FilesChanged: value.FilesChanged,
		TotalTokens:  value.TotalTokens,
	}
}

func mapCodexModelOptions(items []processdomain.CodexModel) []*model.CodexModelOption {
	options := make([]*model.CodexModelOption, 0, len(items))
	for _, item := range items {
		efforts := make([]*model.CodexReasoningEffortOption, 0, len(item.SupportedReasoningLevels))
		for _, effort := range item.SupportedReasoningLevels {
			efforts = append(efforts, &model.CodexReasoningEffortOption{
				Label:       effort.Effort,
				Value:       effort.Effort,
				Description: effort.Description,
			})
		}
		options = append(options, &model.CodexModelOption{
			Label:                  item.DisplayName,
			Value:                  item.Slug,
			DefaultReasoningEffort: item.DefaultReasoningLevel,
			ReasoningEfforts:       efforts,
		})
	}
	return options
}

func mapQuickCommand(dto settingapp.QuickCommandDTO) *model.QuickCommand {
	command := &model.QuickCommand{
		ID:        string(dto.ID),
		Content:   dto.Content,
		CreatedAt: dto.CreatedAt,
	}
	if dto.ProjectID != nil {
		projectID := string(*dto.ProjectID)
		command.ProjectID = &projectID
	}
	return command
}

func mapGeneralSettings(dto settingapp.GeneralSettingsDTO) *model.GeneralSettings {
	return &model.GeneralSettings{
		AgentMaxConcurrent: dto.AgentMaxConcurrent, AgentWritableRoots: append([]string{}, dto.AgentWritableRoots...),
		SendShortcut:   string(dto.SendShortcut),
		MindMapEnabled: dto.MindMapEnabled, MindMapMode: string(dto.MindMapMode), MindMapModel: dto.MindMapModel,
		MindMapLayout:          string(dto.MindMapLayout),
		MindMapReasoningEffort: dto.MindMapReasoningEffort, MindMapMaxConcurrent: dto.MindMapMaxConcurrent,
	}
}

func mapAppearanceSettings(dto settingapp.AppearanceSettingsDTO) *model.AppearanceSettings {
	return &model.AppearanceSettings{
		BackgroundType:       model.AppearanceBackgroundType(strings.ToUpper(string(dto.BackgroundType))),
		SolidTheme:           model.AppearanceSolidTheme(strings.ToUpper(string(dto.SolidTheme))),
		BackgroundMask:       dto.BackgroundMask,
		WallpaperColorScheme: wallpaperColorSchemeToModel(dto.WallpaperColorScheme),
		WallpaperID:          dto.WallpaperID,
		WallpaperFilename:    dto.WallpaperFilename,
	}
}

func wallpaperColorSchemeFromModel(scheme model.WallpaperColorScheme) settingdomain.WallpaperColorScheme {
	return settingdomain.WallpaperColorScheme(strings.ToLower(string(scheme)))
}

func wallpaperColorSchemeToModel(scheme settingdomain.WallpaperColorScheme) model.WallpaperColorScheme {
	return model.WallpaperColorScheme(strings.ToUpper(string(scheme)))
}

func mapQuickCommandPage(page port.Page[settingapp.QuickCommandDTO]) *model.QuickCommandPage {
	items := make([]*model.QuickCommand, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, mapQuickCommand(item))
	}
	return &model.QuickCommandPage{
		Items:    items,
		PageInfo: mapPageInfo(page.Page, page.PageSize, page.Total, page.NextCursor),
	}
}

func mapProject(dto projectapp.DTO) *model.Project {
	return &model.Project{
		ID:                  string(dto.ID),
		Name:                dto.Name,
		Path:                dto.Path,
		IsGit:               dto.IsGit,
		WorktreeInitCommand: dto.WorktreeInitCommand,
		MindMapEnabled:      dto.MindMapEnabled,
		DefaultWorkflowID:   stringPtr(dto.DefaultWorkflowID),
		GitState:            mapGitState(dto.GitState),
		CreatedAt:           dto.CreatedAt,
		UpdatedAt:           dto.UpdatedAt,
	}
}

func mapMindMapGraphPage(dto mindmapapp.GraphPageDTO) *model.MindMapGraphPage {
	nodes := make([]*model.MindMapNode, 0, len(dto.Nodes))
	for _, node := range dto.Nodes {
		nodes = append(nodes, &model.MindMapNode{
			ID: string(node.ID), Title: node.Title, ChangeType: string(node.ChangeType),
		})
	}
	edges := make([]*model.MindMapEdge, 0, len(dto.Edges))
	for _, edge := range dto.Edges {
		edges = append(edges, &model.MindMapEdge{ID: string(edge.ID), SourceID: string(edge.SourceID), TargetID: string(edge.TargetID), Label: edge.Label})
	}
	var sessionID *string
	if dto.SessionID != "" {
		value := string(dto.SessionID)
		sessionID = &value
	}
	var nextNodeCursor, nextEdgeCursor *string
	if dto.NextNodeCursor != "" {
		value := string(dto.NextNodeCursor)
		nextNodeCursor = &value
	}
	if dto.NextEdgeCursor != "" {
		value := string(dto.NextEdgeCursor)
		nextEdgeCursor = &value
	}
	return &model.MindMapGraphPage{
		ProjectID: string(dto.ProjectID), SessionID: sessionID, Nodes: nodes, Edges: edges, UpdatedAt: dto.UpdatedAt,
		NextNodeCursor: nextNodeCursor, NextEdgeCursor: nextEdgeCursor,
	}
}

func mapMindMapUpdate(dto mindmapapp.GraphDTO) *model.MindMapUpdateEvent {
	var sessionID *string
	if dto.SessionID != "" {
		value := string(dto.SessionID)
		sessionID = &value
	}
	return &model.MindMapUpdateEvent{ProjectID: string(dto.ProjectID), SessionID: sessionID, UpdatedAt: dto.UpdatedAt}
}

func mapMindMapCard(dto mindmapapp.CardDTO) *model.MindMapCard {
	var taskID *string
	if dto.TaskID != "" {
		value := string(dto.TaskID)
		taskID = &value
	}
	nodes := make([]*model.MindMapNode, 0, len(dto.Nodes))
	for _, node := range dto.Nodes {
		nodes = append(nodes, &model.MindMapNode{
			ID: string(node.ID), Title: node.Title, ChangeType: string(node.ChangeType),
		})
	}
	edges := make([]*model.MindMapEdge, 0, len(dto.Edges))
	for _, edge := range dto.Edges {
		edges = append(edges, &model.MindMapEdge{
			ID: string(edge.ID), SourceID: string(edge.SourceID), TargetID: string(edge.TargetID), Label: edge.Label,
		})
	}
	modifiedNodeIDs := make([]string, 0, len(dto.ModifiedNodeIDs))
	for _, id := range dto.ModifiedNodeIDs {
		modifiedNodeIDs = append(modifiedNodeIDs, string(id))
	}
	deletedNodeIDs := make([]string, 0, len(dto.DeletedNodeIDs))
	for _, id := range dto.DeletedNodeIDs {
		deletedNodeIDs = append(deletedNodeIDs, string(id))
	}
	deletedEdgeIDs := make([]string, 0, len(dto.DeletedEdgeIDs))
	for _, id := range dto.DeletedEdgeIDs {
		deletedEdgeIDs = append(deletedEdgeIDs, string(id))
	}
	return &model.MindMapCard{
		SessionID: string(dto.SessionID), Requirement: dto.Requirement, UpdatedAt: dto.UpdatedAt,
		HasChanges: dto.HasChanges,
		TaskID:     taskID, TaskStatus: string(dto.TaskStatus), TaskError: dto.TaskError,
		Nodes: nodes, Edges: edges, ModifiedNodeIds: modifiedNodeIDs, DeletedNodeIds: deletedNodeIDs, DeletedEdgeIds: deletedEdgeIDs,
	}
}

func mapMindMapNodeDetail(node mindmapapp.NodeDTO) *model.MindMapNodeDetail {
	return &model.MindMapNodeDetail{
		ID: string(node.ID), Title: node.Title, Content: node.Content, Files: mapMindMapNodeFiles(node.Files),
	}
}

// GLUE: GraphQL uses nullable session IDs while the application uses an empty domain ID for main-graph matches.
func mapMindMapSearchResult(dto mindmapapp.ProjectSearchResultDTO) *model.MindMapSearchResult {
	matches := make([]*model.MindMapSearchMatch, 0, len(dto.Matches))
	for _, match := range dto.Matches {
		var sessionID *string
		if match.SessionID != "" {
			value := string(match.SessionID)
			sessionID = &value
		}
		matches = append(matches, &model.MindMapSearchMatch{NodeID: string(match.NodeID), SessionID: sessionID})
	}
	return &model.MindMapSearchResult{ProjectID: string(dto.ProjectID), Query: dto.Query, Matches: matches}
}

// GLUE: GraphQL owns transport models while mindmap owns node file references; remove when gqlgen can bind the domain value directly.
func mapMindMapNodeFiles(files []mindmapdomain.NodeFile) []*model.MindMapNodeFile {
	result := make([]*model.MindMapNodeFile, len(files))
	for index, item := range files {
		result[index] = &model.MindMapNodeFile{
			File: item.File, Method: item.Method, StartLine: item.StartLine, EndLine: item.EndLine,
		}
	}
	return result
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
		WorktreeCleanup:  mapWorktreeCleanup(dto.WorktreeCleanup),
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
		TerminalSummary:    mapTerminalSummary(dto.TerminalSummary),
		TodoList:           mapTodoList(dto.TodoList),
		ArtifactCount:      dto.ArtifactCount,
		FilesChanged:       dto.FilesChanged,
		Usage:              mapSessionUsage(dto.Usage),
		Attachments:        attachments,
		AvailableActions:   dto.AvailableActions,
		LastRunAt:          dto.LastRunAt,
		CreatedAt:          dto.CreatedAt,
		UpdatedAt:          dto.UpdatedAt,
	}
}

func mapTerminalSummary(dto *sessionapp.TerminalSummaryDTO) *model.TerminalSummary {
	if dto == nil {
		return nil
	}
	return &model.TerminalSummary{
		CurrentDirectory: dto.CurrentDirectory,
		Commands:         append([]string(nil), dto.Commands...),
	}
}

func mapPendingApproval(dto *sessionapp.PendingApprovalDTO) *model.PendingApproval {
	if dto == nil {
		return nil
	}
	var nodeRunID *string
	if dto.NodeRunID != "" {
		value := dto.NodeRunID
		nodeRunID = &value
	}
	return &model.PendingApproval{
		SessionID:        string(dto.SessionID),
		NodeID:           dto.NodeID,
		NodeRunID:        nodeRunID,
		CurrentNodeTitle: dto.CurrentNodeTitle,
		Phase:            dto.Phase,
		Result:           dto.Result,
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
	var forkedFromSessionID *string
	if dto.ForkedFromSessionID != "" {
		value := string(dto.ForkedFromSessionID)
		forkedFromSessionID = &value
	}
	attachments := make([]*model.SessionAttachment, 0, len(dto.Attachments))
	for _, attachment := range dto.Attachments {
		attachments = append(attachments, mapSessionAttachment(attachment))
	}
	appends := make([]*model.PromptAppend, 0, len(dto.PromptAppends))
	for _, appendDTO := range dto.PromptAppends {
		appends = append(appends, mapPromptAppend(appendDTO))
	}
	return &model.SessionDetail{
		ID:                  string(dto.ID),
		ProjectID:           string(dto.ProjectID),
		ProjectName:         dto.ProjectName,
		ForkedFromSessionID: forkedFromSessionID,
		Requirement:         dto.Requirement,
		Mode:                string(dto.Mode),
		Status:              string(dto.Status),
		Priority:            string(dto.Priority),
		CloseReason:         stringPtr(dto.CloseReason),
		BaseBranch:          dto.BaseBranch,
		WorktreeBranch:      dto.WorktreeBranch,
		CurrentNodeTitle:    dto.CurrentNodeTitle,
		PendingApproval:     mapPendingApproval(dto.PendingApproval),
		TodoList:            mapTodoList(dto.TodoList),
		ArtifactCount:       dto.ArtifactCount,
		FilesChanged:        dto.FilesChanged,
		WorktreePath:        dto.WorktreePath,
		WorktreeCleanup:     mapWorktreeCleanup(dto.WorktreeCleanup),
		CodexSessionID:      dto.CodexSessionID,
		Config:              mapSessionConfig(dto.Config),
		Usage:               mapSessionUsage(dto.Usage),
		Attachments:         attachments,
		PromptAppends:       appends,
		AvailableActions:    dto.AvailableActions,
		CanResume:           dto.CanResume,
		LastRunAt:           dto.LastRunAt,
		CreatedAt:           dto.CreatedAt,
		UpdatedAt:           dto.UpdatedAt,
	}
}

func mapWorktreeCleanup(dto sessionapp.WorktreeCleanupDTO) *model.WorktreeCleanup {
	cleanup := &model.WorktreeCleanup{
		Status:      string(dto.Status),
		Attempts:    dto.Attempts,
		RequestedAt: dto.RequestedAt,
		CompletedAt: dto.CompletedAt,
	}
	if dto.Error != nil {
		cleanup.Error = &model.WorktreeCleanupError{
			Code:      dto.Error.Code,
			Message:   dto.Error.Message,
			Retryable: dto.Error.Retryable,
		}
	}
	return cleanup
}

func mapSessionConfig(config sessiondomain.Config) *model.SessionConfig {
	return &model.SessionConfig{
		CodexModel:      config.CodexModel,
		ReasoningEffort: config.ReasoningEffort,
		PermissionMode:  config.PermissionMode,
		FastMode:        config.FastMode,
	}
}

func mapSessionAttachment(attachment sessiondomain.SessionAttachment) *model.SessionAttachment {
	return &model.SessionAttachment{
		ID:          string(attachment.ID),
		SessionID:   string(attachment.SessionID),
		Kind:        string(attachment.Kind),
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
	artifacts := make([]*model.SessionFile, 0, len(dto.Artifacts))
	for _, artifact := range dto.Artifacts {
		artifacts = append(artifacts, mapSessionFile(artifact))
	}
	annotations := make([]*model.PromptAnnotation, 0, len(dto.Annotations))
	for _, annotation := range dto.Annotations {
		annotations = append(annotations, mapPromptAnnotation(annotation))
	}
	return &model.PromptAppend{
		ID:          dto.ID,
		SessionID:   string(dto.SessionID),
		Body:        dto.Body,
		Attachments: attachments,
		Artifacts:   artifacts,
		Annotations: annotations,
		CreatedAt:   dto.CreatedAt,
	}
}

func mapPromptAnnotation(annotation sessiondomain.PromptAnnotation) *model.PromptAnnotation {
	marks := make([]*model.PromptAnnotationMark, 0, len(annotation.Marks))
	for _, mark := range annotation.Marks {
		mapped := &model.PromptAnnotationMark{ID: mark.ID, Kind: mark.Kind, Note: mark.Note}
		if mark.Kind == "image" {
			mapped.Shape = &mark.Shape
			mapped.X, mapped.Y = &mark.X, &mark.Y
			mapped.Width, mapped.Height = &mark.Width, &mark.Height
		} else {
			mapped.Start = mapPromptAnnotationPosition(mark.Start)
			mapped.End = mapPromptAnnotationPosition(mark.End)
			mapped.Quote = &mark.Quote
		}
		marks = append(marks, mapped)
	}
	references := make([]*model.PromptFileReference, 0, len(annotation.FileReferences))
	for _, reference := range annotation.FileReferences {
		mapped := &model.PromptFileReference{Kind: string(reference.Kind)}
		if reference.SessionFileID != "" {
			value := string(reference.SessionFileID)
			mapped.SessionFileID = &value
		}
		if reference.FilePath != "" {
			mapped.FilePath = &reference.FilePath
		}
		if reference.Version != "" {
			mapped.Version = &reference.Version
		}
		references = append(references, mapped)
	}
	return &model.PromptAnnotation{
		ID: annotation.ID, Source: annotation.Source, Content: annotation.Content,
		Marks: marks, FileReferences: references,
	}
}

func mapPromptAnnotationPosition(position *sessiondomain.PromptAnnotationPosition) *model.PromptAnnotationPosition {
	if position == nil {
		return nil
	}
	mapped := &model.PromptAnnotationPosition{Line: position.Line, Column: position.Column}
	if position.Revision != "" {
		mapped.Revision = &position.Revision
	}
	return mapped
}

func mapAttachment(dto attachmentapp.AttachmentDTO) *model.Attachment {
	return &model.Attachment{
		ID:          dto.ID,
		Kind:        dto.Kind,
		Filename:    dto.Filename,
		MimeType:    dto.MimeType,
		Size:        dto.Size,
		Previewable: dto.Previewable,
	}
}

func mapTranscriptPage(page timelineapp.Page) *model.TranscriptPage {
	items := make([]*model.TranscriptEvent, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, mapTranscriptEvent(item))
	}
	return &model.TranscriptPage{
		Events:       items,
		Usage:        mapTranscriptUsage(page.Usage),
		ProcessUsage: mapTranscriptUsageAttributions(page.ProcessUsage),
		NodeUsage:    mapTranscriptUsageAttributions(page.NodeUsage),
		PageInfo:     mapPageInfo(page.Page, page.PageSize, page.Total, page.NextCursor),
	}
}

func mapTranscriptUsageAttributions(values []timelineapp.UsageAttributionDTO) []*model.TranscriptUsageAttribution {
	result := make([]*model.TranscriptUsageAttribution, 0, len(values))
	for _, value := range values {
		usage := value.Usage
		result = append(result, &model.TranscriptUsageAttribution{
			ProcessRunID: optionalString(value.ProcessRunID),
			NodeRunID:    optionalString(value.NodeRunID),
			Usage:        mapTranscriptUsage(&usage),
		})
	}
	return result
}

func mapTranscriptEvent(dto timelineapp.DTO) *model.TranscriptEvent {
	correlationID := dto.CorrelationID
	createdAt, _ := time.Parse(time.RFC3339Nano, dto.OccurredAt)
	event := &model.TranscriptEvent{
		ID:            string(dto.ID),
		OrderKey:      dto.OrderKey,
		CorrelationID: optionalString(correlationID),
		Phase:         mapTranscriptPhase(dto.Phase),
		OccurredAt:    createdAt,
		Content:       mapTranscriptContent(dto.Content),
		Deferred:      mapTranscriptContentReference(dto.Deferred),
	}
	if dto.Group != nil {
		members := make([]*model.TranscriptEvent, 0, len(dto.Group.Members))
		for _, member := range dto.Group.Members {
			members = append(members, mapTranscriptEvent(member))
		}
		event.Group = &model.TranscriptEventGroup{
			Kind: dto.Group.Kind, Label: dto.Group.Label, Count: len(members), Members: members,
		}
	}
	return event
}

func mapTranscriptContentReference(value *processdomain.CodexContentReference) *model.TranscriptContentReference {
	if value == nil {
		return nil
	}
	return &model.TranscriptContentReference{ByteOffset: value.ByteOffset, ByteLength: value.ByteLength}
}

func mapSessionUpdateEvent(dto sessioneventapp.UpdateDTO) *model.SessionUpdateEvent {
	item := &model.SessionUpdateEvent{
		EventType:        dto.Type,
		SessionID:        string(dto.SessionID),
		ArtifactCount:    dto.ArtifactCount,
		FilesChanged:     dto.FilesChanged,
		AvailableActions: dto.AvailableActions,
		UpdatedAt:        dto.UpdatedAt,
	}
	if dto.OccurredAt != "" {
		occurredAt, err := time.Parse(time.RFC3339Nano, dto.OccurredAt)
		if err == nil {
			item.OccurredAt = &occurredAt
		}
	}
	if dto.Status != nil {
		item.Status = &model.SessionStatusUpdate{
			Status:           string(dto.Status.Status),
			CurrentNodeTitle: dto.Status.CurrentNodeTitle,
			AvailableActions: dto.Status.AvailableActions,
			UpdatedAt:        dto.Status.UpdatedAt,
		}
	}
	if dto.TodoList != nil {
		item.TodoList = mapTodoList(*dto.TodoList)
	}
	if dto.Priority != nil {
		priority := string(*dto.Priority)
		item.Priority = &priority
	}
	if dto.Config != nil {
		item.Config = mapSessionConfig(*dto.Config)
	}
	if dto.WorktreeCleanup != nil {
		item.WorktreeCleanup = mapWorktreeCleanup(*dto.WorktreeCleanup)
	}
	item.Usage = mapSessionUsage(dto.Usage)
	return item
}

func mapTranscriptContent(content processdomain.CodexEventContent) model.TranscriptContent {
	switch value := content.(type) {
	case processdomain.CodexMessageContent:
		return &model.TranscriptMessageContent{
			Role:   value.Role,
			Text:   value.Text,
			Format: mapTranscriptTextFormat(value.Format),
			Images: mapTranscriptImages(value.Images),
		}
	case processdomain.CodexReasoningContent:
		return &model.TranscriptReasoningContent{Text: value.Text}
	case processdomain.CodexCommandContent:
		kind := value.Kind
		if kind == "" {
			kind = processdomain.CodexCommandShell
		}
		commands := make([]*model.TranscriptCommandInvocation, 0, len(value.Commands))
		for _, command := range value.Commands {
			commands = append(commands, &model.TranscriptCommandInvocation{
				Command: command.Command, Workdir: command.Workdir, HasOutput: command.HasOutput,
				Output: command.Output, ExitCode: command.ExitCode, DurationMs: command.DurationMS,
			})
		}
		return &model.TranscriptCommandContent{Kind: string(kind), Commands: commands, DurationMs: value.DurationMS}
	case processdomain.CodexToolContent:
		return &model.TranscriptToolContent{
			QualifiedName: value.QualifiedName,
			Category:      value.Category,
			Input:         mapTranscriptStructuredText(value.Input),
			Output:        mapTranscriptStructuredText(value.Output),
			Images:        mapTranscriptImages(value.Images),
		}
	case processdomain.CodexFileChangeContent:
		changes := make([]*model.TranscriptFileChange, 0, len(value.Changes))
		for _, change := range value.Changes {
			changes = append(changes, &model.TranscriptFileChange{Kind: change.Kind, Path: change.Path, MovePath: change.MovePath, UnifiedDiff: change.UnifiedDiff})
		}
		return &model.TranscriptFileChangeContent{Changes: changes}
	case processdomain.CodexStatusContent:
		return &model.TranscriptStatusContent{Code: value.Code, Level: value.Level, Message: value.Message, Details: nonNilMap(value.Details)}
	case processdomain.CodexUnknownContent:
		return &model.TranscriptUnknownContent{RawType: value.RawType, Payload: nonNilMap(value.Payload)}
	default:
		return &model.TranscriptUnknownContent{RawType: "unsupported_content", Payload: map[string]any{}}
	}
}

func mapTranscriptStructuredText(value processdomain.CodexStructuredText) *model.TranscriptStructuredText {
	return &model.TranscriptStructuredText{Format: mapTranscriptTextFormat(value.Format), Text: value.Text}
}

func mapTranscriptImages(values []processdomain.CodexImage) []*model.TranscriptImage {
	items := make([]*model.TranscriptImage, 0, len(values))
	for _, value := range values {
		items = append(items, &model.TranscriptImage{Src: value.Source, Detail: value.Detail})
	}
	return items
}

func mapTranscriptUsage(value *timelineapp.TokenUsageDTO) *model.TranscriptTokenUsage {
	if value == nil {
		return nil
	}
	return &model.TranscriptTokenUsage{
		InputTokens:                  value.InputTokens,
		CachedInputTokens:            value.CachedInputTokens,
		OutputTokens:                 value.OutputTokens,
		ReasoningOutputTokens:        value.ReasoningOutputTokens,
		TotalTokens:                  value.TotalTokens,
		ContextWindow:                value.ContextWindow,
		CurrentInputTokens:           value.CurrentInputTokens,
		CurrentCachedInputTokens:     value.CurrentCachedInputTokens,
		CurrentOutputTokens:          value.CurrentOutputTokens,
		CurrentReasoningOutputTokens: value.CurrentReasoningOutputTokens,
		CurrentTotalTokens:           value.CurrentTotalTokens,
		CompactionCount:              value.CompactionCount,
	}
}

func mapSessionUsage(value *sessiondomain.TokenUsage) *model.TranscriptTokenUsage {
	if value == nil {
		return nil
	}
	return &model.TranscriptTokenUsage{
		InputTokens:                  value.InputTokens,
		CachedInputTokens:            value.CachedInputTokens,
		OutputTokens:                 value.OutputTokens,
		ReasoningOutputTokens:        value.ReasoningOutputTokens,
		TotalTokens:                  value.TotalTokens,
		ContextWindow:                value.ContextWindow,
		CurrentInputTokens:           value.CurrentInputTokens,
		CurrentCachedInputTokens:     value.CurrentCachedInputTokens,
		CurrentOutputTokens:          value.CurrentOutputTokens,
		CurrentReasoningOutputTokens: value.CurrentReasoningOutputTokens,
		CurrentTotalTokens:           value.CurrentTotalTokens,
		CompactionCount:              value.CompactionCount,
	}
}

func mapTranscriptPhase(value processdomain.CodexPhase) model.TranscriptEventPhase {
	switch value {
	case processdomain.CodexPhaseStarted:
		return model.TranscriptEventPhaseStarted
	case processdomain.CodexPhaseProgress:
		return model.TranscriptEventPhaseProgress
	case processdomain.CodexPhaseCompleted:
		return model.TranscriptEventPhaseCompleted
	case processdomain.CodexPhaseFailed:
		return model.TranscriptEventPhaseFailed
	case processdomain.CodexPhaseCancelled:
		return model.TranscriptEventPhaseCancelled
	default:
		return model.TranscriptEventPhaseStandalone
	}
}

func mapTranscriptTextFormat(value processdomain.CodexTextFormat) model.TranscriptTextFormat {
	switch value {
	case processdomain.CodexTextMarkdown:
		return model.TranscriptTextFormatMarkdown
	case processdomain.CodexTextJSON:
		return model.TranscriptTextFormatJSON
	case processdomain.CodexTextANSI:
		return model.TranscriptTextFormatAnsi
	default:
		return model.TranscriptTextFormatPlain
	}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func mapSessionDiff(dto diffapp.SessionDiffDTO) *model.SessionDiff {
	allDiff := make([]*model.FileDiff, 0, len(dto.AllDiff))
	for _, fileDiff := range dto.AllDiff {
		allDiff = append(allDiff, mapFileDiff(fileDiff))
	}
	return &model.SessionDiff{
		Mode:      dto.Mode,
		FilePath:  dto.FilePath,
		Files:     mapDiffFiles(dto.Files),
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

func mapDiffFiles(files []gitdiff.DiffFile) []*model.DiffFile {
	items := make([]*model.DiffFile, 0, len(files))
	for _, item := range files {
		items = append(items, mapDiffFile(item))
	}
	return items
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
			Header:          hunk.Header,
			OldStart:        hunk.OldStart,
			NewStart:        hunk.NewStart,
			CanExpandBefore: hunk.CanExpandBefore,
			CanExpandAfter:  hunk.CanExpandAfter,
			Lines:           lines,
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
		SessionID:     string(dto.SessionID),
		Status:        string(dto.Status),
		CurrentNodeID: dto.CurrentNodeID,
		Context:       dto.Context.Values,
	}
}

func mapSessionWorkflowRun(dto sessionapp.WorkflowRunDTO) *model.WorkflowRun {
	return &model.WorkflowRun{
		SessionID:     string(dto.SessionID),
		Status:        dto.Status,
		CurrentNodeID: dto.CurrentNodeID,
		Context:       dto.Context,
	}
}

func mapWebPushConfig(dto notificationapp.ConfigDTO) *model.WebPushConfig {
	return &model.WebPushConfig{Enabled: dto.Enabled, PublicKey: dto.PublicKey, ProxyURL: dto.ProxyURL}
}

func mapQuestionRequest(dto questionapp.RequestDTO) *model.QuestionRequest {
	questions := make([]*model.Question, 0, len(dto.Questions))
	for _, question := range dto.Questions {
		questions = append(questions, mapQuestion(question))
	}
	return &model.QuestionRequest{
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
	files := make([]*model.QuestionFile, 0, len(question.Files))
	for _, file := range question.Files {
		downloadURL := "/files/" + file.ID + "/download"
		var previewURL *string
		if file.PreviewKind != "none" {
			value := "/files/" + file.ID + "/preview"
			previewURL = &value
		}
		files = append(files, &model.QuestionFile{
			ID: file.ID, Filename: file.Filename, MimeType: file.MimeType, Size: file.Size,
			PreviewKind: file.PreviewKind, PreviewURL: previewURL, DownloadURL: downloadURL,
		})
	}
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
		RequestID:        string(question.RequestID),
		Body:             question.Body,
		Type:             question.Type,
		Files:            files,
		Options:          options,
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

func mapSessionFile(file sessiondomain.SessionAttachment) *model.SessionFile {
	downloadURL := "/files/" + string(file.ID) + "/download"
	var previewURL *string
	if file.PreviewKind != sessiondomain.PreviewKindNone {
		value := "/files/" + string(file.ID) + "/preview"
		previewURL = &value
	}
	return &model.SessionFile{
		ID:           string(file.ID),
		SessionID:    string(file.SessionID),
		Role:         string(file.Role),
		SourceType:   string(file.SourceType),
		ArtifactKind: string(file.ArtifactKind),
		LogicalPath:  file.LogicalPath,
		Filename:     file.Filename,
		MimeType:     file.MimeType,
		Size:         file.Size,
		PreviewKind:  string(file.PreviewKind),
		PreviewURL:   previewURL,
		DownloadURL:  downloadURL,
		CreatedAt:    file.CreatedAt,
	}
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

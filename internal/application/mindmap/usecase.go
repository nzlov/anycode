package mindmap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nzlov/anycode/internal/application/port"
	domain "github.com/nzlov/anycode/internal/domain/mindmap"
	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	settingdomain "github.com/nzlov/anycode/internal/domain/setting"
)

type UseCase interface {
	Get(ctx context.Context, input GetInput) (GraphDTO, error)
	ListCards(ctx context.Context, projectID domain.ProjectID) ([]CardDTO, error)
	Update(ctx context.Context, input UpdateInput) (GraphDTO, error)
	GetForSession(ctx context.Context, sessionID domain.SessionID) (GraphDTO, error)
	SearchForProcess(ctx context.Context, processRunID string, sessionID domain.SessionID, query string, limit int) (SearchResultDTO, error)
	UpdateForSession(ctx context.Context, sessionID domain.SessionID, operations []OperationInput) (GraphDTO, error)
	UpdateForTask(ctx context.Context, processRunID string, sessionID domain.SessionID, operations []OperationInput) (GraphDTO, error)
	UpdateForProcess(ctx context.Context, processRunID string, sessionID domain.SessionID, operations []OperationInput) (GraphDTO, error)
	RetryTask(ctx context.Context, taskID domain.TaskID) (CardDTO, error)
	Watch(ctx context.Context, input GetInput) (<-chan ChangeDTO, error)
}

type GetInput struct {
	ProjectID domain.ProjectID
	SessionID domain.SessionID
}

type UpdateInput struct {
	ProjectID  domain.ProjectID
	SessionID  domain.SessionID
	Operations []OperationInput
}

type OperationInput struct {
	Kind     domain.ChangeKind
	ID       string
	Title    *string
	Content  *string
	SourceID *domain.NodeID
	TargetID *domain.NodeID
	Label    *string
}

type GraphDTO struct {
	ProjectID domain.ProjectID
	SessionID domain.SessionID
	Nodes     []NodeDTO
	Edges     []EdgeDTO
	UpdatedAt time.Time
}

type NodeDTO struct {
	ID      domain.NodeID
	Title   string
	Content string
}

type EdgeDTO struct {
	ID       domain.EdgeID
	SourceID domain.NodeID
	TargetID domain.NodeID
	Label    string
}

type SearchResultDTO struct {
	ProjectID    domain.ProjectID
	SessionID    domain.SessionID
	Query        string
	Matches      []NodeMatchDTO
	RelatedNodes []NodeDTO
	Edges        []EdgeDTO
	TotalMatches int
	Truncated    bool
}

type NodeMatchDTO struct {
	Node          NodeDTO
	MatchedFields []string
}

type CardDTO struct {
	SessionID   domain.SessionID
	Requirement string
	UpdatedAt   time.Time
	TaskID      domain.TaskID
	TaskStatus  domain.TaskStatus
	TaskError   string
}

type ChangeDTO struct {
	ProjectID domain.ProjectID
	SessionID domain.SessionID
	UpdatedAt time.Time
}

type Service struct {
	repo          domain.Repository
	uow           port.UnitOfWork
	projects      projectdomain.Repository
	sessions      sessiondomain.Repository
	settings      settingdomain.MindMapConfigurationProvider
	now           func() time.Time
	generateID    func() (domain.ChangeID, error)
	scheduleQueue func()
	watchInterval time.Duration
}

func (s *Service) SetQueueScheduler(schedule func()) {
	s.scheduleQueue = schedule
}

func New(repo domain.Repository, projects projectdomain.Repository, sessions sessiondomain.Repository, settings settingdomain.MindMapConfigurationProvider, uow port.UnitOfWork) *Service {
	return &Service{
		repo: repo, uow: uow, projects: projects, sessions: sessions, settings: settings,
		now: time.Now, generateID: generateID, watchInterval: 2 * time.Second,
	}
}

func (s *Service) Watch(ctx context.Context, input GetInput) (<-chan ChangeDTO, error) {
	current, err := s.Get(ctx, input)
	if err != nil {
		return nil, err
	}
	interval := s.watchInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	out := make(chan ChangeDTO)
	go func() {
		defer close(out)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		updatedAt := current.UpdatedAt
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				next, err := s.Get(ctx, input)
				if err != nil {
					return
				}
				if next.UpdatedAt.Equal(updatedAt) {
					continue
				}
				updatedAt = next.UpdatedAt
				change := ChangeDTO{ProjectID: next.ProjectID, SessionID: next.SessionID, UpdatedAt: next.UpdatedAt}
				select {
				case <-ctx.Done():
					return
				case out <- change:
				}
			}
		}
	}()
	return out, nil
}

func (s *Service) Get(ctx context.Context, input GetInput) (GraphDTO, error) {
	project, err := s.requireEnabledProject(ctx, input.ProjectID)
	if err != nil {
		return GraphDTO{}, err
	}
	if input.SessionID != "" {
		if err := s.requireSession(ctx, input.ProjectID, input.SessionID, false); err != nil {
			return GraphDTO{}, err
		}
	}
	return s.loadGraph(ctx, project, input.SessionID)
}

func (s *Service) loadGraph(ctx context.Context, project projectdomain.Project, sessionID domain.SessionID) (GraphDTO, error) {
	projectID := domain.ProjectID(project.ID)
	graph, _, err := s.repo.FindGraph(ctx, projectID)
	if err != nil {
		return GraphDTO{}, fmt.Errorf("find project mind map: %w", err)
	}
	graph.ProjectID = projectID
	domain.EnsureRoot(&graph, project.Name, project.UpdatedAt)
	if sessionID != "" {
		overlay, found, err := s.repo.FindOverlay(ctx, sessionID)
		if err != nil {
			return GraphDTO{}, fmt.Errorf("find card mind map: %w", err)
		}
		if found {
			graph = domain.Materialize(graph, overlay.Changes)
			if overlay.UpdatedAt.After(graph.UpdatedAt) {
				graph.UpdatedAt = overlay.UpdatedAt
			}
		}
	} else {
		graph = domain.Visible(graph)
	}
	return toDTO(graph, sessionID), nil
}

func (s *Service) ListCards(ctx context.Context, projectID domain.ProjectID) ([]CardDTO, error) {
	if _, err := s.requireEnabledProject(ctx, projectID); err != nil {
		return nil, err
	}
	overlays, err := s.repo.ListOverlays(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list card mind maps: %w", err)
	}
	tasks, err := s.repo.ListTasks(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list mind map tasks: %w", err)
	}
	taskBySession := make(map[domain.SessionID]domain.Task, len(tasks))
	for _, task := range tasks {
		if task.Status != domain.TaskCompleted {
			taskBySession[task.SessionID] = task
		}
	}
	items := make([]CardDTO, 0, len(overlays)+len(taskBySession))
	seen := make(map[domain.SessionID]struct{}, len(overlays))
	for _, overlay := range overlays {
		session, err := s.sessions.Find(ctx, sessiondomain.ID(overlay.SessionID))
		if err != nil {
			continue
		}
		if session.Status == sessiondomain.StatusClosed {
			if _, ok := taskBySession[overlay.SessionID]; !ok {
				continue
			}
		}
		task := taskBySession[overlay.SessionID]
		items = append(items, CardDTO{
			SessionID: overlay.SessionID, Requirement: session.Requirement, UpdatedAt: overlay.UpdatedAt,
			TaskID: task.ID, TaskStatus: task.Status, TaskError: task.Error,
		})
		seen[overlay.SessionID] = struct{}{}
	}
	for sessionID, task := range taskBySession {
		if _, ok := seen[sessionID]; ok {
			continue
		}
		session, err := s.sessions.Find(ctx, sessiondomain.ID(sessionID))
		if err != nil {
			continue
		}
		items = append(items, CardDTO{
			SessionID: sessionID, Requirement: session.Requirement, UpdatedAt: task.UpdatedAt,
			TaskID: task.ID, TaskStatus: task.Status, TaskError: task.Error,
		})
	}
	return items, nil
}

func (s *Service) RetryTask(ctx context.Context, taskID domain.TaskID) (CardDTO, error) {
	configuration, err := s.settings.MindMapConfiguration(ctx)
	if err != nil {
		return CardDTO{}, fmt.Errorf("get mind map settings: %w", err)
	}
	if !configuration.Enabled || configuration.Mode != settingdomain.MindMapModeAsync {
		return CardDTO{}, errors.New("async mind map queue is paused")
	}
	existing, err := s.repo.FindTask(ctx, taskID)
	if err != nil {
		return CardDTO{}, err
	}
	if _, err := s.requireEnabledProject(ctx, existing.ProjectID); err != nil {
		return CardDTO{}, err
	}
	now := s.now()
	var updated domain.Task
	apply := func(ctx context.Context, repo domain.Repository) error {
		task, err := repo.FindTask(ctx, taskID)
		if err != nil {
			return err
		}
		if task.Status != domain.TaskFailed {
			return errors.New("only failed mind map tasks can be retried")
		}
		task.Status = domain.TaskQueued
		task.ProcessRunID = ""
		task.Error = ""
		task.StartedAt = nil
		task.FinishedAt = nil
		task.UpdatedAt = now
		if err := repo.SaveTask(ctx, task); err != nil {
			return err
		}
		updated = task
		return nil
	}
	if s.uow != nil {
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error { return apply(ctx, tx.MindMaps()) }); err != nil {
			return CardDTO{}, err
		}
	} else if err := apply(ctx, s.repo); err != nil {
		return CardDTO{}, err
	}
	if s.scheduleQueue != nil {
		s.scheduleQueue()
	}
	session, err := s.sessions.Find(ctx, sessiondomain.ID(updated.SessionID))
	if err != nil {
		return CardDTO{}, err
	}
	return CardDTO{
		SessionID: updated.SessionID, Requirement: session.Requirement, UpdatedAt: updated.UpdatedAt,
		TaskID: updated.ID, TaskStatus: updated.Status,
	}, nil
}

func (s *Service) Update(ctx context.Context, input UpdateInput) (GraphDTO, error) {
	return s.update(ctx, input, true, false)
}

func (s *Service) update(ctx context.Context, input UpdateInput, allowClosedSession bool, allowDisabled bool) (GraphDTO, error) {
	if len(input.Operations) == 0 {
		return s.Get(ctx, GetInput{ProjectID: input.ProjectID, SessionID: input.SessionID})
	}
	if len(input.Operations) > 100 {
		return GraphDTO{}, errors.New("mind map update cannot contain more than 100 operations")
	}
	var project projectdomain.Project
	var err error
	if allowDisabled {
		project, err = s.projects.Find(ctx, projectdomain.ID(input.ProjectID))
	} else {
		project, err = s.requireEnabledProject(ctx, input.ProjectID)
	}
	if err != nil {
		return GraphDTO{}, err
	}
	if input.SessionID != "" {
		if err := s.requireSession(ctx, input.ProjectID, input.SessionID, !allowClosedSession); err != nil {
			return GraphDTO{}, err
		}
	}
	changes, err := s.buildChanges(input)
	if err != nil {
		return GraphDTO{}, err
	}
	apply := func(ctx context.Context, repo domain.Repository) error {
		graph, _, err := repo.FindGraph(ctx, input.ProjectID)
		if err != nil {
			return fmt.Errorf("find project mind map: %w", err)
		}
		graph.ProjectID = input.ProjectID
		domain.EnsureRoot(&graph, project.Name, project.UpdatedAt)
		if input.SessionID == "" {
			for _, change := range changes {
				domain.Apply(&graph, change)
				graph.History = append(graph.History, change)
			}
			domain.Touch(&graph, changes[len(changes)-1].OccurredAt)
			if err := validateVisibleGraph(domain.Visible(graph)); err != nil {
				return err
			}
			return repo.SaveGraph(ctx, graph)
		}
		overlay, _, err := repo.FindOverlay(ctx, input.SessionID)
		if err != nil {
			return fmt.Errorf("find card mind map: %w", err)
		}
		overlay.ProjectID = input.ProjectID
		overlay.SessionID = input.SessionID
		candidate := domain.Materialize(graph, append(append([]domain.Change(nil), overlay.Changes...), changes...))
		if err := validateVisibleGraph(candidate); err != nil {
			return err
		}
		overlay.Changes = append(overlay.Changes, changes...)
		currentUpdatedAt := graph.UpdatedAt
		if overlay.UpdatedAt.After(currentUpdatedAt) {
			currentUpdatedAt = overlay.UpdatedAt
		}
		overlay.UpdatedAt = domain.NextUpdatedAt(currentUpdatedAt, changes[len(changes)-1].OccurredAt)
		return repo.SaveOverlay(ctx, overlay)
	}
	if s.uow != nil {
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error { return apply(ctx, tx.MindMaps()) }); err != nil {
			return GraphDTO{}, err
		}
	} else if err := apply(ctx, s.repo); err != nil {
		return GraphDTO{}, err
	}
	if allowDisabled {
		return s.loadGraph(ctx, project, input.SessionID)
	}
	return s.Get(ctx, GetInput{ProjectID: input.ProjectID, SessionID: input.SessionID})
}

func (s *Service) GetForSession(ctx context.Context, sessionID domain.SessionID) (GraphDTO, error) {
	session, err := s.sessions.Find(ctx, sessiondomain.ID(sessionID))
	if err != nil {
		return GraphDTO{}, fmt.Errorf("find session: %w", err)
	}
	return s.Get(ctx, GetInput{ProjectID: domain.ProjectID(session.ProjectID), SessionID: sessionID})
}

func (s *Service) UpdateForSession(ctx context.Context, sessionID domain.SessionID, operations []OperationInput) (GraphDTO, error) {
	configuration, err := s.settings.MindMapConfiguration(ctx)
	if err != nil {
		return GraphDTO{}, fmt.Errorf("get mind map settings: %w", err)
	}
	if !configuration.Enabled || configuration.Mode != settingdomain.MindMapModeRealtime {
		return GraphDTO{}, errors.New("mind map update tool is unavailable outside realtime mode")
	}
	session, err := s.sessions.Find(ctx, sessiondomain.ID(sessionID))
	if err != nil {
		return GraphDTO{}, fmt.Errorf("find session: %w", err)
	}
	return s.Update(ctx, UpdateInput{ProjectID: domain.ProjectID(session.ProjectID), SessionID: sessionID, Operations: operations})
}

func (s *Service) UpdateForTask(ctx context.Context, processRunID string, sessionID domain.SessionID, operations []OperationInput) (GraphDTO, error) {
	task, found, err := s.repo.FindTaskBySession(ctx, sessionID)
	if err != nil {
		return GraphDTO{}, fmt.Errorf("find mind map task: %w", err)
	}
	if !found || task.Status != domain.TaskRunning || strings.TrimSpace(task.ProcessRunID) != strings.TrimSpace(processRunID) {
		return GraphDTO{}, errors.New("mind map task is not active")
	}
	session, err := s.sessions.Find(ctx, sessiondomain.ID(sessionID))
	if err != nil {
		return GraphDTO{}, fmt.Errorf("find session: %w", err)
	}
	return s.update(ctx, UpdateInput{ProjectID: domain.ProjectID(session.ProjectID), SessionID: sessionID, Operations: operations}, true, true)
}

func (s *Service) getForProcess(ctx context.Context, processRunID string, sessionID domain.SessionID) (GraphDTO, error) {
	task, found, err := s.repo.FindTaskBySession(ctx, sessionID)
	if err != nil {
		return GraphDTO{}, fmt.Errorf("find mind map task: %w", err)
	}
	if !found || task.Status != domain.TaskRunning || strings.TrimSpace(task.ProcessRunID) != strings.TrimSpace(processRunID) {
		return s.GetForSession(ctx, sessionID)
	}
	session, err := s.sessions.Find(ctx, sessiondomain.ID(sessionID))
	if err != nil {
		return GraphDTO{}, fmt.Errorf("find session: %w", err)
	}
	project, err := s.projects.Find(ctx, projectdomain.ID(session.ProjectID))
	if err != nil {
		return GraphDTO{}, fmt.Errorf("find project: %w", err)
	}
	return s.loadGraph(ctx, project, sessionID)
}

func (s *Service) SearchForProcess(ctx context.Context, processRunID string, sessionID domain.SessionID, query string, limit int) (SearchResultDTO, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return SearchResultDTO{}, errors.New("mind map search query is required")
	}
	if len(query) > 500 {
		return SearchResultDTO{}, errors.New("mind map search query is too long")
	}
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 50 {
		return SearchResultDTO{}, errors.New("mind map search limit must be between 1 and 50")
	}
	graph, err := s.getForProcess(ctx, processRunID, sessionID)
	if err != nil {
		return SearchResultDTO{}, err
	}
	return searchGraph(graph, query, limit), nil
}

func (s *Service) UpdateForProcess(ctx context.Context, processRunID string, sessionID domain.SessionID, operations []OperationInput) (GraphDTO, error) {
	task, found, err := s.repo.FindTaskBySession(ctx, sessionID)
	if err != nil {
		return GraphDTO{}, fmt.Errorf("find mind map task: %w", err)
	}
	if found && task.Status == domain.TaskRunning && strings.TrimSpace(task.ProcessRunID) == strings.TrimSpace(processRunID) {
		return s.UpdateForTask(ctx, processRunID, sessionID, operations)
	}
	return s.UpdateForSession(ctx, sessionID, operations)
}

func (s *Service) buildChanges(input UpdateInput) ([]domain.Change, error) {
	changes := make([]domain.Change, 0, len(input.Operations))
	for _, operation := range input.Operations {
		id := strings.TrimSpace(operation.ID)
		if id == "" {
			return nil, errors.New("mind map operation id is required")
		}
		if len(id) > 128 {
			return nil, errors.New("mind map operation id is too long")
		}
		if !validChangeKind(operation.Kind) {
			return nil, fmt.Errorf("unsupported mind map operation %q", operation.Kind)
		}
		if id == string(domain.RootNodeID) && (operation.Kind == domain.ChangeUpsertNode || operation.Kind == domain.ChangeDeleteNode) {
			return nil, errors.New("project root node is managed by the system")
		}
		if operation.Title != nil {
			value := strings.TrimSpace(*operation.Title)
			if len(value) > 500 {
				return nil, errors.New("mind map node title is too long")
			}
			operation.Title = &value
		}
		if operation.Content != nil && len(*operation.Content) > 20000 {
			return nil, errors.New("mind map node content is too long")
		}
		if operation.Label != nil && len(*operation.Label) > 500 {
			return nil, errors.New("mind map edge label is too long")
		}
		if err := validateOperationShape(operation); err != nil {
			return nil, err
		}
		changeID, err := s.generateID()
		if err != nil {
			return nil, fmt.Errorf("generate mind map change id: %w", err)
		}
		changes = append(changes, domain.Change{
			ID: changeID, ProjectID: input.ProjectID, SessionID: input.SessionID, Kind: operation.Kind,
			EntityID: id, Title: operation.Title, Content: operation.Content,
			SourceID: operation.SourceID, TargetID: operation.TargetID, Label: operation.Label, OccurredAt: s.now(),
		})
	}
	return changes, nil
}

func validateOperationShape(operation OperationInput) error {
	hasNodeFields := operation.Title != nil || operation.Content != nil
	hasEdgeFields := operation.SourceID != nil || operation.TargetID != nil || operation.Label != nil
	switch operation.Kind {
	case domain.ChangeUpsertNode:
		if !hasNodeFields {
			return errors.New("mind map node upsert requires a title or content")
		}
		if operation.Title != nil && strings.TrimSpace(*operation.Title) == "" {
			return errors.New("mind map node title cannot be empty")
		}
		if hasEdgeFields {
			return errors.New("mind map node upsert cannot contain edge fields")
		}
	case domain.ChangeDeleteNode, domain.ChangeDeleteEdge:
		if hasNodeFields || hasEdgeFields {
			return errors.New("mind map delete operation only accepts an id")
		}
	case domain.ChangeUpsertEdge:
		if operation.SourceID == nil || strings.TrimSpace(string(*operation.SourceID)) == "" ||
			operation.TargetID == nil || strings.TrimSpace(string(*operation.TargetID)) == "" {
			return errors.New("mind map edge upsert requires source and target nodes")
		}
		if hasNodeFields {
			return errors.New("mind map edge upsert cannot contain node fields")
		}
	}
	return nil
}

func searchGraph(graph GraphDTO, query string, limit int) SearchResultDTO {
	type candidate struct {
		match NodeMatchDTO
		score int
	}
	normalizedQuery := strings.ToLower(strings.TrimSpace(query))
	terms := strings.Fields(normalizedQuery)
	candidates := make([]candidate, 0)
	for _, node := range graph.Nodes {
		id := strings.ToLower(string(node.ID))
		title := strings.ToLower(node.Title)
		content := strings.ToLower(node.Content)
		searchable := id + "\n" + title + "\n" + content
		matched := true
		for _, term := range terms {
			if !strings.Contains(searchable, term) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		fields := make([]string, 0, 3)
		score := 0
		if containsAny(id, terms) {
			fields = append(fields, "id")
			score += 10
		}
		if containsAny(title, terms) {
			fields = append(fields, "title")
			score += 40
		}
		if containsAny(content, terms) {
			fields = append(fields, "content")
			score += 20
		}
		if id == normalizedQuery {
			score += 200
		}
		if title == normalizedQuery {
			score += 160
		}
		candidates = append(candidates, candidate{match: NodeMatchDTO{Node: node, MatchedFields: fields}, score: score})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		left, right := strings.ToLower(candidates[i].match.Node.Title), strings.ToLower(candidates[j].match.Node.Title)
		if left != right {
			return left < right
		}
		return candidates[i].match.Node.ID < candidates[j].match.Node.ID
	})
	result := SearchResultDTO{
		ProjectID: graph.ProjectID, SessionID: graph.SessionID, Query: query,
		TotalMatches: len(candidates), Truncated: len(candidates) > limit,
	}
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	matchedIDs := make(map[domain.NodeID]struct{}, len(candidates))
	result.Matches = make([]NodeMatchDTO, 0, len(candidates))
	for _, item := range candidates {
		result.Matches = append(result.Matches, item.match)
		matchedIDs[item.match.Node.ID] = struct{}{}
	}
	relatedIDs := make(map[domain.NodeID]struct{})
	for _, edge := range graph.Edges {
		_, sourceMatched := matchedIDs[edge.SourceID]
		_, targetMatched := matchedIDs[edge.TargetID]
		if !sourceMatched && !targetMatched {
			continue
		}
		result.Edges = append(result.Edges, edge)
		if !sourceMatched {
			relatedIDs[edge.SourceID] = struct{}{}
		}
		if !targetMatched {
			relatedIDs[edge.TargetID] = struct{}{}
		}
	}
	for _, node := range graph.Nodes {
		if _, ok := relatedIDs[node.ID]; ok {
			result.RelatedNodes = append(result.RelatedNodes, node)
		}
	}
	return result
}

func containsAny(value string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

func (s *Service) requireEnabledProject(ctx context.Context, projectID domain.ProjectID) (projectdomain.Project, error) {
	if s == nil || s.repo == nil || s.projects == nil || s.sessions == nil || s.settings == nil {
		return projectdomain.Project{}, errors.New("mind map service is unavailable")
	}
	if projectID == "" {
		return projectdomain.Project{}, errors.New("project id is required")
	}
	project, err := s.projects.Find(ctx, projectdomain.ID(projectID))
	if err != nil {
		return projectdomain.Project{}, fmt.Errorf("find project: %w", err)
	}
	if !project.MindMapEnabled {
		return projectdomain.Project{}, errors.New("project mind map is disabled")
	}
	configuration, err := s.settings.MindMapConfiguration(ctx)
	if err != nil {
		return projectdomain.Project{}, fmt.Errorf("get mind map settings: %w", err)
	}
	if !configuration.Enabled {
		return projectdomain.Project{}, errors.New("global mind map is disabled")
	}
	return project, nil
}

func (s *Service) requireSession(ctx context.Context, projectID domain.ProjectID, sessionID domain.SessionID, mutable bool) error {
	session, err := s.sessions.Find(ctx, sessiondomain.ID(sessionID))
	if err != nil {
		return fmt.Errorf("find session: %w", err)
	}
	if domain.ProjectID(session.ProjectID) != projectID {
		return errors.New("session does not belong to project")
	}
	if mutable && session.Status == sessiondomain.StatusClosed {
		return errors.New("closed session mind map cannot be changed")
	}
	return nil
}

func validateVisibleGraph(graph domain.Graph) error {
	nodes := make(map[domain.NodeID]struct{}, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if _, exists := nodes[node.ID]; exists {
			return fmt.Errorf("mind map node id %q is duplicated", node.ID)
		}
		if strings.TrimSpace(node.Title) == "" {
			return fmt.Errorf("mind map node %q requires a title", node.ID)
		}
		nodes[node.ID] = struct{}{}
	}
	edges := make(map[domain.EdgeID]struct{}, len(graph.Edges))
	for _, edge := range graph.Edges {
		if _, exists := edges[edge.ID]; exists {
			return fmt.Errorf("mind map edge id %q is duplicated", edge.ID)
		}
		edges[edge.ID] = struct{}{}
		if edge.SourceID == edge.TargetID {
			return fmt.Errorf("mind map edge %q cannot connect a node to itself", edge.ID)
		}
		if _, ok := nodes[edge.SourceID]; !ok {
			return fmt.Errorf("mind map edge %q source node does not exist", edge.ID)
		}
		if _, ok := nodes[edge.TargetID]; !ok {
			return fmt.Errorf("mind map edge %q target node does not exist", edge.ID)
		}
	}
	return nil
}

func validChangeKind(kind domain.ChangeKind) bool {
	switch kind {
	case domain.ChangeUpsertNode, domain.ChangeDeleteNode, domain.ChangeUpsertEdge, domain.ChangeDeleteEdge:
		return true
	default:
		return false
	}
}

func toDTO(graph domain.Graph, sessionID domain.SessionID) GraphDTO {
	dto := GraphDTO{ProjectID: graph.ProjectID, SessionID: sessionID, UpdatedAt: graph.UpdatedAt}
	dto.Nodes = make([]NodeDTO, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		dto.Nodes = append(dto.Nodes, NodeDTO{ID: node.ID, Title: node.Title, Content: node.Content})
	}
	dto.Edges = make([]EdgeDTO, 0, len(graph.Edges))
	for _, edge := range graph.Edges {
		dto.Edges = append(dto.Edges, EdgeDTO{ID: edge.ID, SourceID: edge.SourceID, TargetID: edge.TargetID, Label: edge.Label})
	}
	return dto
}

func generateID() (domain.ChangeID, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return domain.ChangeID(hex.EncodeToString(value[:])), nil
}

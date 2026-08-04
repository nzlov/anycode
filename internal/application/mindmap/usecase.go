package mindmap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
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
	GetPage(ctx context.Context, input GetPageInput) (GraphPageDTO, error)
	ListCards(ctx context.Context, projectID domain.ProjectID) ([]CardDTO, error)
	Search(ctx context.Context, input SearchInput) (ProjectSearchResultDTO, error)
	Update(ctx context.Context, input UpdateInput) (GraphDTO, error)
	Apply(ctx context.Context, input UpdateInput) (GraphDTO, error)
	GetForSession(ctx context.Context, sessionID domain.SessionID) (GraphDTO, error)
	SearchForProcess(ctx context.Context, processRunID string, sessionID domain.SessionID, query string, limit int) (SearchResultDTO, error)
	ListTagsForProcess(ctx context.Context, processRunID string, sessionID domain.SessionID) (TagListDTO, error)
	UpdateForSession(ctx context.Context, sessionID domain.SessionID, operations []OperationInput) (GraphDTO, error)
	UpdateForTask(ctx context.Context, processRunID string, sessionID domain.SessionID, operations []OperationInput) (GraphDTO, error)
	UpdateForProcess(ctx context.Context, processRunID string, sessionID domain.SessionID, tagRevision string, operations []OperationInput) (GraphDTO, error)
	RetryTask(ctx context.Context, taskID domain.TaskID) (CardDTO, error)
	Watch(ctx context.Context, input GetInput) (<-chan ChangeDTO, error)
}

type GetInput struct {
	ProjectID domain.ProjectID
	SessionID domain.SessionID
}

type GetPageInput struct {
	ProjectID    domain.ProjectID
	SessionID    domain.SessionID
	NodeAfter    domain.NodeID
	EdgeAfter    domain.EdgeID
	IncludeNodes bool
	IncludeEdges bool
	PageSize     int
}

type UpdateInput struct {
	ProjectID        domain.ProjectID
	SessionID        domain.SessionID
	TagRevision      string
	EnforceTagPolicy bool
	Operations       []OperationInput
}

type SearchInput struct {
	ProjectID domain.ProjectID
	Query     string
}

type OperationInput struct {
	Kind     domain.ChangeKind
	ID       string
	Title    *string
	Content  *string
	Files    *[]domain.NodeFile
	Tags     *[]string
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

type GraphPageDTO struct {
	ProjectID      domain.ProjectID
	SessionID      domain.SessionID
	Nodes          []NodeDTO
	Edges          []EdgeDTO
	UpdatedAt      time.Time
	NextNodeCursor domain.NodeID
	NextEdgeCursor domain.EdgeID
}

type NodeDTO struct {
	ID         domain.NodeID
	Title      string
	Content    string
	Files      []domain.NodeFile
	ChangeType NodeChangeType
}

type NodeChangeType string

const (
	NodeUnchanged NodeChangeType = "unchanged"
	NodeAdded     NodeChangeType = "added"
	NodeModified  NodeChangeType = "modified"
	NodeDeleted   NodeChangeType = "deleted"
)

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

type ProjectSearchResultDTO struct {
	ProjectID domain.ProjectID
	Query     string
	Matches   []ProjectSearchMatchDTO
}

type TagListDTO struct {
	ProjectID domain.ProjectID
	SessionID domain.SessionID
	Revision  string
	Tags      []NodeDTO
}

type ProjectSearchMatchDTO struct {
	NodeID    domain.NodeID
	SessionID domain.SessionID
}

type CardDTO struct {
	SessionID       domain.SessionID
	Requirement     string
	UpdatedAt       time.Time
	HasChanges      bool
	TaskID          domain.TaskID
	TaskStatus      domain.TaskStatus
	TaskError       string
	Nodes           []NodeDTO
	Edges           []EdgeDTO
	ModifiedNodeIDs []domain.NodeID
	DeletedNodeIDs  []domain.NodeID
	DeletedEdgeIDs  []domain.EdgeID
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
	current, err := s.watchSnapshot(ctx, input)
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
				next, err := s.watchSnapshot(ctx, input)
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

func (s *Service) watchSnapshot(ctx context.Context, input GetInput) (GraphDTO, error) {
	project, err := s.requireEnabledProject(ctx, input.ProjectID)
	if err != nil {
		return GraphDTO{}, err
	}
	if input.SessionID != "" {
		if err := s.requireSession(ctx, input.ProjectID, input.SessionID, false); err != nil {
			return GraphDTO{}, err
		}
	}
	updatedAt, err := s.repo.FindRevision(ctx, input.ProjectID, input.SessionID)
	if err != nil {
		return GraphDTO{}, err
	}
	if project.UpdatedAt.After(updatedAt) {
		updatedAt = project.UpdatedAt
	}
	return GraphDTO{ProjectID: input.ProjectID, SessionID: input.SessionID, UpdatedAt: updatedAt}, nil
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
	return s.loadGraph(ctx, project, input.SessionID, true)
}

func (s *Service) GetPage(ctx context.Context, input GetPageInput) (GraphPageDTO, error) {
	if input.PageSize == 0 {
		input.PageSize = 200
	}
	if input.PageSize < 10 || input.PageSize > 500 {
		return GraphPageDTO{}, errors.New("mind map page size must be between 10 and 500")
	}
	if !input.IncludeNodes && !input.IncludeEdges {
		return GraphPageDTO{}, errors.New("mind map page must include nodes or edges")
	}
	project, err := s.requireEnabledProject(ctx, input.ProjectID)
	if err != nil {
		return GraphPageDTO{}, err
	}
	if input.SessionID != "" {
		if err := s.requireSession(ctx, input.ProjectID, input.SessionID, false); err != nil {
			return GraphPageDTO{}, err
		}
		graph, err := s.loadGraph(ctx, project, input.SessionID, true)
		if err != nil {
			return GraphPageDTO{}, err
		}
		nodes, nextNode := pageNodeDTOs(graph.Nodes, input.NodeAfter, input.PageSize, input.IncludeNodes)
		edges, nextEdge := pageEdgeDTOs(graph.Edges, input.EdgeAfter, input.PageSize, input.IncludeEdges)
		return GraphPageDTO{
			ProjectID: graph.ProjectID, SessionID: graph.SessionID, Nodes: nodes, Edges: edges, UpdatedAt: graph.UpdatedAt,
			NextNodeCursor: nextNode, NextEdgeCursor: nextEdge,
		}, nil
	}
	nodeLimit, edgeLimit := 0, 0
	if input.IncludeNodes {
		nodeLimit = input.PageSize
		if input.NodeAfter == "" {
			nodeLimit--
		}
	}
	if input.IncludeEdges {
		edgeLimit = input.PageSize
	}
	page, _, err := s.repo.FindGraphPage(ctx, input.ProjectID, input.NodeAfter, input.EdgeAfter, nodeLimit, edgeLimit)
	if err != nil {
		return GraphPageDTO{}, err
	}
	updatedAt := page.Graph.UpdatedAt
	if project.UpdatedAt.After(updatedAt) {
		updatedAt = project.UpdatedAt
	}
	result := GraphPageDTO{
		ProjectID: input.ProjectID, UpdatedAt: updatedAt,
		NextNodeCursor: page.NextNodeCursor, NextEdgeCursor: page.NextEdgeCursor,
	}
	if input.IncludeNodes {
		result.Nodes = make([]NodeDTO, 0, len(page.Graph.Nodes)+1)
		if input.NodeAfter == "" {
			result.Nodes = append(result.Nodes, NodeDTO{ID: domain.RootNodeID, Title: project.Name, ChangeType: NodeUnchanged})
		}
		for _, node := range page.Graph.Nodes {
			result.Nodes = append(result.Nodes, NodeDTO{ID: node.ID, Title: node.Title, Content: node.Content, Files: node.Files, ChangeType: NodeUnchanged})
		}
	}
	if input.IncludeEdges {
		result.Edges = make([]EdgeDTO, 0, len(page.Graph.Edges))
		for _, edge := range page.Graph.Edges {
			result.Edges = append(result.Edges, EdgeDTO{ID: edge.ID, SourceID: edge.SourceID, TargetID: edge.TargetID, Label: edge.Label})
		}
	}
	return result, nil
}

func pageNodeDTOs(items []NodeDTO, after domain.NodeID, limit int, include bool) ([]NodeDTO, domain.NodeID) {
	if !include {
		return nil, ""
	}
	ordered := append([]NodeDTO(nil), items...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].ID == domain.RootNodeID {
			return true
		}
		if ordered[j].ID == domain.RootNodeID {
			return false
		}
		return ordered[i].ID < ordered[j].ID
	})
	start := 0
	if after != "" {
		start = len(ordered)
		for index := range ordered {
			if ordered[index].ID == after {
				start = index + 1
				break
			}
		}
	}
	end := start + limit
	if end >= len(ordered) {
		end = len(ordered)
		return ordered[start:end], ""
	}
	return ordered[start:end], ordered[end-1].ID
}

func pageEdgeDTOs(items []EdgeDTO, after domain.EdgeID, limit int, include bool) ([]EdgeDTO, domain.EdgeID) {
	if !include {
		return nil, ""
	}
	ordered := append([]EdgeDTO(nil), items...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	start := 0
	if after != "" {
		start = len(ordered)
		for index := range ordered {
			if ordered[index].ID == after {
				start = index + 1
				break
			}
		}
	}
	end := start + limit
	if end >= len(ordered) {
		end = len(ordered)
		return ordered[start:end], ""
	}
	return ordered[start:end], ordered[end-1].ID
}

func (s *Service) loadGraph(ctx context.Context, project projectdomain.Project, sessionID domain.SessionID, withChanges bool) (GraphDTO, error) {
	projectID := domain.ProjectID(project.ID)
	graph, _, err := s.repo.FindGraph(ctx, projectID)
	if err != nil {
		return GraphDTO{}, fmt.Errorf("find project mind map: %w", err)
	}
	graph.ProjectID = projectID
	domain.EnsureRoot(&graph, project.Name, project.UpdatedAt)
	base := domain.Visible(graph)
	if sessionID != "" {
		overlay, found, err := s.repo.FindOverlay(ctx, sessionID)
		if err != nil {
			return GraphDTO{}, fmt.Errorf("find card mind map: %w", err)
		}
		if found {
			current := domain.Materialize(graph, overlay.Changes)
			if overlay.UpdatedAt.After(current.UpdatedAt) {
				current.UpdatedAt = overlay.UpdatedAt
			}
			if withChanges {
				return toDiffDTO(base, current, sessionID), nil
			}
			graph = current
		} else {
			graph = base
		}
	} else {
		graph = base
	}
	return toDTO(graph, sessionID), nil
}

func (s *Service) ListCards(ctx context.Context, projectID domain.ProjectID) ([]CardDTO, error) {
	project, err := s.requireEnabledProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	graph, _, err := s.repo.FindGraph(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("find project mind map: %w", err)
	}
	graph.ProjectID = projectID
	domain.EnsureRoot(&graph, project.Name, project.UpdatedAt)
	base := domain.Visible(graph)
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
		nodes, edges, modifiedNodeIDs, deletedNodeIDs, deletedEdgeIDs := cardDeltaDTO(base, domain.Materialize(graph, overlay.Changes))
		items = append(items, CardDTO{
			SessionID: overlay.SessionID, Requirement: session.Requirement, UpdatedAt: overlay.UpdatedAt,
			HasChanges: len(overlay.Changes) > 0,
			TaskID:     task.ID, TaskStatus: task.Status, TaskError: task.Error,
			Nodes: nodes, Edges: edges, ModifiedNodeIDs: modifiedNodeIDs, DeletedNodeIDs: deletedNodeIDs, DeletedEdgeIDs: deletedEdgeIDs,
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

func (s *Service) Search(ctx context.Context, input SearchInput) (ProjectSearchResultDTO, error) {
	query, err := validateSearchQuery(input.Query)
	if err != nil {
		return ProjectSearchResultDTO{}, err
	}
	graph, err := s.Get(ctx, GetInput{ProjectID: input.ProjectID})
	if err != nil {
		return ProjectSearchResultDTO{}, err
	}
	cards, err := s.ListCards(ctx, input.ProjectID)
	if err != nil {
		return ProjectSearchResultDTO{}, err
	}
	result := ProjectSearchResultDTO{ProjectID: input.ProjectID, Query: query}
	appendMatches := func(nodes []NodeDTO, sessionID domain.SessionID) {
		if len(nodes) == 0 {
			return
		}
		matches := searchGraph(GraphDTO{ProjectID: input.ProjectID, SessionID: sessionID, Nodes: nodes}, query, len(nodes))
		for _, match := range matches.Matches {
			result.Matches = append(result.Matches, ProjectSearchMatchDTO{NodeID: match.Node.ID, SessionID: sessionID})
		}
	}
	appendMatches(graph.Nodes, "")
	for _, card := range cards {
		appendMatches(card.Nodes, card.SessionID)
	}
	return result, nil
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
	return s.update(ctx, input, true, false, true, true)
}

func (s *Service) Apply(ctx context.Context, input UpdateInput) (GraphDTO, error) {
	return s.update(ctx, input, true, false, false, false)
}

func (s *Service) update(ctx context.Context, input UpdateInput, allowClosedSession bool, allowDisabled bool, withChanges bool, reload bool) (GraphDTO, error) {
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
	if len(input.Operations) == 0 {
		return s.loadGraph(ctx, project, input.SessionID, withChanges)
	}
	var staticChanges []domain.Change
	if !input.EnforceTagPolicy {
		staticChanges, err = s.buildChanges(input)
		if err != nil {
			return GraphDTO{}, err
		}
	}
	result := GraphDTO{ProjectID: input.ProjectID, SessionID: input.SessionID}
	apply := func(ctx context.Context, repo domain.Repository) error {
		graph, _, err := repo.FindGraph(ctx, input.ProjectID)
		if err != nil {
			return fmt.Errorf("find project mind map: %w", err)
		}
		graph.ProjectID = input.ProjectID
		domain.EnsureRoot(&graph, project.Name, project.UpdatedAt)
		if input.SessionID == "" {
			before := domain.Visible(graph)
			changes := staticChanges
			if input.EnforceTagPolicy {
				if err := validateTagRevision(input.TagRevision, before.UpdatedAt); err != nil {
					return err
				}
				changes, err = s.buildAgentChanges(input, before)
				if err != nil {
					return err
				}
			}
			if len(changes) == 0 {
				result.UpdatedAt = before.UpdatedAt
				return nil
			}
			for _, change := range changes {
				domain.Apply(&graph, change)
				graph.History = append(graph.History, change)
			}
			domain.Touch(&graph, changes[len(changes)-1].OccurredAt)
			candidate := domain.Visible(graph)
			if input.EnforceTagPolicy {
				if err := validateAgentTagTopology(before, candidate); err != nil {
					return err
				}
			}
			if err := validateVisibleGraph(candidate); err != nil {
				return err
			}
			result.UpdatedAt = graph.UpdatedAt
			return repo.SaveGraph(ctx, graph, changes)
		}
		overlay, _, err := repo.FindOverlay(ctx, input.SessionID)
		if err != nil {
			return fmt.Errorf("find card mind map: %w", err)
		}
		overlay.ProjectID = input.ProjectID
		overlay.SessionID = input.SessionID
		before := domain.Materialize(graph, overlay.Changes)
		changes := staticChanges
		if overlay.UpdatedAt.After(before.UpdatedAt) {
			before.UpdatedAt = overlay.UpdatedAt
		}
		if input.EnforceTagPolicy {
			if err := validateTagRevision(input.TagRevision, before.UpdatedAt); err != nil {
				return err
			}
			changes, err = s.buildAgentChanges(input, before)
			if err != nil {
				return err
			}
		}
		if len(changes) == 0 {
			result.UpdatedAt = before.UpdatedAt
			return nil
		}
		candidate := domain.Materialize(graph, append(append([]domain.Change(nil), overlay.Changes...), changes...))
		if input.EnforceTagPolicy {
			if err := validateAgentTagTopology(before, candidate); err != nil {
				return err
			}
		}
		if err := validateVisibleGraph(candidate); err != nil {
			return err
		}
		overlay.Changes = append(overlay.Changes, changes...)
		overlay = domain.CompactOverlay(graph, overlay)
		currentUpdatedAt := graph.UpdatedAt
		if overlay.UpdatedAt.After(currentUpdatedAt) {
			currentUpdatedAt = overlay.UpdatedAt
		}
		overlay.UpdatedAt = domain.NextUpdatedAt(currentUpdatedAt, changes[len(changes)-1].OccurredAt)
		result.UpdatedAt = overlay.UpdatedAt
		return repo.SaveOverlay(ctx, overlay)
	}
	if s.uow != nil {
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error { return apply(ctx, tx.MindMaps()) }); err != nil {
			return GraphDTO{}, err
		}
	} else if err := apply(ctx, s.repo); err != nil {
		return GraphDTO{}, err
	}
	if !reload {
		return result, nil
	}
	return s.loadGraph(ctx, project, input.SessionID, withChanges)
}

func (s *Service) GetForSession(ctx context.Context, sessionID domain.SessionID) (GraphDTO, error) {
	session, err := s.sessions.Find(ctx, sessiondomain.ID(sessionID))
	if err != nil {
		return GraphDTO{}, fmt.Errorf("find session: %w", err)
	}
	project, err := s.requireEnabledProject(ctx, domain.ProjectID(session.ProjectID))
	if err != nil {
		return GraphDTO{}, err
	}
	return s.loadGraph(ctx, project, sessionID, false)
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
	return s.update(ctx, UpdateInput{ProjectID: domain.ProjectID(session.ProjectID), SessionID: sessionID, Operations: operations}, true, false, false, true)
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
	return s.update(ctx, UpdateInput{ProjectID: domain.ProjectID(session.ProjectID), SessionID: sessionID, Operations: operations}, true, true, false, true)
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
	return s.loadGraph(ctx, project, sessionID, false)
}

func (s *Service) SearchForProcess(ctx context.Context, processRunID string, sessionID domain.SessionID, query string, limit int) (SearchResultDTO, error) {
	query, err := validateSearchQuery(query)
	if err != nil {
		return SearchResultDTO{}, err
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

func (s *Service) ListTagsForProcess(ctx context.Context, processRunID string, sessionID domain.SessionID) (TagListDTO, error) {
	graph, err := s.getForProcess(ctx, processRunID, sessionID)
	if err != nil {
		return TagListDTO{}, err
	}
	result := TagListDTO{
		ProjectID: graph.ProjectID, SessionID: graph.SessionID, Revision: formatTagRevision(graph.UpdatedAt),
	}
	for _, node := range graph.Nodes {
		if domain.IsTagNodeID(node.ID) {
			result.Tags = append(result.Tags, node)
		}
	}
	sort.Slice(result.Tags, func(i, j int) bool {
		return strings.ToLower(result.Tags[i].Title) < strings.ToLower(result.Tags[j].Title)
	})
	return result, nil
}

func validateSearchQuery(query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", errors.New("mind map search query is required")
	}
	if len(query) > 500 {
		return "", errors.New("mind map search query is too long")
	}
	return query, nil
}

func (s *Service) UpdateForProcess(ctx context.Context, processRunID string, sessionID domain.SessionID, tagRevision string, operations []OperationInput) (GraphDTO, error) {
	if strings.TrimSpace(tagRevision) == "" {
		return GraphDTO{}, errors.New("mind map tag revision is required; list tags before updating")
	}
	task, found, err := s.repo.FindTaskBySession(ctx, sessionID)
	if err != nil {
		return GraphDTO{}, fmt.Errorf("find mind map task: %w", err)
	}
	if found && task.Status == domain.TaskRunning && strings.TrimSpace(task.ProcessRunID) == strings.TrimSpace(processRunID) {
		session, err := s.sessions.Find(ctx, sessiondomain.ID(sessionID))
		if err != nil {
			return GraphDTO{}, fmt.Errorf("find session: %w", err)
		}
		return s.update(ctx, UpdateInput{
			ProjectID: domain.ProjectID(session.ProjectID), SessionID: sessionID, TagRevision: tagRevision,
			EnforceTagPolicy: true, Operations: operations,
		}, true, true, false, true)
	}
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
	return s.update(ctx, UpdateInput{
		ProjectID: domain.ProjectID(session.ProjectID), SessionID: sessionID, TagRevision: tagRevision,
		EnforceTagPolicy: true, Operations: operations,
	}, true, false, false, true)
}

type managedTag struct {
	key   string
	title string
}

func (s *Service) buildAgentChanges(input UpdateInput, before domain.Graph) ([]domain.Change, error) {
	beforeNodes := make(map[domain.NodeID]domain.Node, len(before.Nodes))
	beforeEdges := make(map[domain.EdgeID]domain.Edge, len(before.Edges))
	for _, node := range before.Nodes {
		beforeNodes[node.ID] = node
	}
	for _, edge := range before.Edges {
		beforeEdges[edge.ID] = edge
	}
	explicit := make([]OperationInput, 0, len(input.Operations))
	desiredTags := make(map[domain.NodeID][]managedTag)
	for _, operation := range input.Operations {
		nodeID := domain.NodeID(strings.TrimSpace(operation.ID))
		if (operation.Kind == domain.ChangeUpsertNode || operation.Kind == domain.ChangeDeleteNode) && domain.IsTagNodeID(nodeID) {
			return nil, fmt.Errorf("mind map tags are managed by the server; operation %q cannot target %q", operation.Kind, nodeID)
		}
		if operation.Kind == domain.ChangeUpsertNode {
			if _, duplicate := desiredTags[nodeID]; duplicate {
				return nil, fmt.Errorf("mind map node %q cannot be upserted more than once per update", nodeID)
			}
			tags, err := normalizeManagedTags(operation.Tags)
			if err != nil {
				return nil, fmt.Errorf("mind map node %q: %w", nodeID, err)
			}
			desiredTags[nodeID] = tags
			operation.Tags = nil
			if operation.Title != nil || operation.Content != nil || operation.Files != nil {
				explicit = append(explicit, operation)
			} else if _, found := beforeNodes[nodeID]; !found {
				return nil, fmt.Errorf("new mind map node %q requires a title", nodeID)
			}
			continue
		}
		if operation.Tags != nil {
			return nil, fmt.Errorf("mind map operation %q cannot contain tags", operation.Kind)
		}
		if operation.Kind == domain.ChangeUpsertEdge && operation.SourceID != nil && operation.TargetID != nil {
			if *operation.SourceID == domain.RootNodeID || *operation.TargetID == domain.RootNodeID ||
				domain.IsTagNodeID(*operation.SourceID) || domain.IsTagNodeID(*operation.TargetID) {
				return nil, errors.New("mind map root and tag relationships are managed by the server")
			}
		}
		if operation.Kind == domain.ChangeDeleteEdge {
			if edge, found := beforeEdges[domain.EdgeID(strings.TrimSpace(operation.ID))]; found &&
				(edge.SourceID == domain.RootNodeID || edge.TargetID == domain.RootNodeID ||
					domain.IsTagNodeID(edge.SourceID) || domain.IsTagNodeID(edge.TargetID)) {
				return nil, errors.New("mind map root and tag relationships are managed by the server")
			}
		}
		if operation.Kind == domain.ChangeDeleteNode {
			for _, edge := range before.Edges {
				if (edge.SourceID == nodeID && domain.IsTagNodeID(edge.TargetID)) ||
					(edge.TargetID == nodeID && domain.IsTagNodeID(edge.SourceID)) {
					explicit = append(explicit, OperationInput{Kind: domain.ChangeDeleteEdge, ID: string(edge.ID)})
				}
			}
		}
		explicit = append(explicit, operation)
	}

	changes, err := s.buildChanges(UpdateInput{ProjectID: input.ProjectID, SessionID: input.SessionID, Operations: explicit})
	if err != nil {
		return nil, err
	}
	candidate := domain.Materialize(before, changes)
	managedOperations, err := reconcileManagedTags(candidate, desiredTags)
	if err != nil {
		return nil, err
	}
	managedChanges, err := s.buildChanges(UpdateInput{ProjectID: input.ProjectID, SessionID: input.SessionID, Operations: managedOperations})
	if err != nil {
		return nil, err
	}
	changes = append(changes, managedChanges...)
	candidate = domain.Materialize(candidate, managedChanges)
	cleanupOperations := orphanTagCleanupOperations(candidate)
	cleanupChanges, err := s.buildChanges(UpdateInput{ProjectID: input.ProjectID, SessionID: input.SessionID, Operations: cleanupOperations})
	if err != nil {
		return nil, err
	}
	return append(changes, cleanupChanges...), nil
}

func normalizeManagedTags(values *[]string) ([]managedTag, error) {
	if values == nil || len(*values) == 0 {
		return nil, errors.New("tags must contain at least one tag name")
	}
	if len(*values) > 20 {
		return nil, errors.New("tags cannot contain more than 20 tag names")
	}
	seen := make(map[string]struct{}, len(*values))
	result := make([]managedTag, 0, len(*values))
	for _, value := range *values {
		title := strings.Join(strings.Fields(value), " ")
		if title == "" {
			return nil, errors.New("tag names cannot be empty")
		}
		if len(title) > 100 {
			return nil, fmt.Errorf("tag name %q is too long", title)
		}
		key := strings.ToLower(title)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, managedTag{key: key, title: title})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].key < result[j].key })
	return result, nil
}

func reconcileManagedTags(graph domain.Graph, desiredTags map[domain.NodeID][]managedTag) ([]OperationInput, error) {
	tagByKey := make(map[string]domain.NodeID)
	tagNodes := make(map[domain.NodeID]struct{})
	rootLinked := make(map[domain.NodeID]bool)
	for _, node := range graph.Nodes {
		if !domain.IsTagNodeID(node.ID) {
			continue
		}
		key := strings.ToLower(strings.Join(strings.Fields(node.Title), " "))
		if existing, found := tagByKey[key]; !found || string(node.ID) < string(existing) {
			tagByKey[key] = node.ID
		}
		tagNodes[node.ID] = struct{}{}
	}
	for _, edge := range graph.Edges {
		if edge.SourceID == domain.RootNodeID && domain.IsTagNodeID(edge.TargetID) {
			rootLinked[edge.TargetID] = true
		}
	}
	nodeIDs := make([]domain.NodeID, 0, len(desiredTags))
	for nodeID := range desiredTags {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Slice(nodeIDs, func(i, j int) bool { return nodeIDs[i] < nodeIDs[j] })
	operations := make([]OperationInput, 0)
	for _, nodeID := range nodeIDs {
		desiredIDs := make(map[domain.NodeID]struct{}, len(desiredTags[nodeID]))
		for _, tag := range desiredTags[nodeID] {
			tagID, found := tagByKey[tag.key]
			if !found {
				tagID = domain.ManagedTagNodeID(tag.key)
				if _, collision := tagNodes[tagID]; collision {
					return nil, fmt.Errorf("managed mind map tag id collision for %q", tag.title)
				}
				title := tag.title
				operations = append(operations, OperationInput{Kind: domain.ChangeUpsertNode, ID: string(tagID), Title: &title})
				tagByKey[tag.key] = tagID
				tagNodes[tagID] = struct{}{}
			}
			desiredIDs[tagID] = struct{}{}
			if !rootLinked[tagID] {
				source, target, label := domain.RootNodeID, tagID, "分类"
				operations = append(operations, OperationInput{
					Kind: domain.ChangeUpsertEdge, ID: string(domain.TagRootEdgeID(tagID)),
					SourceID: &source, TargetID: &target, Label: &label,
				})
				rootLinked[tagID] = true
			}
		}
		linked := make(map[domain.NodeID]bool, len(desiredIDs))
		for _, edge := range graph.Edges {
			var tagID domain.NodeID
			switch {
			case edge.TargetID == nodeID && domain.IsTagNodeID(edge.SourceID):
				tagID = edge.SourceID
			case edge.SourceID == nodeID && domain.IsTagNodeID(edge.TargetID):
				tagID = edge.TargetID
			default:
				continue
			}
			_, desired := desiredIDs[tagID]
			canonical := domain.TagNodeEdgeID(tagID, nodeID)
			if desired && edge.ID == canonical && edge.SourceID == tagID && edge.TargetID == nodeID {
				linked[tagID] = true
				continue
			}
			operations = append(operations, OperationInput{Kind: domain.ChangeDeleteEdge, ID: string(edge.ID)})
		}
		for _, tag := range desiredTags[nodeID] {
			tagID := tagByKey[tag.key]
			if linked[tagID] {
				continue
			}
			source, target, label := tagID, nodeID, "标记"
			operations = append(operations, OperationInput{
				Kind: domain.ChangeUpsertEdge, ID: string(domain.TagNodeEdgeID(tagID, nodeID)),
				SourceID: &source, TargetID: &target, Label: &label,
			})
		}
	}
	return operations, nil
}

func orphanTagCleanupOperations(graph domain.Graph) []OperationInput {
	used := make(map[domain.NodeID]bool)
	for _, edge := range graph.Edges {
		if domain.IsTagNodeID(edge.SourceID) && edge.TargetID != domain.RootNodeID && !domain.IsTagNodeID(edge.TargetID) {
			used[edge.SourceID] = true
		}
		if domain.IsTagNodeID(edge.TargetID) && edge.SourceID != domain.RootNodeID && !domain.IsTagNodeID(edge.SourceID) {
			used[edge.TargetID] = true
		}
	}
	orphans := make([]domain.NodeID, 0)
	for _, node := range graph.Nodes {
		if domain.IsTagNodeID(node.ID) && !used[node.ID] {
			orphans = append(orphans, node.ID)
		}
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i] < orphans[j] })
	deleteEdges := make(map[domain.EdgeID]struct{})
	operations := make([]OperationInput, 0)
	for _, tagID := range orphans {
		for _, edge := range graph.Edges {
			if edge.SourceID != tagID && edge.TargetID != tagID {
				continue
			}
			if _, duplicate := deleteEdges[edge.ID]; duplicate {
				continue
			}
			deleteEdges[edge.ID] = struct{}{}
			operations = append(operations, OperationInput{Kind: domain.ChangeDeleteEdge, ID: string(edge.ID)})
		}
		operations = append(operations, OperationInput{Kind: domain.ChangeDeleteNode, ID: string(tagID)})
	}
	return operations
}

func formatTagRevision(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func validateTagRevision(expected string, current time.Time) error {
	if strings.TrimSpace(expected) != formatTagRevision(current) {
		return errors.New("mind map tags changed; list tags again before updating")
	}
	return nil
}

func validateAgentTagTopology(before, candidate domain.Graph) error {
	beforeNodes := make(map[domain.NodeID]struct{}, len(before.Nodes))
	beforeEdges := make(map[domain.EdgeID]domain.Edge, len(before.Edges))
	beforeRootTags := make(map[domain.NodeID]struct{})
	for _, node := range before.Nodes {
		beforeNodes[node.ID] = struct{}{}
	}
	for _, edge := range before.Edges {
		beforeEdges[edge.ID] = edge
		if edge.SourceID == domain.RootNodeID && domain.IsTagNodeID(edge.TargetID) {
			beforeRootTags[edge.TargetID] = struct{}{}
		}
	}
	rootTags := make(map[domain.NodeID]struct{})
	taggedNodes := make(map[domain.NodeID]struct{})
	for _, edge := range candidate.Edges {
		previous, existed := beforeEdges[edge.ID]
		changed := !existed || previous.SourceID != edge.SourceID || previous.TargetID != edge.TargetID
		if changed && (edge.SourceID == domain.RootNodeID || edge.TargetID == domain.RootNodeID) {
			if edge.SourceID != domain.RootNodeID || !domain.IsTagNodeID(edge.TargetID) {
				return fmt.Errorf("new mind map root relationship %q must target a tag", edge.ID)
			}
		}
		if edge.SourceID == domain.RootNodeID && domain.IsTagNodeID(edge.TargetID) {
			rootTags[edge.TargetID] = struct{}{}
		}
		if domain.IsTagNodeID(edge.SourceID) && edge.TargetID != domain.RootNodeID && !domain.IsTagNodeID(edge.TargetID) {
			taggedNodes[edge.TargetID] = struct{}{}
		}
		if domain.IsTagNodeID(edge.TargetID) && edge.SourceID != domain.RootNodeID && !domain.IsTagNodeID(edge.SourceID) {
			taggedNodes[edge.SourceID] = struct{}{}
		}
	}
	for _, node := range candidate.Nodes {
		if _, existed := beforeNodes[node.ID]; existed || node.ID == domain.RootNodeID {
			if _, wasRootTag := beforeRootTags[node.ID]; wasRootTag {
				if _, remainsRootTag := rootTags[node.ID]; !remainsRootTag {
					return fmt.Errorf("mind map tag %q must remain directly connected to project-root", node.ID)
				}
			}
			continue
		}
		if domain.IsTagNodeID(node.ID) {
			normalizedTitle := strings.ToLower(strings.Join(strings.Fields(node.Title), " "))
			if node.ID != domain.ManagedTagNodeID(normalizedTitle) {
				return fmt.Errorf("new mind map tag %q must use a server-managed id", node.ID)
			}
			if _, ok := rootTags[node.ID]; !ok {
				return fmt.Errorf("new mind map tag %q must remain directly connected to project-root", node.ID)
			}
			continue
		}
		if _, tagged := taggedNodes[node.ID]; !tagged {
			return fmt.Errorf("new mind map node %q must connect to a tag", node.ID)
		}
	}
	return nil
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
		if operation.Files != nil {
			files, err := validateNodeFiles(*operation.Files)
			if err != nil {
				return nil, err
			}
			operation.Files = &files
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
			EntityID: id, Title: operation.Title, Content: operation.Content, Files: operation.Files,
			SourceID: operation.SourceID, TargetID: operation.TargetID, Label: operation.Label, OccurredAt: s.now(),
		})
	}
	return changes, nil
}

func validateOperationShape(operation OperationInput) error {
	if operation.Tags != nil {
		return errors.New("mind map tag intent fields must be resolved before persistence")
	}
	hasNodeFields := operation.Title != nil || operation.Content != nil || operation.Files != nil
	hasEdgeFields := operation.SourceID != nil || operation.TargetID != nil || operation.Label != nil
	switch operation.Kind {
	case domain.ChangeUpsertNode:
		if !hasNodeFields {
			return errors.New("mind map node upsert requires a title, content, or files")
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

func validateNodeFiles(files []domain.NodeFile) ([]domain.NodeFile, error) {
	if len(files) > 100 {
		return nil, errors.New("mind map node has too many files")
	}
	result := make([]domain.NodeFile, len(files))
	for index, item := range files {
		item.File = strings.TrimSpace(item.File)
		item.Method = strings.TrimSpace(item.Method)
		if item.File == "" {
			return nil, fmt.Errorf("mind map node file %d requires a file path", index+1)
		}
		if len(item.File) > 2000 {
			return nil, fmt.Errorf("mind map node file %d path is too long", index+1)
		}
		if item.Method == "" {
			return nil, fmt.Errorf("mind map node file %d requires a method", index+1)
		}
		if len(item.Method) > 500 {
			return nil, fmt.Errorf("mind map node file %d method is too long", index+1)
		}
		if item.StartLine < 1 || item.EndLine < item.StartLine {
			return nil, fmt.Errorf("mind map node file %d has an invalid line range", index+1)
		}
		result[index] = item
	}
	return result, nil
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
		fileLocations := strings.Builder{}
		for _, item := range node.Files {
			fileLocations.WriteString("\n")
			fileLocations.WriteString(strings.ToLower(item.File))
			fileLocations.WriteString("\n")
			fileLocations.WriteString(strings.ToLower(item.Method))
		}
		files := fileLocations.String()
		searchable := id + "\n" + title + "\n" + content + files
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
		fields := make([]string, 0, 4)
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
		if containsAny(files, terms) {
			fields = append(fields, "files")
			score += 15
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
		dto.Nodes = append(dto.Nodes, NodeDTO{ID: node.ID, Title: node.Title, Content: node.Content, Files: node.Files, ChangeType: NodeUnchanged})
	}
	dto.Edges = make([]EdgeDTO, 0, len(graph.Edges))
	for _, edge := range graph.Edges {
		dto.Edges = append(dto.Edges, EdgeDTO{ID: edge.ID, SourceID: edge.SourceID, TargetID: edge.TargetID, Label: edge.Label})
	}
	return dto
}

func toDiffDTO(base, current domain.Graph, sessionID domain.SessionID) GraphDTO {
	dto := toDTO(current, sessionID)
	baseByID := make(map[domain.NodeID]domain.Node, len(base.Nodes))
	currentIDs := make(map[domain.NodeID]struct{}, len(current.Nodes))
	for _, node := range base.Nodes {
		baseByID[node.ID] = node
	}
	for index := range dto.Nodes {
		node := &dto.Nodes[index]
		currentIDs[node.ID] = struct{}{}
		baseNode, found := baseByID[node.ID]
		if !found {
			node.ChangeType = NodeAdded
		} else if node.Title != baseNode.Title || node.Content != baseNode.Content || !slices.Equal(node.Files, baseNode.Files) {
			node.ChangeType = NodeModified
		}
	}
	for _, node := range base.Nodes {
		if _, found := currentIDs[node.ID]; found {
			continue
		}
		dto.Nodes = append(dto.Nodes, NodeDTO{
			ID: node.ID, Title: node.Title, Content: node.Content, Files: node.Files, ChangeType: NodeDeleted,
		})
	}
	return dto
}

func cardDeltaDTO(base, current domain.Graph) ([]NodeDTO, []EdgeDTO, []domain.NodeID, []domain.NodeID, []domain.EdgeID) {
	baseNodes := make(map[domain.NodeID]domain.Node, len(base.Nodes))
	currentNodes := make(map[domain.NodeID]domain.Node, len(current.Nodes))
	for _, node := range base.Nodes {
		baseNodes[node.ID] = node
	}
	addedNodeIDs := make(map[domain.NodeID]struct{})
	nodes := make([]NodeDTO, 0)
	modifiedNodeIDs := make([]domain.NodeID, 0)
	for _, node := range current.Nodes {
		currentNodes[node.ID] = node
		baseNode, found := baseNodes[node.ID]
		if !found {
			addedNodeIDs[node.ID] = struct{}{}
			nodes = append(nodes, NodeDTO{ID: node.ID, Title: node.Title, Content: node.Content, Files: node.Files, ChangeType: NodeAdded})
		} else if node.Title != baseNode.Title || node.Content != baseNode.Content || !slices.Equal(node.Files, baseNode.Files) {
			nodes = append(nodes, NodeDTO{ID: node.ID, Title: node.Title, Content: node.Content, Files: node.Files, ChangeType: NodeModified})
			modifiedNodeIDs = append(modifiedNodeIDs, node.ID)
		}
	}
	deletedNodeIDs := make([]domain.NodeID, 0)
	for _, node := range base.Nodes {
		if _, found := currentNodes[node.ID]; !found {
			deletedNodeIDs = append(deletedNodeIDs, node.ID)
		}
	}
	edges := make([]EdgeDTO, 0)
	currentEdgeIDs := make(map[domain.EdgeID]struct{}, len(current.Edges))
	for _, edge := range current.Edges {
		currentEdgeIDs[edge.ID] = struct{}{}
		_, sourceAdded := addedNodeIDs[edge.SourceID]
		_, targetAdded := addedNodeIDs[edge.TargetID]
		if !sourceAdded && !targetAdded {
			continue
		}
		edges = append(edges, EdgeDTO{ID: edge.ID, SourceID: edge.SourceID, TargetID: edge.TargetID, Label: edge.Label})
	}
	deletedEdgeIDs := make([]domain.EdgeID, 0)
	for _, edge := range base.Edges {
		if _, found := currentEdgeIDs[edge.ID]; !found {
			deletedEdgeIDs = append(deletedEdgeIDs, edge.ID)
		}
	}
	return nodes, edges, modifiedNodeIDs, deletedNodeIDs, deletedEdgeIDs
}

func generateID() (domain.ChangeID, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return domain.ChangeID(hex.EncodeToString(value[:])), nil
}

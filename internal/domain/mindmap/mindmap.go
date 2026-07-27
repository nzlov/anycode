package mindmap

import (
	"context"
	"errors"
	"slices"
	"sort"
	"time"
)

var ErrNotFound = errors.New("mind map not found")

type ProjectID string
type SessionID string
type NodeID string
type EdgeID string
type ChangeID string
type TaskID string

type TaskStatus string

const (
	TaskQueued    TaskStatus = "queued"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
)

type ChangeKind string

const RootNodeID NodeID = "project-root"

const (
	ChangeUpsertNode ChangeKind = "upsert_node"
	ChangeDeleteNode ChangeKind = "delete_node"
	ChangeUpsertEdge ChangeKind = "upsert_edge"
	ChangeDeleteEdge ChangeKind = "delete_edge"
)

type Graph struct {
	ProjectID ProjectID
	Nodes     []Node
	Edges     []Edge
	History   []Change
	UpdatedAt time.Time
}

type Node struct {
	ID               NodeID
	Title            string
	Content          string
	Files            []NodeFile
	TitleUpdatedAt   time.Time
	ContentUpdatedAt time.Time
	FilesUpdatedAt   time.Time
	DeletedAt        *time.Time
}

type NodeFile struct {
	File      string
	Method    string
	StartLine int
	EndLine   int
}

type Edge struct {
	ID              EdgeID
	SourceID        NodeID
	TargetID        NodeID
	Label           string
	SourceUpdatedAt time.Time
	TargetUpdatedAt time.Time
	LabelUpdatedAt  time.Time
	DeletedAt       *time.Time
}

type Change struct {
	ID         ChangeID
	ProjectID  ProjectID
	SessionID  SessionID
	Kind       ChangeKind
	EntityID   string
	Title      *string
	Content    *string
	Files      *[]NodeFile
	SourceID   *NodeID
	TargetID   *NodeID
	Label      *string
	OccurredAt time.Time
}

type Overlay struct {
	ProjectID ProjectID
	SessionID SessionID
	Changes   []Change
	UpdatedAt time.Time
}

type GraphPage struct {
	Graph          Graph
	NextNodeCursor NodeID
	NextEdgeCursor EdgeID
}

type Task struct {
	ID           TaskID
	ProjectID    ProjectID
	SessionID    SessionID
	Status       TaskStatus
	ProcessRunID string
	Attempts     int
	Error        string
	CreatedAt    time.Time
	StartedAt    *time.Time
	FinishedAt   *time.Time
	UpdatedAt    time.Time
}

type Repository interface {
	FindGraph(ctx context.Context, projectID ProjectID) (Graph, bool, error)
	FindGraphPage(ctx context.Context, projectID ProjectID, nodeAfter NodeID, edgeAfter EdgeID, nodeLimit int, edgeLimit int) (GraphPage, bool, error)
	FindRevision(ctx context.Context, projectID ProjectID, sessionID SessionID) (time.Time, error)
	SaveGraph(ctx context.Context, graph Graph, changes []Change) error
	FindOverlay(ctx context.Context, sessionID SessionID) (Overlay, bool, error)
	ListOverlays(ctx context.Context, projectID ProjectID) ([]Overlay, error)
	SaveOverlay(ctx context.Context, overlay Overlay) error
	DeleteOverlay(ctx context.Context, sessionID SessionID) error
	SaveTask(ctx context.Context, task Task) error
	FindTask(ctx context.Context, id TaskID) (Task, error)
	FindTaskBySession(ctx context.Context, sessionID SessionID) (Task, bool, error)
	ListTasks(ctx context.Context, projectID ProjectID) ([]Task, error)
	ListQueuedTasks(ctx context.Context, limit int) ([]Task, error)
	CountRunningTasks(ctx context.Context) (int, error)
}

func EnsureRoot(graph *Graph, projectName string, updatedAt time.Time) {
	if graph == nil {
		return
	}
	if updatedAt.After(graph.UpdatedAt) {
		graph.UpdatedAt = updatedAt
	}
	index := nodeIndex(graph.Nodes, RootNodeID)
	if index < 0 {
		graph.Nodes = append([]Node{{
			ID: RootNodeID, Title: projectName, TitleUpdatedAt: updatedAt,
		}}, graph.Nodes...)
		return
	}
	root := &graph.Nodes[index]
	root.Title = projectName
	root.DeletedAt = nil
	if updatedAt.After(root.TitleUpdatedAt) {
		root.TitleUpdatedAt = updatedAt
	}
}

func Touch(graph *Graph, updatedAt time.Time) {
	if graph == nil {
		return
	}
	graph.UpdatedAt = NextUpdatedAt(graph.UpdatedAt, updatedAt)
}

func NextUpdatedAt(current, updatedAt time.Time) time.Time {
	if updatedAt.After(current) {
		return updatedAt
	}
	return current.Add(time.Nanosecond)
}

func Materialize(graph Graph, changes []Change) Graph {
	result := cloneGraph(graph)
	ordered := append([]Change(nil), changes...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].OccurredAt.Before(ordered[j].OccurredAt) })
	for _, change := range ordered {
		Apply(&result, change)
	}
	return Visible(result)
}

func Visible(graph Graph) Graph {
	result := Normalize(graph)
	normalizedNodes := result.Nodes
	result.Nodes = make([]Node, 0, len(normalizedNodes))
	visibleNodes := make(map[NodeID]struct{}, len(normalizedNodes))
	for _, node := range normalizedNodes {
		if node.DeletedAt != nil {
			continue
		}
		result.Nodes = append(result.Nodes, node)
		visibleNodes[node.ID] = struct{}{}
	}
	normalizedEdges := result.Edges
	result.Edges = make([]Edge, 0, len(normalizedEdges))
	for _, edge := range normalizedEdges {
		if edge.DeletedAt != nil {
			continue
		}
		if _, ok := visibleNodes[edge.SourceID]; !ok {
			continue
		}
		if _, ok := visibleNodes[edge.TargetID]; !ok {
			continue
		}
		result.Edges = append(result.Edges, edge)
	}
	return result
}

// Normalize repairs legacy duplicate records while preserving the newest value of each field.
func Normalize(graph Graph) Graph {
	result := graph
	result.Nodes = make([]Node, 0, len(graph.Nodes))
	nodeByID := make(map[NodeID]int, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if index, ok := nodeByID[node.ID]; ok {
			mergeNode(&result.Nodes[index], node)
			continue
		}
		nodeByID[node.ID] = len(result.Nodes)
		result.Nodes = append(result.Nodes, node)
	}
	result.Edges = make([]Edge, 0, len(graph.Edges))
	edgeByID := make(map[EdgeID]int, len(graph.Edges))
	for _, edge := range graph.Edges {
		if _, ok := nodeByID[edge.SourceID]; !ok {
			continue
		}
		if _, ok := nodeByID[edge.TargetID]; !ok {
			continue
		}
		if index, ok := edgeByID[edge.ID]; ok {
			mergeEdge(&result.Edges[index], edge)
			continue
		}
		edgeByID[edge.ID] = len(result.Edges)
		result.Edges = append(result.Edges, edge)
	}
	return result
}

func Apply(graph *Graph, change Change) {
	if graph == nil {
		return
	}
	if graph.ProjectID == "" {
		graph.ProjectID = change.ProjectID
	}
	switch change.Kind {
	case ChangeUpsertNode:
		applyNode(graph, change)
	case ChangeDeleteNode:
		deleteNode(graph, change)
	case ChangeUpsertEdge:
		applyEdge(graph, change)
	case ChangeDeleteEdge:
		deleteEdge(graph, change)
	}
	if change.OccurredAt.After(graph.UpdatedAt) {
		graph.UpdatedAt = change.OccurredAt
	}
}

func MergeOverlay(graph *Graph, overlay Overlay) {
	if graph == nil {
		return
	}
	ordered := append([]Change(nil), overlay.Changes...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].OccurredAt.Before(ordered[j].OccurredAt) })
	for _, change := range ordered {
		Apply(graph, change)
		graph.History = append(graph.History, change)
	}
}

// CompactOverlay replaces an operation history with the effective per-entity delta from the project graph.
func CompactOverlay(base Graph, overlay Overlay) Overlay {
	candidate := cloneGraph(base)
	ordered := append([]Change(nil), overlay.Changes...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].OccurredAt.Before(ordered[j].OccurredAt) })
	type nodeChanges struct {
		title   *Change
		content *Change
		files   *Change
		deleted *Change
	}
	type edgeChanges struct {
		endpoints *Change
		label     *Change
		deleted   *Change
	}
	latestNodeChanges := make(map[NodeID]nodeChanges)
	latestEdgeChanges := make(map[EdgeID]edgeChanges)
	for _, change := range ordered {
		Apply(&candidate, change)
		switch change.Kind {
		case ChangeUpsertNode:
			id := NodeID(change.EntityID)
			fields := latestNodeChanges[id]
			if change.Title != nil {
				value := change
				fields.title = &value
			}
			if change.Content != nil {
				value := change
				fields.content = &value
			}
			if change.Files != nil {
				value := change
				fields.files = &value
			}
			latestNodeChanges[id] = fields
		case ChangeDeleteNode:
			id := NodeID(change.EntityID)
			fields := latestNodeChanges[id]
			value := change
			fields.deleted = &value
			latestNodeChanges[id] = fields
		case ChangeUpsertEdge:
			id := EdgeID(change.EntityID)
			fields := latestEdgeChanges[id]
			if change.SourceID != nil || change.TargetID != nil {
				value := change
				fields.endpoints = &value
			}
			if change.Label != nil {
				value := change
				fields.label = &value
			}
			latestEdgeChanges[id] = fields
		case ChangeDeleteEdge:
			id := EdgeID(change.EntityID)
			fields := latestEdgeChanges[id]
			value := change
			fields.deleted = &value
			latestEdgeChanges[id] = fields
		}
	}
	base = Visible(base)
	current := Visible(candidate)
	baseNodes := make(map[NodeID]Node, len(base.Nodes))
	currentNodes := make(map[NodeID]Node, len(current.Nodes))
	for _, node := range base.Nodes {
		baseNodes[node.ID] = node
	}
	for _, node := range current.Nodes {
		currentNodes[node.ID] = node
	}
	baseEdges := make(map[EdgeID]Edge, len(base.Edges))
	currentEdges := make(map[EdgeID]Edge, len(current.Edges))
	for _, edge := range base.Edges {
		baseEdges[edge.ID] = edge
	}
	for _, edge := range current.Edges {
		currentEdges[edge.ID] = edge
	}

	compacted := make([]Change, 0, len(latestNodeChanges)+len(latestEdgeChanges))
	for _, node := range current.Nodes {
		baseNode, found := baseNodes[node.ID]
		fields := latestNodeChanges[node.ID]
		titleChanged := !found || node.Title != baseNode.Title
		contentChanged := (!found && node.Content != "") || (found && node.Content != baseNode.Content)
		filesChanged := (!found && len(node.Files) > 0) || (found && !slices.Equal(node.Files, baseNode.Files))
		combinedTextChange := titleChanged && contentChanged && fields.title != nil && fields.content != nil && fields.title.ID == fields.content.ID && fields.title.OccurredAt.Equal(fields.content.OccurredAt)
		if combinedTextChange {
			change := *fields.title
			title, content := node.Title, node.Content
			change.Kind, change.EntityID = ChangeUpsertNode, string(node.ID)
			change.Title, change.Content = &title, &content
			change.Files, change.SourceID, change.TargetID, change.Label = nil, nil, nil, nil
			compacted = append(compacted, change)
		} else {
			if titleChanged && fields.title != nil {
				change := *fields.title
				title := node.Title
				change.Kind, change.EntityID = ChangeUpsertNode, string(node.ID)
				change.Title, change.Content = &title, nil
				change.Files, change.SourceID, change.TargetID, change.Label = nil, nil, nil, nil
				compacted = append(compacted, change)
			}
			if contentChanged && fields.content != nil {
				change := *fields.content
				content := node.Content
				change.Kind, change.EntityID = ChangeUpsertNode, string(node.ID)
				change.Title, change.Content = nil, &content
				change.Files, change.SourceID, change.TargetID, change.Label = nil, nil, nil, nil
				compacted = append(compacted, change)
			}
		}
		if filesChanged && fields.files != nil {
			change := *fields.files
			files := append([]NodeFile(nil), node.Files...)
			change.Kind, change.EntityID = ChangeUpsertNode, string(node.ID)
			change.Title, change.Content, change.Files = nil, nil, &files
			change.SourceID, change.TargetID, change.Label = nil, nil, nil
			compacted = append(compacted, change)
		}
	}
	for _, edge := range current.Edges {
		baseEdge, found := baseEdges[edge.ID]
		fields := latestEdgeChanges[edge.ID]
		endpointsChanged := !found || edge.SourceID != baseEdge.SourceID || edge.TargetID != baseEdge.TargetID
		labelChanged := (!found && edge.Label != "") || (found && edge.Label != baseEdge.Label)
		if endpointsChanged && labelChanged && fields.endpoints != nil && fields.label != nil && fields.endpoints.ID == fields.label.ID && fields.endpoints.OccurredAt.Equal(fields.label.OccurredAt) {
			change := *fields.endpoints
			sourceID, targetID, label := edge.SourceID, edge.TargetID, edge.Label
			change.Kind, change.EntityID = ChangeUpsertEdge, string(edge.ID)
			change.Title, change.Content, change.Files = nil, nil, nil
			change.SourceID, change.TargetID, change.Label = &sourceID, &targetID, &label
			compacted = append(compacted, change)
			continue
		}
		if endpointsChanged && fields.endpoints != nil {
			change := *fields.endpoints
			sourceID, targetID := edge.SourceID, edge.TargetID
			change.Kind, change.EntityID = ChangeUpsertEdge, string(edge.ID)
			change.Title, change.Content, change.Files, change.Label = nil, nil, nil, nil
			change.SourceID, change.TargetID = &sourceID, &targetID
			compacted = append(compacted, change)
		}
		if labelChanged && fields.label != nil {
			change := *fields.label
			label := edge.Label
			change.Kind, change.EntityID = ChangeUpsertEdge, string(edge.ID)
			change.Title, change.Content, change.Files, change.SourceID, change.TargetID = nil, nil, nil, nil, nil
			change.Label = &label
			compacted = append(compacted, change)
		}
	}
	for _, edge := range base.Edges {
		if _, found := currentEdges[edge.ID]; found {
			continue
		}
		if _, sourceExists := currentNodes[edge.SourceID]; !sourceExists {
			continue
		}
		if _, targetExists := currentNodes[edge.TargetID]; !targetExists {
			continue
		}
		fields := latestEdgeChanges[edge.ID]
		var change Change
		if fields.deleted != nil {
			change = *fields.deleted
		} else {
			sourceDelete := latestNodeChanges[edge.SourceID].deleted
			targetDelete := latestNodeChanges[edge.TargetID].deleted
			switch {
			case sourceDelete == nil && targetDelete == nil:
				continue
			case targetDelete == nil || (sourceDelete != nil && sourceDelete.OccurredAt.Before(targetDelete.OccurredAt)):
				change = *sourceDelete
			default:
				change = *targetDelete
			}
			change.ID = ChangeID(string(change.ID) + ":edge:" + string(edge.ID))
		}
		change.Kind = ChangeDeleteEdge
		change.EntityID = string(edge.ID)
		change.Title, change.Content, change.Files, change.SourceID, change.TargetID, change.Label = nil, nil, nil, nil, nil, nil
		compacted = append(compacted, change)
	}
	for _, node := range base.Nodes {
		if _, found := currentNodes[node.ID]; found {
			continue
		}
		change := latestNodeChanges[node.ID].deleted
		if change == nil {
			continue
		}
		value := *change
		value.Kind = ChangeDeleteNode
		value.EntityID = string(node.ID)
		value.Title, value.Content, value.Files, value.SourceID, value.TargetID, value.Label = nil, nil, nil, nil, nil, nil
		compacted = append(compacted, value)
	}
	overlay.Changes = compacted
	return overlay
}

func applyNode(graph *Graph, change Change) {
	id := NodeID(change.EntityID)
	index := nodeIndex(graph.Nodes, id)
	if index < 0 {
		graph.Nodes = append(graph.Nodes, Node{ID: id})
		index = len(graph.Nodes) - 1
	}
	node := &graph.Nodes[index]
	if node.DeletedAt != nil && !change.OccurredAt.Before(*node.DeletedAt) {
		node.DeletedAt = nil
	}
	if change.Title != nil && !change.OccurredAt.Before(node.TitleUpdatedAt) {
		node.Title = *change.Title
		node.TitleUpdatedAt = change.OccurredAt
	}
	if change.Content != nil && !change.OccurredAt.Before(node.ContentUpdatedAt) {
		node.Content = *change.Content
		node.ContentUpdatedAt = change.OccurredAt
	}
	if change.Files != nil && !change.OccurredAt.Before(node.FilesUpdatedAt) {
		node.Files = append([]NodeFile(nil), (*change.Files)...)
		node.FilesUpdatedAt = change.OccurredAt
	}
}

func deleteNode(graph *Graph, change Change) {
	id := NodeID(change.EntityID)
	for index := range graph.Nodes {
		node := &graph.Nodes[index]
		if node.ID != id || change.OccurredAt.Before(latestTime(node.TitleUpdatedAt, node.ContentUpdatedAt, node.FilesUpdatedAt)) {
			continue
		}
		if node.DeletedAt == nil || !change.OccurredAt.Before(*node.DeletedAt) {
			deletedAt := change.OccurredAt
			node.DeletedAt = &deletedAt
		}
	}
	for index := range graph.Edges {
		edge := &graph.Edges[index]
		if edge.SourceID != id && edge.TargetID != id {
			continue
		}
		deleteEdgeAt(edge, change.OccurredAt)
	}
}

func applyEdge(graph *Graph, change Change) {
	id := EdgeID(change.EntityID)
	index := edgeIndex(graph.Edges, id)
	if index < 0 {
		graph.Edges = append(graph.Edges, Edge{ID: id})
		index = len(graph.Edges) - 1
	}
	edge := &graph.Edges[index]
	if edge.DeletedAt != nil && !change.OccurredAt.Before(*edge.DeletedAt) {
		edge.DeletedAt = nil
	}
	if change.SourceID != nil && !change.OccurredAt.Before(edge.SourceUpdatedAt) {
		edge.SourceID = *change.SourceID
		edge.SourceUpdatedAt = change.OccurredAt
	}
	if change.TargetID != nil && !change.OccurredAt.Before(edge.TargetUpdatedAt) {
		edge.TargetID = *change.TargetID
		edge.TargetUpdatedAt = change.OccurredAt
	}
	if change.Label != nil && !change.OccurredAt.Before(edge.LabelUpdatedAt) {
		edge.Label = *change.Label
		edge.LabelUpdatedAt = change.OccurredAt
	}
}

func deleteEdge(graph *Graph, change Change) {
	id := EdgeID(change.EntityID)
	for index := range graph.Edges {
		edge := &graph.Edges[index]
		if edge.ID != id {
			continue
		}
		deleteEdgeAt(edge, change.OccurredAt)
	}
}

func deleteEdgeAt(edge *Edge, occurredAt time.Time) {
	if edge == nil || occurredAt.Before(latestTime(edge.SourceUpdatedAt, edge.TargetUpdatedAt, edge.LabelUpdatedAt)) {
		return
	}
	if edge.DeletedAt == nil || !occurredAt.Before(*edge.DeletedAt) {
		deletedAt := occurredAt
		edge.DeletedAt = &deletedAt
	}
}

func mergeNode(target *Node, candidate Node) {
	if !candidate.TitleUpdatedAt.Before(target.TitleUpdatedAt) {
		target.Title = candidate.Title
		target.TitleUpdatedAt = candidate.TitleUpdatedAt
	}
	if !candidate.ContentUpdatedAt.Before(target.ContentUpdatedAt) {
		target.Content = candidate.Content
		target.ContentUpdatedAt = candidate.ContentUpdatedAt
	}
	if !candidate.FilesUpdatedAt.Before(target.FilesUpdatedAt) {
		target.Files = append([]NodeFile(nil), candidate.Files...)
		target.FilesUpdatedAt = candidate.FilesUpdatedAt
	}
	target.DeletedAt = latestDeletion(target.DeletedAt, candidate.DeletedAt)
	if target.DeletedAt != nil && latestTime(target.TitleUpdatedAt, target.ContentUpdatedAt, target.FilesUpdatedAt).After(*target.DeletedAt) {
		target.DeletedAt = nil
	}
}

func mergeEdge(target *Edge, candidate Edge) {
	if !candidate.SourceUpdatedAt.Before(target.SourceUpdatedAt) {
		target.SourceID = candidate.SourceID
		target.SourceUpdatedAt = candidate.SourceUpdatedAt
	}
	if !candidate.TargetUpdatedAt.Before(target.TargetUpdatedAt) {
		target.TargetID = candidate.TargetID
		target.TargetUpdatedAt = candidate.TargetUpdatedAt
	}
	if !candidate.LabelUpdatedAt.Before(target.LabelUpdatedAt) {
		target.Label = candidate.Label
		target.LabelUpdatedAt = candidate.LabelUpdatedAt
	}
	target.DeletedAt = latestDeletion(target.DeletedAt, candidate.DeletedAt)
	if target.DeletedAt != nil && latestTime(target.SourceUpdatedAt, target.TargetUpdatedAt, target.LabelUpdatedAt).After(*target.DeletedAt) {
		target.DeletedAt = nil
	}
}

func latestDeletion(left, right *time.Time) *time.Time {
	if left == nil {
		return right
	}
	if right == nil || left.After(*right) {
		return left
	}
	return right
}

func latestTime(values ...time.Time) time.Time {
	var latest time.Time
	for _, value := range values {
		if value.After(latest) {
			latest = value
		}
	}
	return latest
}

func nodeIndex(nodes []Node, id NodeID) int {
	for index := range nodes {
		if nodes[index].ID == id {
			return index
		}
	}
	return -1
}

func edgeIndex(edges []Edge, id EdgeID) int {
	for index := range edges {
		if edges[index].ID == id {
			return index
		}
	}
	return -1
}

func cloneGraph(graph Graph) Graph {
	result := graph
	result.Nodes = append([]Node(nil), graph.Nodes...)
	for index := range result.Nodes {
		result.Nodes[index].Files = append([]NodeFile(nil), result.Nodes[index].Files...)
	}
	result.Edges = append([]Edge(nil), graph.Edges...)
	result.History = append([]Change(nil), graph.History...)
	return result
}

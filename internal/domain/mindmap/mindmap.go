package mindmap

import (
	"context"
	"errors"
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
	ID                NodeID
	Title             string
	Content           string
	X                 float64
	Y                 float64
	TitleUpdatedAt    time.Time
	ContentUpdatedAt  time.Time
	PositionUpdatedAt time.Time
	DeletedAt         *time.Time
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
	X          *float64
	Y          *float64
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
	SaveGraph(ctx context.Context, graph Graph) error
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
		graph.Nodes = append(graph.Nodes, Node{
			ID: RootNodeID, Title: projectName, X: 0, Y: 0,
			TitleUpdatedAt: updatedAt, PositionUpdatedAt: updatedAt,
		})
		return
	}
	root := &graph.Nodes[index]
	root.Title = projectName
	root.X = 0
	root.Y = 0
	root.DeletedAt = nil
	if updatedAt.After(root.TitleUpdatedAt) {
		root.TitleUpdatedAt = updatedAt
	}
	if updatedAt.After(root.PositionUpdatedAt) {
		root.PositionUpdatedAt = updatedAt
	}
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
	result := graph
	result.Nodes = make([]Node, 0, len(graph.Nodes))
	visibleNodes := make(map[NodeID]struct{}, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if node.DeletedAt != nil {
			continue
		}
		result.Nodes = append(result.Nodes, node)
		visibleNodes[node.ID] = struct{}{}
	}
	result.Edges = make([]Edge, 0, len(graph.Edges))
	for _, edge := range graph.Edges {
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
	if (change.X != nil || change.Y != nil) && !change.OccurredAt.Before(node.PositionUpdatedAt) {
		if change.X != nil {
			node.X = *change.X
		}
		if change.Y != nil {
			node.Y = *change.Y
		}
		node.PositionUpdatedAt = change.OccurredAt
	}
}

func deleteNode(graph *Graph, change Change) {
	id := NodeID(change.EntityID)
	for index := range graph.Nodes {
		node := &graph.Nodes[index]
		if node.ID != id || change.OccurredAt.Before(latestTime(node.TitleUpdatedAt, node.ContentUpdatedAt, node.PositionUpdatedAt)) {
			continue
		}
		if node.DeletedAt == nil || !change.OccurredAt.Before(*node.DeletedAt) {
			deletedAt := change.OccurredAt
			node.DeletedAt = &deletedAt
		}
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
		if edge.ID != id || change.OccurredAt.Before(latestTime(edge.SourceUpdatedAt, edge.TargetUpdatedAt, edge.LabelUpdatedAt)) {
			continue
		}
		if edge.DeletedAt == nil || !change.OccurredAt.Before(*edge.DeletedAt) {
			deletedAt := change.OccurredAt
			edge.DeletedAt = &deletedAt
		}
	}
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
	result.Edges = append([]Edge(nil), graph.Edges...)
	result.History = append([]Change(nil), graph.History...)
	return result
}

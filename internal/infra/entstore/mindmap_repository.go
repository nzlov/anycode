package entstore

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/nzlov/anycode/internal/domain/mindmap"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	entedge "github.com/nzlov/anycode/internal/infra/entstore/ent/mindmapedge"
	entnode "github.com/nzlov/anycode/internal/infra/entstore/ent/mindmapnode"
	entoverlay "github.com/nzlov/anycode/internal/infra/entstore/ent/mindmapoverlay"
	enttask "github.com/nzlov/anycode/internal/infra/entstore/ent/mindmaptask"
	entschema "github.com/nzlov/anycode/internal/infra/entstore/ent/schema"
)

type MindMapRepository struct {
	client *ent.Client
}

func NewMindMapRepository(client *ent.Client) *MindMapRepository {
	return &MindMapRepository{client: client}
}

func (r *MindMapRepository) FindGraph(ctx context.Context, projectID mindmap.ProjectID) (mindmap.Graph, bool, error) {
	row, err := r.client.MindMapGraph.Get(ctx, string(projectID))
	if ent.IsNotFound(err) {
		return mindmap.Graph{ProjectID: projectID}, false, nil
	}
	if err != nil {
		return mindmap.Graph{}, false, fmt.Errorf("find mind map graph: %w", err)
	}
	nodes, edges, err := r.findScope(ctx, projectID, "")
	if err != nil {
		return mindmap.Graph{}, false, err
	}
	return mindmap.Normalize(mindmap.Graph{ProjectID: projectID, Nodes: nodes, Edges: edges, UpdatedAt: row.UpdatedAt}), true, nil
}

func (r *MindMapRepository) FindGraphPage(ctx context.Context, projectID mindmap.ProjectID, nodeAfter mindmap.NodeID, edgeAfter mindmap.EdgeID, nodeLimit int, edgeLimit int) (mindmap.GraphPage, bool, error) {
	row, err := r.client.MindMapGraph.Get(ctx, string(projectID))
	if ent.IsNotFound(err) {
		return mindmap.GraphPage{Graph: mindmap.Graph{ProjectID: projectID}}, false, nil
	}
	if err != nil {
		return mindmap.GraphPage{}, false, fmt.Errorf("find mind map graph page: %w", err)
	}
	page := mindmap.GraphPage{Graph: mindmap.Graph{ProjectID: projectID, UpdatedAt: row.UpdatedAt}}
	if nodeLimit > 0 {
		query := r.client.MindMapNode.Query().Where(
			entnode.ProjectIDEQ(string(projectID)), entnode.SessionIDEQ(""), entnode.DeletedAtIsNil(),
			entnode.NodeIDNEQ(string(mindmap.RootNodeID)), entnode.NodeIDGT(string(nodeAfter)),
		).Order(ent.Asc(entnode.FieldNodeID)).Limit(nodeLimit + 1)
		rows, err := query.All(ctx)
		if err != nil {
			return mindmap.GraphPage{}, false, fmt.Errorf("find mind map node page: %w", err)
		}
		if len(rows) > nodeLimit {
			rows = rows[:nodeLimit]
			page.NextNodeCursor = mindmap.NodeID(rows[len(rows)-1].NodeID)
		}
		page.Graph.Nodes = make([]mindmap.Node, 0, len(rows))
		for _, item := range rows {
			page.Graph.Nodes = append(page.Graph.Nodes, toDomainMindMapNode(item))
		}
	}
	if edgeLimit > 0 {
		query := r.client.MindMapEdge.Query().Where(
			entedge.ProjectIDEQ(string(projectID)), entedge.SessionIDEQ(""), entedge.DeletedAtIsNil(),
			entedge.EdgeIDGT(string(edgeAfter)),
		).Order(ent.Asc(entedge.FieldEdgeID)).Limit(edgeLimit + 1)
		rows, err := query.All(ctx)
		if err != nil {
			return mindmap.GraphPage{}, false, fmt.Errorf("find mind map edge page: %w", err)
		}
		if len(rows) > edgeLimit {
			rows = rows[:edgeLimit]
			page.NextEdgeCursor = mindmap.EdgeID(rows[len(rows)-1].EdgeID)
		}
		page.Graph.Edges = make([]mindmap.Edge, 0, len(rows))
		for _, item := range rows {
			page.Graph.Edges = append(page.Graph.Edges, toDomainMindMapEdge(item))
		}
	}
	return page, true, nil
}

func (r *MindMapRepository) FindRevision(ctx context.Context, projectID mindmap.ProjectID, sessionID mindmap.SessionID) (time.Time, error) {
	var updatedAt time.Time
	row, err := r.client.MindMapGraph.Get(ctx, string(projectID))
	if err == nil {
		updatedAt = row.UpdatedAt
	} else if !ent.IsNotFound(err) {
		return time.Time{}, fmt.Errorf("find mind map graph revision: %w", err)
	}
	if sessionID != "" {
		overlay, err := r.client.MindMapOverlay.Get(ctx, string(sessionID))
		if ent.IsNotFound(err) {
			return updatedAt, nil
		}
		if err != nil {
			return time.Time{}, fmt.Errorf("find mind map overlay revision: %w", err)
		}
		if overlay.UpdatedAt.After(updatedAt) {
			updatedAt = overlay.UpdatedAt
		}
		return updatedAt, nil
	}
	overlay, err := r.client.MindMapOverlay.Query().Where(entoverlay.ProjectIDEQ(string(projectID))).
		Order(ent.Desc(entoverlay.FieldUpdatedAt)).First(ctx)
	if err == nil && overlay.UpdatedAt.After(updatedAt) {
		updatedAt = overlay.UpdatedAt
	} else if err != nil && !ent.IsNotFound(err) {
		return time.Time{}, fmt.Errorf("find latest mind map overlay revision: %w", err)
	}
	return updatedAt, nil
}

func (r *MindMapRepository) SaveGraph(ctx context.Context, graph mindmap.Graph, changes []mindmap.Change) error {
	graph = mindmap.Normalize(graph)
	nodeIDs, edgeIDs := changedEntityIDs(graph, changes)
	if err := r.replaceNodes(ctx, graph.ProjectID, "", graph.Nodes, nodeIDs, false); err != nil {
		return err
	}
	if err := r.replaceEdges(ctx, graph.ProjectID, "", graph.Edges, edgeIDs, false); err != nil {
		return err
	}
	update := r.client.MindMapGraph.UpdateOneID(string(graph.ProjectID))
	if !graph.UpdatedAt.IsZero() {
		update.SetUpdatedAt(graph.UpdatedAt)
	}
	if err := update.Exec(ctx); err == nil {
		return nil
	} else if !ent.IsNotFound(err) {
		return fmt.Errorf("update mind map graph: %w", err)
	}
	create := r.client.MindMapGraph.Create().SetID(string(graph.ProjectID))
	if !graph.UpdatedAt.IsZero() {
		create.SetUpdatedAt(graph.UpdatedAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("create mind map graph: %w", err)
	}
	return nil
}

func (r *MindMapRepository) FindOverlay(ctx context.Context, sessionID mindmap.SessionID) (mindmap.Overlay, bool, error) {
	row, err := r.client.MindMapOverlay.Get(ctx, string(sessionID))
	if ent.IsNotFound(err) {
		return mindmap.Overlay{SessionID: sessionID}, false, nil
	}
	if err != nil {
		return mindmap.Overlay{}, false, fmt.Errorf("find mind map overlay: %w", err)
	}
	nodes, edges, err := r.findScope(ctx, mindmap.ProjectID(row.ProjectID), sessionID)
	if err != nil {
		return mindmap.Overlay{}, false, err
	}
	return mindmap.Overlay{
		ProjectID: mindmap.ProjectID(row.ProjectID), SessionID: sessionID,
		Changes: scopeChanges(mindmap.ProjectID(row.ProjectID), sessionID, nodes, edges), UpdatedAt: row.UpdatedAt,
	}, true, nil
}

func (r *MindMapRepository) ListOverlays(ctx context.Context, projectID mindmap.ProjectID) ([]mindmap.Overlay, error) {
	rows, err := r.client.MindMapOverlay.Query().
		Where(entoverlay.ProjectIDEQ(string(projectID))).
		Order(ent.Desc(entoverlay.FieldUpdatedAt)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list mind map overlays: %w", err)
	}
	nodeRows, err := r.client.MindMapNode.Query().Where(
		entnode.ProjectIDEQ(string(projectID)), entnode.SessionIDNEQ(""),
	).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list card mind map nodes: %w", err)
	}
	edgeRows, err := r.client.MindMapEdge.Query().Where(
		entedge.ProjectIDEQ(string(projectID)), entedge.SessionIDNEQ(""),
	).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list card mind map edges: %w", err)
	}
	nodesBySession := make(map[mindmap.SessionID][]mindmap.Node)
	for _, row := range nodeRows {
		sessionID := mindmap.SessionID(row.SessionID)
		nodesBySession[sessionID] = append(nodesBySession[sessionID], toDomainMindMapNode(row))
	}
	edgesBySession := make(map[mindmap.SessionID][]mindmap.Edge)
	for _, row := range edgeRows {
		sessionID := mindmap.SessionID(row.SessionID)
		edgesBySession[sessionID] = append(edgesBySession[sessionID], toDomainMindMapEdge(row))
	}
	result := make([]mindmap.Overlay, 0, len(rows))
	for _, row := range rows {
		sessionID := mindmap.SessionID(row.ID)
		result = append(result, mindmap.Overlay{
			ProjectID: projectID, SessionID: sessionID, UpdatedAt: row.UpdatedAt,
			Changes: scopeChanges(projectID, sessionID, nodesBySession[sessionID], edgesBySession[sessionID]),
		})
	}
	return result, nil
}

func (r *MindMapRepository) SaveOverlay(ctx context.Context, overlay mindmap.Overlay) error {
	nodes, edges := scopeEntities(overlay.Changes)
	if err := r.replaceNodes(ctx, overlay.ProjectID, overlay.SessionID, nodes, nil, true); err != nil {
		return err
	}
	if err := r.replaceEdges(ctx, overlay.ProjectID, overlay.SessionID, edges, nil, true); err != nil {
		return err
	}
	update := r.client.MindMapOverlay.UpdateOneID(string(overlay.SessionID)).
		SetProjectID(string(overlay.ProjectID))
	if !overlay.UpdatedAt.IsZero() {
		update.SetUpdatedAt(overlay.UpdatedAt)
	}
	if err := update.Exec(ctx); err == nil {
		return nil
	} else if !ent.IsNotFound(err) {
		return fmt.Errorf("update mind map overlay: %w", err)
	}
	create := r.client.MindMapOverlay.Create().SetID(string(overlay.SessionID)).SetProjectID(string(overlay.ProjectID))
	if !overlay.UpdatedAt.IsZero() {
		create.SetUpdatedAt(overlay.UpdatedAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("create mind map overlay: %w", err)
	}
	return nil
}

func (r *MindMapRepository) DeleteOverlay(ctx context.Context, sessionID mindmap.SessionID) error {
	if _, err := r.client.MindMapNode.Delete().Where(entnode.SessionIDEQ(string(sessionID))).Exec(ctx); err != nil {
		return fmt.Errorf("delete mind map overlay nodes: %w", err)
	}
	if _, err := r.client.MindMapEdge.Delete().Where(entedge.SessionIDEQ(string(sessionID))).Exec(ctx); err != nil {
		return fmt.Errorf("delete mind map overlay edges: %w", err)
	}
	if err := r.client.MindMapOverlay.DeleteOneID(string(sessionID)).Exec(ctx); err != nil && !ent.IsNotFound(err) {
		return fmt.Errorf("delete mind map overlay: %w", err)
	}
	return nil
}

func (r *MindMapRepository) SaveTask(ctx context.Context, task mindmap.Task) error {
	update := r.client.MindMapTask.UpdateOneID(string(task.ID)).
		SetProjectID(string(task.ProjectID)).SetSessionID(string(task.SessionID)).SetStatus(string(task.Status)).
		SetProcessRunID(task.ProcessRunID).SetAttempts(task.Attempts).SetError(task.Error)
	setMindMapTaskTimesOnUpdate(update, task)
	if err := update.Exec(ctx); err == nil {
		return nil
	} else if !ent.IsNotFound(err) {
		return fmt.Errorf("update mind map task: %w", err)
	}
	create := r.client.MindMapTask.Create().SetID(string(task.ID)).SetProjectID(string(task.ProjectID)).
		SetSessionID(string(task.SessionID)).SetStatus(string(task.Status)).SetProcessRunID(task.ProcessRunID).
		SetAttempts(task.Attempts).SetError(task.Error)
	if !task.CreatedAt.IsZero() {
		create.SetCreatedAt(task.CreatedAt)
	}
	if task.StartedAt != nil {
		create.SetStartedAt(*task.StartedAt)
	}
	if task.FinishedAt != nil {
		create.SetFinishedAt(*task.FinishedAt)
	}
	if !task.UpdatedAt.IsZero() {
		create.SetUpdatedAt(task.UpdatedAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("create mind map task: %w", err)
	}
	return nil
}

func (r *MindMapRepository) FindTask(ctx context.Context, id mindmap.TaskID) (mindmap.Task, error) {
	row, err := r.client.MindMapTask.Get(ctx, string(id))
	if err != nil {
		return mindmap.Task{}, fmt.Errorf("find mind map task: %w", err)
	}
	return toDomainMindMapTask(row), nil
}

func (r *MindMapRepository) FindTaskBySession(ctx context.Context, sessionID mindmap.SessionID) (mindmap.Task, bool, error) {
	row, err := r.client.MindMapTask.Query().Where(enttask.SessionIDEQ(string(sessionID))).First(ctx)
	if ent.IsNotFound(err) {
		return mindmap.Task{}, false, nil
	}
	if err != nil {
		return mindmap.Task{}, false, fmt.Errorf("find mind map task by session: %w", err)
	}
	return toDomainMindMapTask(row), true, nil
}

func (r *MindMapRepository) ListTasks(ctx context.Context, projectID mindmap.ProjectID) ([]mindmap.Task, error) {
	query := r.client.MindMapTask.Query()
	if projectID != "" {
		query.Where(enttask.ProjectIDEQ(string(projectID)))
	}
	rows, err := query.Order(ent.Desc(enttask.FieldCreatedAt)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list mind map tasks: %w", err)
	}
	result := make([]mindmap.Task, 0, len(rows))
	for _, row := range rows {
		result = append(result, toDomainMindMapTask(row))
	}
	return result, nil
}

func (r *MindMapRepository) ListQueuedTasks(ctx context.Context, limit int) ([]mindmap.Task, error) {
	query := r.client.MindMapTask.Query().Where(enttask.StatusEQ(string(mindmap.TaskQueued))).Order(ent.Asc(enttask.FieldCreatedAt))
	if limit > 0 {
		query.Limit(limit)
	}
	rows, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list queued mind map tasks: %w", err)
	}
	result := make([]mindmap.Task, 0, len(rows))
	for _, row := range rows {
		result = append(result, toDomainMindMapTask(row))
	}
	return result, nil
}

func (r *MindMapRepository) CountRunningTasks(ctx context.Context) (int, error) {
	count, err := r.client.MindMapTask.Query().Where(enttask.StatusEQ(string(mindmap.TaskRunning))).Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count running mind map tasks: %w", err)
	}
	return count, nil
}

func (r *MindMapRepository) findScope(ctx context.Context, projectID mindmap.ProjectID, sessionID mindmap.SessionID) ([]mindmap.Node, []mindmap.Edge, error) {
	nodeRows, err := r.client.MindMapNode.Query().Where(
		entnode.ProjectIDEQ(string(projectID)), entnode.SessionIDEQ(string(sessionID)),
	).Order(ent.Asc(entnode.FieldNodeID)).All(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("find mind map nodes: %w", err)
	}
	edgeRows, err := r.client.MindMapEdge.Query().Where(
		entedge.ProjectIDEQ(string(projectID)), entedge.SessionIDEQ(string(sessionID)),
	).Order(ent.Asc(entedge.FieldEdgeID)).All(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("find mind map edges: %w", err)
	}
	nodes := make([]mindmap.Node, 0, len(nodeRows))
	for _, row := range nodeRows {
		nodes = append(nodes, toDomainMindMapNode(row))
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].ID == mindmap.RootNodeID {
			return true
		}
		if nodes[j].ID == mindmap.RootNodeID {
			return false
		}
		return nodes[i].ID < nodes[j].ID
	})
	edges := make([]mindmap.Edge, 0, len(edgeRows))
	for _, row := range edgeRows {
		edges = append(edges, toDomainMindMapEdge(row))
	}
	return nodes, edges, nil
}

func changedEntityIDs(graph mindmap.Graph, changes []mindmap.Change) ([]mindmap.NodeID, []mindmap.EdgeID) {
	if len(changes) == 0 {
		nodes := make([]mindmap.NodeID, 0, len(graph.Nodes))
		for _, node := range graph.Nodes {
			nodes = append(nodes, node.ID)
		}
		edges := make([]mindmap.EdgeID, 0, len(graph.Edges))
		for _, edge := range graph.Edges {
			edges = append(edges, edge.ID)
		}
		return nodes, edges
	}
	nodeIDs := make(map[mindmap.NodeID]struct{})
	edgeIDs := make(map[mindmap.EdgeID]struct{})
	for _, change := range changes {
		switch change.Kind {
		case mindmap.ChangeUpsertNode, mindmap.ChangeDeleteNode:
			nodeID := mindmap.NodeID(change.EntityID)
			nodeIDs[nodeID] = struct{}{}
			if change.Kind == mindmap.ChangeDeleteNode {
				for _, edge := range graph.Edges {
					if edge.SourceID == nodeID || edge.TargetID == nodeID {
						edgeIDs[edge.ID] = struct{}{}
					}
				}
			}
		case mindmap.ChangeUpsertEdge, mindmap.ChangeDeleteEdge:
			edgeIDs[mindmap.EdgeID(change.EntityID)] = struct{}{}
		}
	}
	nodes := make([]mindmap.NodeID, 0, len(nodeIDs))
	for id := range nodeIDs {
		nodes = append(nodes, id)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i] < nodes[j] })
	edges := make([]mindmap.EdgeID, 0, len(edgeIDs))
	for id := range edgeIDs {
		edges = append(edges, id)
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i] < edges[j] })
	return nodes, edges
}

func (r *MindMapRepository) replaceNodes(ctx context.Context, projectID mindmap.ProjectID, sessionID mindmap.SessionID, nodes []mindmap.Node, ids []mindmap.NodeID, partial bool) error {
	wanted := make(map[mindmap.NodeID]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	query := r.client.MindMapNode.Delete().Where(
		entnode.ProjectIDEQ(string(projectID)), entnode.SessionIDEQ(string(sessionID)),
	)
	if ids != nil {
		values := make([]string, 0, len(ids))
		for _, id := range ids {
			values = append(values, string(id))
		}
		if len(values) == 0 {
			return nil
		}
		query.Where(entnode.NodeIDIn(values...))
	}
	if _, err := query.Exec(ctx); err != nil {
		return fmt.Errorf("replace mind map nodes: %w", err)
	}
	builders := make([]*ent.MindMapNodeCreate, 0, len(nodes))
	for _, node := range nodes {
		if ids != nil {
			if _, ok := wanted[node.ID]; !ok {
				continue
			}
		}
		builder := r.client.MindMapNode.Create().
			SetProjectID(string(projectID)).SetSessionID(string(sessionID)).SetNodeID(string(node.ID))
		if !partial || !node.TitleUpdatedAt.IsZero() {
			builder.SetTitle(node.Title)
		}
		if !partial || !node.ContentUpdatedAt.IsZero() {
			builder.SetContent(node.Content)
		}
		if !partial || !node.FilesUpdatedAt.IsZero() {
			builder.SetFiles(toStorageMindMapNodeFiles(node.Files))
		}
		if !node.TitleUpdatedAt.IsZero() {
			builder.SetTitleUpdatedAt(node.TitleUpdatedAt)
		}
		if !node.ContentUpdatedAt.IsZero() {
			builder.SetContentUpdatedAt(node.ContentUpdatedAt)
		}
		if !node.FilesUpdatedAt.IsZero() {
			builder.SetFilesUpdatedAt(node.FilesUpdatedAt)
		}
		if node.DeletedAt != nil {
			builder.SetDeletedAt(*node.DeletedAt)
		}
		builders = append(builders, builder)
	}
	if len(builders) > 0 {
		if _, err := r.client.MindMapNode.CreateBulk(builders...).Save(ctx); err != nil {
			return fmt.Errorf("create mind map nodes: %w", err)
		}
	}
	return nil
}

func (r *MindMapRepository) replaceEdges(ctx context.Context, projectID mindmap.ProjectID, sessionID mindmap.SessionID, edges []mindmap.Edge, ids []mindmap.EdgeID, partial bool) error {
	wanted := make(map[mindmap.EdgeID]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	query := r.client.MindMapEdge.Delete().Where(
		entedge.ProjectIDEQ(string(projectID)), entedge.SessionIDEQ(string(sessionID)),
	)
	if ids != nil {
		values := make([]string, 0, len(ids))
		for _, id := range ids {
			values = append(values, string(id))
		}
		if len(values) == 0 {
			return nil
		}
		query.Where(entedge.EdgeIDIn(values...))
	}
	if _, err := query.Exec(ctx); err != nil {
		return fmt.Errorf("replace mind map edges: %w", err)
	}
	builders := make([]*ent.MindMapEdgeCreate, 0, len(edges))
	for _, edge := range edges {
		if ids != nil {
			if _, ok := wanted[edge.ID]; !ok {
				continue
			}
		}
		builder := r.client.MindMapEdge.Create().
			SetProjectID(string(projectID)).SetSessionID(string(sessionID)).SetEdgeID(string(edge.ID))
		if !partial || !edge.SourceUpdatedAt.IsZero() {
			builder.SetSourceID(string(edge.SourceID))
		}
		if !partial || !edge.TargetUpdatedAt.IsZero() {
			builder.SetTargetID(string(edge.TargetID))
		}
		if !partial || !edge.LabelUpdatedAt.IsZero() {
			builder.SetLabel(edge.Label)
		}
		if !edge.SourceUpdatedAt.IsZero() {
			builder.SetSourceUpdatedAt(edge.SourceUpdatedAt)
		}
		if !edge.TargetUpdatedAt.IsZero() {
			builder.SetTargetUpdatedAt(edge.TargetUpdatedAt)
		}
		if !edge.LabelUpdatedAt.IsZero() {
			builder.SetLabelUpdatedAt(edge.LabelUpdatedAt)
		}
		if edge.DeletedAt != nil {
			builder.SetDeletedAt(*edge.DeletedAt)
		}
		builders = append(builders, builder)
	}
	if len(builders) > 0 {
		if _, err := r.client.MindMapEdge.CreateBulk(builders...).Save(ctx); err != nil {
			return fmt.Errorf("create mind map edges: %w", err)
		}
	}
	return nil
}

func scopeEntities(changes []mindmap.Change) ([]mindmap.Node, []mindmap.Edge) {
	ordered := append([]mindmap.Change(nil), changes...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].OccurredAt.Before(ordered[j].OccurredAt) })
	nodes := make(map[mindmap.NodeID]mindmap.Node)
	edges := make(map[mindmap.EdgeID]mindmap.Edge)
	for _, change := range ordered {
		switch change.Kind {
		case mindmap.ChangeUpsertNode:
			id := mindmap.NodeID(change.EntityID)
			node := nodes[id]
			node.ID = id
			if node.DeletedAt != nil && !change.OccurredAt.Before(*node.DeletedAt) {
				node.DeletedAt = nil
			}
			if change.Title != nil && !change.OccurredAt.Before(node.TitleUpdatedAt) {
				node.Title, node.TitleUpdatedAt = *change.Title, change.OccurredAt
			}
			if change.Content != nil && !change.OccurredAt.Before(node.ContentUpdatedAt) {
				node.Content, node.ContentUpdatedAt = *change.Content, change.OccurredAt
			}
			if change.Files != nil && !change.OccurredAt.Before(node.FilesUpdatedAt) {
				node.Files = append([]mindmap.NodeFile(nil), (*change.Files)...)
				node.FilesUpdatedAt = change.OccurredAt
			}
			nodes[id] = node
		case mindmap.ChangeDeleteNode:
			id := mindmap.NodeID(change.EntityID)
			node := nodes[id]
			node.ID = id
			deletedAt := change.OccurredAt
			node.DeletedAt = &deletedAt
			nodes[id] = node
		case mindmap.ChangeUpsertEdge:
			id := mindmap.EdgeID(change.EntityID)
			edge := edges[id]
			edge.ID = id
			if edge.DeletedAt != nil && !change.OccurredAt.Before(*edge.DeletedAt) {
				edge.DeletedAt = nil
			}
			if change.SourceID != nil && !change.OccurredAt.Before(edge.SourceUpdatedAt) {
				edge.SourceID, edge.SourceUpdatedAt = *change.SourceID, change.OccurredAt
			}
			if change.TargetID != nil && !change.OccurredAt.Before(edge.TargetUpdatedAt) {
				edge.TargetID, edge.TargetUpdatedAt = *change.TargetID, change.OccurredAt
			}
			if change.Label != nil && !change.OccurredAt.Before(edge.LabelUpdatedAt) {
				edge.Label, edge.LabelUpdatedAt = *change.Label, change.OccurredAt
			}
			edges[id] = edge
		case mindmap.ChangeDeleteEdge:
			id := mindmap.EdgeID(change.EntityID)
			edge := edges[id]
			edge.ID = id
			deletedAt := change.OccurredAt
			edge.DeletedAt = &deletedAt
			edges[id] = edge
		}
	}
	nodeList := make([]mindmap.Node, 0, len(nodes))
	for _, node := range nodes {
		nodeList = append(nodeList, node)
	}
	sort.Slice(nodeList, func(i, j int) bool { return nodeList[i].ID < nodeList[j].ID })
	edgeList := make([]mindmap.Edge, 0, len(edges))
	for _, edge := range edges {
		edgeList = append(edgeList, edge)
	}
	sort.Slice(edgeList, func(i, j int) bool { return edgeList[i].ID < edgeList[j].ID })
	return nodeList, edgeList
}

func scopeChanges(projectID mindmap.ProjectID, sessionID mindmap.SessionID, nodes []mindmap.Node, edges []mindmap.Edge) []mindmap.Change {
	changes := make([]mindmap.Change, 0, len(nodes)*3+len(edges)*4)
	for _, node := range nodes {
		if !node.TitleUpdatedAt.IsZero() && node.TitleUpdatedAt.Equal(node.ContentUpdatedAt) {
			title, content := node.Title, node.Content
			changes = append(changes, mindmap.Change{ID: mindmap.ChangeID(string(node.ID) + ":node"), ProjectID: projectID, SessionID: sessionID, Kind: mindmap.ChangeUpsertNode, EntityID: string(node.ID), Title: &title, Content: &content, OccurredAt: node.TitleUpdatedAt})
		} else {
			if !node.TitleUpdatedAt.IsZero() {
				title := node.Title
				changes = append(changes, mindmap.Change{ID: mindmap.ChangeID(string(node.ID) + ":title"), ProjectID: projectID, SessionID: sessionID, Kind: mindmap.ChangeUpsertNode, EntityID: string(node.ID), Title: &title, OccurredAt: node.TitleUpdatedAt})
			}
			if !node.ContentUpdatedAt.IsZero() {
				content := node.Content
				changes = append(changes, mindmap.Change{ID: mindmap.ChangeID(string(node.ID) + ":content"), ProjectID: projectID, SessionID: sessionID, Kind: mindmap.ChangeUpsertNode, EntityID: string(node.ID), Content: &content, OccurredAt: node.ContentUpdatedAt})
			}
		}
		if node.DeletedAt != nil {
			changes = append(changes, mindmap.Change{ID: mindmap.ChangeID(string(node.ID) + ":deleted"), ProjectID: projectID, SessionID: sessionID, Kind: mindmap.ChangeDeleteNode, EntityID: string(node.ID), OccurredAt: *node.DeletedAt})
		}
		if !node.FilesUpdatedAt.IsZero() {
			files := append([]mindmap.NodeFile(nil), node.Files...)
			changes = append(changes, mindmap.Change{ID: mindmap.ChangeID(string(node.ID) + ":files"), ProjectID: projectID, SessionID: sessionID, Kind: mindmap.ChangeUpsertNode, EntityID: string(node.ID), Files: &files, OccurredAt: node.FilesUpdatedAt})
		}
	}
	for _, edge := range edges {
		if !edge.SourceUpdatedAt.IsZero() && edge.SourceUpdatedAt.Equal(edge.TargetUpdatedAt) {
			sourceID, targetID := edge.SourceID, edge.TargetID
			changes = append(changes, mindmap.Change{ID: mindmap.ChangeID(string(edge.ID) + ":endpoints"), ProjectID: projectID, SessionID: sessionID, Kind: mindmap.ChangeUpsertEdge, EntityID: string(edge.ID), SourceID: &sourceID, TargetID: &targetID, OccurredAt: edge.SourceUpdatedAt})
		} else {
			if !edge.SourceUpdatedAt.IsZero() {
				sourceID := edge.SourceID
				changes = append(changes, mindmap.Change{ID: mindmap.ChangeID(string(edge.ID) + ":source"), ProjectID: projectID, SessionID: sessionID, Kind: mindmap.ChangeUpsertEdge, EntityID: string(edge.ID), SourceID: &sourceID, OccurredAt: edge.SourceUpdatedAt})
			}
			if !edge.TargetUpdatedAt.IsZero() {
				targetID := edge.TargetID
				changes = append(changes, mindmap.Change{ID: mindmap.ChangeID(string(edge.ID) + ":target"), ProjectID: projectID, SessionID: sessionID, Kind: mindmap.ChangeUpsertEdge, EntityID: string(edge.ID), TargetID: &targetID, OccurredAt: edge.TargetUpdatedAt})
			}
		}
		if !edge.LabelUpdatedAt.IsZero() {
			label := edge.Label
			changes = append(changes, mindmap.Change{ID: mindmap.ChangeID(string(edge.ID) + ":label"), ProjectID: projectID, SessionID: sessionID, Kind: mindmap.ChangeUpsertEdge, EntityID: string(edge.ID), Label: &label, OccurredAt: edge.LabelUpdatedAt})
		}
		if edge.DeletedAt != nil {
			changes = append(changes, mindmap.Change{ID: mindmap.ChangeID(string(edge.ID) + ":deleted"), ProjectID: projectID, SessionID: sessionID, Kind: mindmap.ChangeDeleteEdge, EntityID: string(edge.ID), OccurredAt: *edge.DeletedAt})
		}
	}
	sort.SliceStable(changes, func(i, j int) bool {
		if !changes[i].OccurredAt.Equal(changes[j].OccurredAt) {
			return changes[i].OccurredAt.Before(changes[j].OccurredAt)
		}
		if changes[i].EntityID != changes[j].EntityID {
			return changes[i].EntityID < changes[j].EntityID
		}
		if changes[i].Kind != changes[j].Kind {
			return persistedChangeOrder(changes[i].Kind) < persistedChangeOrder(changes[j].Kind)
		}
		return changes[i].ID < changes[j].ID
	})
	return changes
}

func persistedChangeOrder(kind mindmap.ChangeKind) int {
	switch kind {
	case mindmap.ChangeUpsertNode, mindmap.ChangeUpsertEdge:
		return 0
	default:
		return 1
	}
}

func toDomainMindMapNode(row *ent.MindMapNode) mindmap.Node {
	node := mindmap.Node{ID: mindmap.NodeID(row.NodeID), Files: toDomainMindMapNodeFiles(row.Files), DeletedAt: row.DeletedAt}
	if row.Title != nil {
		node.Title = *row.Title
	}
	if row.Content != nil {
		node.Content = *row.Content
	}
	if row.TitleUpdatedAt != nil {
		node.TitleUpdatedAt = *row.TitleUpdatedAt
	}
	if row.ContentUpdatedAt != nil {
		node.ContentUpdatedAt = *row.ContentUpdatedAt
	}
	if row.FilesUpdatedAt != nil {
		node.FilesUpdatedAt = *row.FilesUpdatedAt
	}
	return node
}

func toStorageMindMapNodeFiles(files []mindmap.NodeFile) []entschema.MindMapNodeFile {
	result := make([]entschema.MindMapNodeFile, len(files))
	for index, item := range files {
		result[index] = entschema.MindMapNodeFile{
			File: item.File, Method: item.Method, StartLine: item.StartLine, EndLine: item.EndLine,
		}
	}
	return result
}

func toDomainMindMapNodeFiles(files []entschema.MindMapNodeFile) []mindmap.NodeFile {
	result := make([]mindmap.NodeFile, len(files))
	for index, item := range files {
		result[index] = mindmap.NodeFile{
			File: item.File, Method: item.Method, StartLine: item.StartLine, EndLine: item.EndLine,
		}
	}
	return result
}

func toDomainMindMapEdge(row *ent.MindMapEdge) mindmap.Edge {
	edge := mindmap.Edge{ID: mindmap.EdgeID(row.EdgeID), DeletedAt: row.DeletedAt}
	if row.SourceID != nil {
		edge.SourceID = mindmap.NodeID(*row.SourceID)
	}
	if row.TargetID != nil {
		edge.TargetID = mindmap.NodeID(*row.TargetID)
	}
	if row.Label != nil {
		edge.Label = *row.Label
	}
	if row.SourceUpdatedAt != nil {
		edge.SourceUpdatedAt = *row.SourceUpdatedAt
	}
	if row.TargetUpdatedAt != nil {
		edge.TargetUpdatedAt = *row.TargetUpdatedAt
	}
	if row.LabelUpdatedAt != nil {
		edge.LabelUpdatedAt = *row.LabelUpdatedAt
	}
	return edge
}

func toDomainMindMapTask(row *ent.MindMapTask) mindmap.Task {
	return mindmap.Task{
		ID: mindmap.TaskID(row.ID), ProjectID: mindmap.ProjectID(row.ProjectID), SessionID: mindmap.SessionID(row.SessionID),
		Status: mindmap.TaskStatus(row.Status), ProcessRunID: row.ProcessRunID, Attempts: row.Attempts, Error: row.Error,
		CreatedAt: row.CreatedAt, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, UpdatedAt: row.UpdatedAt,
	}
}

func setMindMapTaskTimesOnUpdate(update *ent.MindMapTaskUpdateOne, task mindmap.Task) {
	if task.StartedAt != nil {
		update.SetStartedAt(*task.StartedAt)
	} else {
		update.ClearStartedAt()
	}
	if task.FinishedAt != nil {
		update.SetFinishedAt(*task.FinishedAt)
	} else {
		update.ClearFinishedAt()
	}
	if !task.UpdatedAt.IsZero() {
		update.SetUpdatedAt(task.UpdatedAt)
	}
}

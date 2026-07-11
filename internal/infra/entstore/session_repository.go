package entstore

import (
	"context"
	"fmt"
	"strings"

	domainsession "github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	entmergerecord "github.com/nzlov/anycode/internal/infra/entstore/ent/mergerecord"
	entpromptappend "github.com/nzlov/anycode/internal/infra/entstore/ent/promptappend"
	entsession "github.com/nzlov/anycode/internal/infra/entstore/ent/session"
)

const (
	defaultSessionPage     = 1
	defaultSessionPageSize = 20
	maxSessionPageSize     = 100
)

type SessionRepository struct {
	client *ent.Client
}

func NewSessionRepository(client *ent.Client) *SessionRepository {
	return &SessionRepository{client: client}
}

func (r *SessionRepository) Create(ctx context.Context, s domainsession.Session) error {
	if err := r.create(ctx, s); err != nil {
		if ent.IsConstraintError(err) {
			return fmt.Errorf("%w: %w", domainsession.ErrSessionAlreadyExists, err)
		}
		return err
	}
	return nil
}

func (r *SessionRepository) Save(ctx context.Context, s domainsession.Session) error {
	exists, err := r.client.Session.Query().Where(entsession.IDEQ(string(s.ID))).Exist(ctx)
	if err != nil {
		return fmt.Errorf("check session exists: %w", err)
	}
	if exists {
		update := r.client.Session.UpdateOneID(string(s.ID)).
			SetProjectID(string(s.ProjectID)).
			SetRequirement(s.Requirement).
			SetMode(string(s.Mode)).
			SetStatus(string(s.Status)).
			SetPriority(string(normalizePriority(s.Priority))).
			SetBaseBranch(s.BaseBranch).
			SetWorktreePath(s.WorktreePath).
			SetWorktreeBaseCommit(s.WorktreeBaseCommit).
			SetCodexSessionID(s.CodexSessionID).
			SetCodexModel(s.Config.CodexModel).
			SetReasoningEffort(s.Config.ReasoningEffort).
			SetPermissionMode(s.Config.PermissionMode).
			SetTodoList(s.TodoList).
			SetQueueKind(string(s.Queue.Kind)).
			SetQueuePriority(string(normalizeQueuePriority(s.Queue.Priority))).
			SetQueueWorkflowRunID(string(s.Queue.WorkflowRunID)).
			SetQueuePrompt(s.Queue.Prompt).
			SetQueueResumeCodexSessionID(s.Queue.ResumeCodexSessionID)
		if s.Queue.NodeRunID == nil {
			update.SetQueueNodeRunID("")
		} else {
			update.SetQueueNodeRunID(string(*s.Queue.NodeRunID))
		}
		if s.QueuedAt == nil {
			update.ClearQueuedAt()
		} else {
			update.SetQueuedAt(*s.QueuedAt)
		}
		if s.CloseReason == nil {
			update.ClearCloseReason()
		} else {
			update.SetCloseReason(string(*s.CloseReason))
		}
		if s.LastRunAt == nil {
			update.ClearLastRunAt()
		} else {
			update.SetLastRunAt(*s.LastRunAt)
		}
		if s.ClosedAt == nil {
			update.ClearClosedAt()
		} else {
			update.SetClosedAt(*s.ClosedAt)
		}
		if !s.UpdatedAt.IsZero() {
			update.SetUpdatedAt(s.UpdatedAt)
		}
		if err := update.Exec(ctx); err != nil {
			return fmt.Errorf("update session: %w", err)
		}
		return nil
	}

	return r.create(ctx, s)
}

func (r *SessionRepository) create(ctx context.Context, s domainsession.Session) error {
	create := r.client.Session.Create().
		SetID(string(s.ID)).
		SetProjectID(string(s.ProjectID)).
		SetRequirement(s.Requirement).
		SetMode(string(s.Mode)).
		SetStatus(string(s.Status)).
		SetPriority(string(normalizePriority(s.Priority))).
		SetBaseBranch(s.BaseBranch).
		SetWorktreePath(s.WorktreePath).
		SetWorktreeBaseCommit(s.WorktreeBaseCommit).
		SetCodexSessionID(s.CodexSessionID).
		SetCodexModel(s.Config.CodexModel).
		SetReasoningEffort(s.Config.ReasoningEffort).
		SetPermissionMode(s.Config.PermissionMode).
		SetTodoList(s.TodoList).
		SetQueueKind(string(s.Queue.Kind)).
		SetQueuePriority(string(normalizeQueuePriority(s.Queue.Priority))).
		SetQueueWorkflowRunID(string(s.Queue.WorkflowRunID)).
		SetQueuePrompt(s.Queue.Prompt).
		SetQueueResumeCodexSessionID(s.Queue.ResumeCodexSessionID)
	if s.Queue.NodeRunID != nil {
		create.SetQueueNodeRunID(string(*s.Queue.NodeRunID))
	}
	if s.QueuedAt != nil {
		create.SetQueuedAt(*s.QueuedAt)
	}
	if s.CloseReason != nil {
		create.SetCloseReason(string(*s.CloseReason))
	}
	if s.LastRunAt != nil {
		create.SetLastRunAt(*s.LastRunAt)
	}
	if !s.CreatedAt.IsZero() {
		create.SetCreatedAt(s.CreatedAt)
	}
	if !s.UpdatedAt.IsZero() {
		create.SetUpdatedAt(s.UpdatedAt)
	}
	if s.ClosedAt != nil {
		create.SetClosedAt(*s.ClosedAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (r *SessionRepository) Find(ctx context.Context, id domainsession.ID) (domainsession.Session, error) {
	row, err := r.client.Session.Get(ctx, string(id))
	if err != nil {
		return domainsession.Session{}, fmt.Errorf("find session: %w", err)
	}
	return toDomainSession(row), nil
}

func (r *SessionRepository) ListCards(ctx context.Context, query domainsession.ListQuery) ([]domainsession.Session, int, error) {
	page, pageSize := normalizeSessionPage(query.Page, query.PageSize)
	base := applySessionListFilters(r.client.Session.Query(), query)
	total, err := base.Clone().Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count session cards: %w", err)
	}
	rows, err := base.
		Order(sessionOrder(query.Sort)...).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list session cards: %w", err)
	}
	sessions := make([]domainsession.Session, 0, len(rows))
	for _, row := range rows {
		sessions = append(sessions, toDomainSession(row))
	}
	return sessions, total, nil
}

func (r *SessionRepository) ListQueued(ctx context.Context) ([]domainsession.Session, error) {
	rows, err := r.client.Session.Query().
		Where(entsession.StatusEQ(string(domainsession.StatusQueued))).
		Order(ent.Asc(entsession.FieldQueuedAt), ent.Asc(entsession.FieldUpdatedAt), ent.Asc(entsession.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list queued sessions: %w", err)
	}
	sessions := make([]domainsession.Session, 0, len(rows))
	for _, row := range rows {
		sessions = append(sessions, toDomainSession(row))
	}
	return sessions, nil
}

func (r *SessionRepository) LastConfigForProject(ctx context.Context, projectID domainsession.ProjectID) (domainsession.Config, bool, error) {
	row, err := r.client.Session.Query().
		Where(entsession.ProjectIDEQ(string(projectID))).
		Order(ent.Desc(entsession.FieldCreatedAt)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return domainsession.Config{}, false, nil
		}
		return domainsession.Config{}, false, fmt.Errorf("last session config: %w", err)
	}
	return domainsession.Config{
		CodexModel:      row.CodexModel,
		ReasoningEffort: row.ReasoningEffort,
		PermissionMode:  row.PermissionMode,
	}, true, nil
}

func (r *SessionRepository) ListInterruptedWithCodexSession(ctx context.Context) ([]domainsession.Session, error) {
	rows, err := r.client.Session.Query().
		Where(
			entsession.StatusIn(
				string(domainsession.StatusStarting),
				string(domainsession.StatusRunning),
				string(domainsession.StatusWaitingUser),
				string(domainsession.StatusStopping),
				string(domainsession.StatusQueued),
			),
			entsession.CodexSessionIDNEQ(""),
		).
		Order(ent.Asc(entsession.FieldUpdatedAt), ent.Asc(entsession.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list interrupted sessions: %w", err)
	}
	sessions := make([]domainsession.Session, 0, len(rows))
	for _, row := range rows {
		session := toDomainSession(row)
		if session.Status == domainsession.StatusQueued && session.Queue.Kind != domainsession.QueueKindAnswerUser {
			continue
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

func (r *SessionRepository) CountByProject(ctx context.Context, projectID domainsession.ProjectID) (int, error) {
	count, err := r.client.Session.Query().
		Where(entsession.ProjectIDEQ(string(projectID))).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count project sessions: %w", err)
	}
	return count, nil
}

func (r *SessionRepository) AppendPrompt(ctx context.Context, append domainsession.PromptAppend) error {
	create := r.client.PromptAppend.Create().
		SetID(append.ID).
		SetSessionID(string(append.SessionID)).
		SetBody(append.Body)
	if !append.CreatedAt.IsZero() {
		create.SetCreatedAt(append.CreatedAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("append prompt: %w", err)
	}
	return nil
}

func (r *SessionRepository) DeletePromptAppend(ctx context.Context, id string) error {
	if err := r.client.PromptAppend.DeleteOneID(id).Exec(ctx); err != nil && !ent.IsNotFound(err) {
		return fmt.Errorf("delete prompt append: %w", err)
	}
	return nil
}

func (r *SessionRepository) ListPromptAppends(ctx context.Context, sessionID domainsession.ID) ([]domainsession.PromptAppend, error) {
	rows, err := r.client.PromptAppend.Query().
		Where(entpromptappend.SessionIDEQ(string(sessionID))).
		Order(ent.Asc(entpromptappend.FieldCreatedAt), ent.Asc(entpromptappend.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list prompt appends: %w", err)
	}
	appends := make([]domainsession.PromptAppend, 0, len(rows))
	for _, row := range rows {
		appends = append(appends, domainsession.PromptAppend{
			ID:        row.ID,
			SessionID: domainsession.ID(row.SessionID),
			Body:      row.Body,
			CreatedAt: row.CreatedAt,
		})
	}
	return appends, nil
}

func (r *SessionRepository) AddMergeRecord(ctx context.Context, record domainsession.MergeRecord) error {
	create := r.client.MergeRecord.Create().
		SetID(record.ID).
		SetSessionID(string(record.SessionID)).
		SetStrategy(record.Strategy).
		SetBaseBranch(record.BaseBranch).
		SetWorktreeBranch(record.WorktreeBranch).
		SetBaseCommit(record.BaseCommit).
		SetHeadCommit(record.HeadCommit).
		SetMergeCommit(record.MergeCommit).
		SetStatus(record.Status).
		SetFailureCode(record.FailureCode).
		SetFailureReason(record.FailureReason)
	if record.NodeRunID != nil {
		create.SetNodeRunID(string(*record.NodeRunID))
	}
	if record.MergedAt != nil {
		create.SetMergedAt(*record.MergedAt)
	}
	if !record.CreatedAt.IsZero() {
		create.SetCreatedAt(record.CreatedAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("add merge record: %w", err)
	}
	return nil
}

func (r *SessionRepository) LatestSuccessfulMergeRecord(ctx context.Context, sessionID domainsession.ID) (domainsession.MergeRecord, bool, error) {
	row, err := r.client.MergeRecord.Query().
		Where(
			entmergerecord.SessionIDEQ(string(sessionID)),
			entmergerecord.StatusEQ("merged"),
		).
		Order(ent.Desc(entmergerecord.FieldCreatedAt), ent.Desc(entmergerecord.FieldID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return domainsession.MergeRecord{}, false, nil
		}
		return domainsession.MergeRecord{}, false, fmt.Errorf("latest successful merge record: %w", err)
	}
	return toDomainMergeRecord(row), true, nil
}

func applySessionListFilters(q *ent.SessionQuery, query domainsession.ListQuery) *ent.SessionQuery {
	if query.ProjectID != nil {
		q.Where(entsession.ProjectIDEQ(string(*query.ProjectID)))
	}
	switch strings.ToLower(strings.TrimSpace(query.Scope)) {
	case string(domainsession.StatusCreated),
		string(domainsession.StatusQueued),
		string(domainsession.StatusStarting),
		string(domainsession.StatusRunning),
		string(domainsession.StatusWaitingUser),
		string(domainsession.StatusStopping),
		string(domainsession.StatusStopped),
		string(domainsession.StatusResumeFailed),
		string(domainsession.StatusFailed),
		string(domainsession.StatusCompleted),
		"blocked",
		string(domainsession.StatusClosed):
		q.Where(entsession.StatusEQ(strings.ToLower(strings.TrimSpace(query.Scope))))
	}
	switch strings.ToLower(strings.TrimSpace(query.Range)) {
	case "latest":
		q.Where(entsession.StatusNEQ(string(domainsession.StatusClosed)))
	case "history":
		q.Where(entsession.StatusEQ(string(domainsession.StatusClosed)))
	}
	filter := strings.TrimSpace(query.Filter)
	if filter != "" {
		q.Where(entsession.Or(
			entsession.RequirementContainsFold(filter),
			entsession.BaseBranchContainsFold(filter),
			entsession.CodexSessionIDContainsFold(filter),
			entsession.StatusContainsFold(filter),
		))
	}
	return q
}

func normalizeSessionPage(page, pageSize int) (int, int) {
	if page < 1 {
		page = defaultSessionPage
	}
	if pageSize < 1 {
		pageSize = defaultSessionPageSize
	}
	if pageSize > maxSessionPageSize {
		pageSize = maxSessionPageSize
	}
	return page, pageSize
}

func sessionOrder(sort string) []entsession.OrderOption {
	field, desc := parseSessionSort(sort)
	dir := ent.Asc
	if desc {
		dir = ent.Desc
	}
	return []entsession.OrderOption{
		dir(field),
		ent.Desc(entsession.FieldID),
	}
}

func parseSessionSort(sort string) (string, bool) {
	sort = strings.ToLower(strings.TrimSpace(sort))
	desc := true
	if strings.HasPrefix(sort, "-") {
		sort = strings.TrimPrefix(sort, "-")
		desc = true
	} else if strings.HasPrefix(sort, "+") {
		sort = strings.TrimPrefix(sort, "+")
		desc = false
	}
	parts := strings.Fields(sort)
	if len(parts) > 0 {
		sort = parts[0]
	}
	if len(parts) > 1 {
		switch parts[1] {
		case "asc":
			desc = false
		case "desc":
			desc = true
		}
	}
	switch sort {
	case entsession.FieldCreatedAt, "created":
		return entsession.FieldCreatedAt, desc
	case entsession.FieldUpdatedAt, "updated", "":
		return entsession.FieldUpdatedAt, desc
	case entsession.FieldLastRunAt, "last_run", "last":
		return entsession.FieldLastRunAt, desc
	case entsession.FieldRequirement, "title":
		return entsession.FieldRequirement, desc
	case entsession.FieldProjectID, "project":
		return entsession.FieldProjectID, desc
	case entsession.FieldStatus:
		return entsession.FieldStatus, desc
	case entsession.FieldPriority:
		return entsession.FieldPriority, desc
	case entsession.FieldBaseBranch, "base":
		return entsession.FieldBaseBranch, desc
	default:
		return entsession.FieldUpdatedAt, desc
	}
}

func toDomainSession(row *ent.Session) domainsession.Session {
	var closeReason *domainsession.CloseReason
	if row.CloseReason != nil {
		v := domainsession.CloseReason(*row.CloseReason)
		closeReason = &v
	}
	var queueNodeRunID *domainsession.NodeRunID
	if strings.TrimSpace(row.QueueNodeRunID) != "" {
		v := domainsession.NodeRunID(row.QueueNodeRunID)
		queueNodeRunID = &v
	}
	return domainsession.Session{
		ID:                 domainsession.ID(row.ID),
		ProjectID:          domainsession.ProjectID(row.ProjectID),
		Requirement:        row.Requirement,
		Mode:               domainsession.Mode(row.Mode),
		Status:             domainsession.Status(row.Status),
		Priority:           normalizePriority(domainsession.Priority(row.Priority)),
		CloseReason:        closeReason,
		BaseBranch:         row.BaseBranch,
		WorktreePath:       row.WorktreePath,
		WorktreeBaseCommit: row.WorktreeBaseCommit,
		CodexSessionID:     row.CodexSessionID,
		Config: domainsession.Config{
			CodexModel:      row.CodexModel,
			ReasoningEffort: row.ReasoningEffort,
			PermissionMode:  row.PermissionMode,
		},
		TodoList: row.TodoList,
		QueuedAt: row.QueuedAt,
		Queue: domainsession.QueueIntent{
			Kind:                 domainsession.QueueKind(row.QueueKind),
			Priority:             normalizeQueuePriority(domainsession.QueuePriority(row.QueuePriority)),
			WorkflowRunID:        domainsession.WorkflowRunID(row.QueueWorkflowRunID),
			NodeRunID:            queueNodeRunID,
			Prompt:               row.QueuePrompt,
			ResumeCodexSessionID: row.QueueResumeCodexSessionID,
		},
		LastRunAt: row.LastRunAt,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
		ClosedAt:  row.ClosedAt,
	}
}

func normalizePriority(priority domainsession.Priority) domainsession.Priority {
	switch priority {
	case domainsession.PriorityHigh, domainsession.PriorityLow:
		return priority
	default:
		return domainsession.PriorityMedium
	}
}

func normalizeQueuePriority(priority domainsession.QueuePriority) domainsession.QueuePriority {
	switch priority {
	case domainsession.QueuePriorityImmediate, domainsession.QueuePriorityHigh, domainsession.QueuePriorityLow:
		return priority
	default:
		return domainsession.QueuePriorityMedium
	}
}

func toDomainMergeRecord(row *ent.MergeRecord) domainsession.MergeRecord {
	var nodeRunID *domainsession.NodeRunID
	if row.NodeRunID != nil {
		v := domainsession.NodeRunID(*row.NodeRunID)
		nodeRunID = &v
	}
	return domainsession.MergeRecord{
		ID:             row.ID,
		SessionID:      domainsession.ID(row.SessionID),
		NodeRunID:      nodeRunID,
		Strategy:       row.Strategy,
		BaseBranch:     row.BaseBranch,
		WorktreeBranch: row.WorktreeBranch,
		BaseCommit:     row.BaseCommit,
		HeadCommit:     row.HeadCommit,
		MergeCommit:    row.MergeCommit,
		Status:         row.Status,
		FailureCode:    row.FailureCode,
		FailureReason:  row.FailureReason,
		MergedAt:       row.MergedAt,
		CreatedAt:      row.CreatedAt,
	}
}

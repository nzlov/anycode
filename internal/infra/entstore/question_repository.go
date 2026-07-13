package entstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nzlov/anycode/internal/domain/question"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	entquestionbatch "github.com/nzlov/anycode/internal/infra/entstore/ent/questionbatch"
)

var _ question.Repository = (*QuestionRepository)(nil)
var _ question.AgentRepository = (*QuestionRepository)(nil)

type QuestionRepository struct {
	client *ent.Client
	inTx   bool
}

func NewQuestionRepository(client *ent.Client) *QuestionRepository {
	return &QuestionRepository{client: client}
}

func newQuestionRepositoryInTx(client *ent.Client) *QuestionRepository {
	return &QuestionRepository{client: client, inTx: true}
}

func (r *QuestionRepository) CreateBatch(ctx context.Context, batch question.Batch) error {
	questions, err := questionsToJSON(batch.Questions)
	if err != nil {
		return fmt.Errorf("encode batch questions: %w", err)
	}
	create := r.client.QuestionBatch.Create().
		SetID(string(batch.ID)).
		SetSessionID(string(batch.SessionID)).
		SetStatus(string(batch.Status)).
		SetDeliveryStatus(string(normalizeDeliveryStatus(batch.DeliveryStatus))).
		SetQuestions(questions)
	if batch.WorkflowRunID != nil {
		create.SetWorkflowRunID(string(*batch.WorkflowRunID))
	}
	if batch.OriginProcessRunID != nil {
		create.SetOriginProcessRunID(string(*batch.OriginProcessRunID))
	}
	if batch.DeliveryProcessRunID != nil {
		create.SetDeliveryProcessRunID(string(*batch.DeliveryProcessRunID))
	}
	if !batch.CreatedAt.IsZero() {
		create.SetCreatedAt(batch.CreatedAt)
	}
	if batch.AnsweredAt != nil {
		create.SetAnsweredAt(*batch.AnsweredAt)
	}
	if batch.DeliveredAt != nil {
		create.SetDeliveredAt(*batch.DeliveredAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("create question batch: %w", err)
	}
	return nil
}

func (r *QuestionRepository) FindBatch(ctx context.Context, id question.BatchID) (question.Batch, error) {
	row, err := r.client.QuestionBatch.Get(ctx, string(id))
	if err != nil {
		return question.Batch{}, fmt.Errorf("find question batch: %w", err)
	}
	return toDomainQuestionBatch(row)
}

func (r *QuestionRepository) ListPendingBySession(ctx context.Context, sessionID question.SessionID) ([]question.Batch, error) {
	rows, err := r.client.QuestionBatch.Query().
		Where(
			entquestionbatch.SessionIDEQ(string(sessionID)),
			entquestionbatch.StatusEQ(string(question.BatchPending)),
		).
		Order(ent.Asc(entquestionbatch.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending question batches: %w", err)
	}
	batches := make([]question.Batch, 0, len(rows))
	for _, row := range rows {
		batch, err := toDomainQuestionBatch(row)
		if err != nil {
			return nil, err
		}
		batches = append(batches, batch)
	}
	return batches, nil
}

func (r *QuestionRepository) CancelPendingBatch(ctx context.Context, id question.BatchID, reason string) (question.Batch, bool, error) {
	row, err := r.client.QuestionBatch.Get(ctx, string(id))
	if err != nil {
		return question.Batch{}, false, fmt.Errorf("find question batch for cancel: %w", err)
	}
	batch, err := toDomainQuestionBatch(row)
	if err != nil {
		return question.Batch{}, false, err
	}
	if batch.Status != question.BatchPending {
		return batch, false, nil
	}
	updated, err := r.client.QuestionBatch.Update().
		Where(
			entquestionbatch.IDEQ(string(id)),
			entquestionbatch.StatusEQ(string(question.BatchPending)),
		).
		SetStatus(string(question.BatchCancelled)).
		SetCancelReason(reason).
		Save(ctx)
	if err != nil {
		return question.Batch{}, false, fmt.Errorf("cancel question batch: %w", err)
	}
	if updated == 0 {
		current, err := r.FindBatch(ctx, id)
		if err != nil {
			return question.Batch{}, false, err
		}
		return current, false, nil
	}
	batch.Status = question.BatchCancelled
	return batch, true, nil
}

func (r *QuestionRepository) FindPendingByOriginProcessRun(ctx context.Context, processRunID question.ProcessRunID) (question.Batch, bool, error) {
	row, err := r.client.QuestionBatch.Query().
		Where(
			entquestionbatch.OriginProcessRunIDEQ(string(processRunID)),
			entquestionbatch.StatusEQ(string(question.BatchPending)),
		).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return question.Batch{}, false, nil
		}
		return question.Batch{}, false, fmt.Errorf("find pending question batch by process run: %w", err)
	}
	batch, err := toDomainQuestionBatch(row)
	return batch, err == nil, err
}

func (r *QuestionRepository) FindAwaitingDeliveryBySession(ctx context.Context, sessionID question.SessionID) (question.Batch, bool, error) {
	row, err := r.client.QuestionBatch.Query().
		Where(
			entquestionbatch.SessionIDEQ(string(sessionID)),
			entquestionbatch.StatusEQ(string(question.BatchAnswered)),
			entquestionbatch.DeliveryStatusEQ(string(question.DeliveryAwaitingResume)),
		).
		Order(ent.Asc(entquestionbatch.FieldAnsweredAt), ent.Asc(entquestionbatch.FieldID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return question.Batch{}, false, nil
		}
		return question.Batch{}, false, fmt.Errorf("find awaiting question delivery: %w", err)
	}
	batch, err := toDomainQuestionBatch(row)
	return batch, err == nil, err
}

func (r *QuestionRepository) ListAgentBatchesForRecovery(ctx context.Context) ([]question.Batch, error) {
	rows, err := r.client.QuestionBatch.Query().
		Where(entquestionbatch.Or(
			entquestionbatch.StatusEQ(string(question.BatchPending)),
			entquestionbatch.And(
				entquestionbatch.StatusEQ(string(question.BatchAnswered)),
				entquestionbatch.DeliveryStatusIn(string(question.DeliveryAwaitingResume), string(question.DeliveryInflight)),
			),
		)).
		Order(ent.Asc(entquestionbatch.FieldCreatedAt), ent.Asc(entquestionbatch.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list question batches for recovery: %w", err)
	}
	batches := make([]question.Batch, 0, len(rows))
	for _, row := range rows {
		batch, err := toDomainQuestionBatch(row)
		if err != nil {
			return nil, err
		}
		batches = append(batches, batch)
	}
	return batches, nil
}

func (r *QuestionRepository) SetOriginProcessRun(ctx context.Context, id question.BatchID, processRunID question.ProcessRunID) error {
	updated, err := r.client.QuestionBatch.Update().
		Where(
			entquestionbatch.IDEQ(string(id)),
			entquestionbatch.OriginProcessRunIDEQ(""),
		).
		SetOriginProcessRunID(string(processRunID)).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("set question origin process run: %w", err)
	}
	if updated == 0 {
		row, err := r.client.QuestionBatch.Get(ctx, string(id))
		if err != nil {
			return fmt.Errorf("find question batch after origin update: %w", err)
		}
		if row.OriginProcessRunID != string(processRunID) {
			return fmt.Errorf("question batch %s already has a different origin process run", id)
		}
	}
	return nil
}

func (r *QuestionRepository) MarkDeliveryAwaitingResume(ctx context.Context, id question.BatchID) error {
	updated, err := r.client.QuestionBatch.Update().
		Where(
			entquestionbatch.IDEQ(string(id)),
			entquestionbatch.StatusEQ(string(question.BatchAnswered)),
			entquestionbatch.DeliveryStatusIn(string(question.DeliveryNone), string(question.DeliveryAwaitingResume)),
		).
		SetDeliveryStatus(string(question.DeliveryAwaitingResume)).
		SetDeliveryProcessRunID("").
		ClearDeliveredAt().
		Save(ctx)
	if err != nil {
		return fmt.Errorf("mark question delivery awaiting resume: %w", err)
	}
	if updated == 0 {
		return fmt.Errorf("question batch %s cannot await resume", id)
	}
	return nil
}

func (r *QuestionRepository) MarkDeliveryInflight(ctx context.Context, id question.BatchID, processRunID question.ProcessRunID) error {
	updated, err := r.client.QuestionBatch.Update().
		Where(
			entquestionbatch.IDEQ(string(id)),
			entquestionbatch.StatusEQ(string(question.BatchAnswered)),
			entquestionbatch.DeliveryStatusEQ(string(question.DeliveryAwaitingResume)),
		).
		SetDeliveryStatus(string(question.DeliveryInflight)).
		SetDeliveryProcessRunID(string(processRunID)).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("mark question delivery inflight: %w", err)
	}
	if updated == 0 {
		row, err := r.client.QuestionBatch.Get(ctx, string(id))
		if err != nil {
			return fmt.Errorf("find question batch after inflight update: %w", err)
		}
		if row.DeliveryStatus != string(question.DeliveryInflight) || row.DeliveryProcessRunID != string(processRunID) {
			return fmt.Errorf("question batch %s cannot start delivery", id)
		}
	}
	return nil
}

func (r *QuestionRepository) MarkDeliveryDeliveredByProcessRun(ctx context.Context, processRunID question.ProcessRunID, deliveredAt time.Time) ([]question.Batch, error) {
	rows, err := r.client.QuestionBatch.Query().
		Where(
			entquestionbatch.DeliveryProcessRunIDEQ(string(processRunID)),
			entquestionbatch.DeliveryStatusEQ(string(question.DeliveryInflight)),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list inflight question deliveries: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if _, err := r.client.QuestionBatch.Update().
		Where(
			entquestionbatch.DeliveryProcessRunIDEQ(string(processRunID)),
			entquestionbatch.DeliveryStatusEQ(string(question.DeliveryInflight)),
		).
		SetDeliveryStatus(string(question.DeliveryDelivered)).
		SetDeliveredAt(deliveredAt).
		Save(ctx); err != nil {
		return nil, fmt.Errorf("mark question delivery delivered: %w", err)
	}
	batches := make([]question.Batch, 0, len(rows))
	for _, row := range rows {
		batch, err := toDomainQuestionBatch(row)
		if err != nil {
			return nil, err
		}
		batch.DeliveryStatus = question.DeliveryDelivered
		batch.DeliveredAt = &deliveredAt
		batches = append(batches, batch)
	}
	return batches, nil
}

func (r *QuestionRepository) ResetDeliveryAwaitingResume(ctx context.Context, id question.BatchID) error {
	if _, err := r.client.QuestionBatch.Update().
		Where(
			entquestionbatch.IDEQ(string(id)),
			entquestionbatch.DeliveryStatusEQ(string(question.DeliveryInflight)),
		).
		SetDeliveryStatus(string(question.DeliveryAwaitingResume)).
		SetDeliveryProcessRunID("").
		Save(ctx); err != nil {
		return fmt.Errorf("reset question delivery awaiting resume: %w", err)
	}
	return nil
}

func (r *QuestionRepository) ResetDeliveryAwaitingResumeByProcessRun(ctx context.Context, processRunID question.ProcessRunID) ([]question.Batch, error) {
	rows, err := r.client.QuestionBatch.Query().
		Where(
			entquestionbatch.DeliveryProcessRunIDEQ(string(processRunID)),
			entquestionbatch.DeliveryStatusEQ(string(question.DeliveryInflight)),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list inflight question deliveries for reset: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if _, err := r.client.QuestionBatch.Update().
		Where(
			entquestionbatch.DeliveryProcessRunIDEQ(string(processRunID)),
			entquestionbatch.DeliveryStatusEQ(string(question.DeliveryInflight)),
		).
		SetDeliveryStatus(string(question.DeliveryAwaitingResume)).
		SetDeliveryProcessRunID("").
		Save(ctx); err != nil {
		return nil, fmt.Errorf("reset question deliveries awaiting resume: %w", err)
	}
	batches := make([]question.Batch, 0, len(rows))
	for _, row := range rows {
		batch, err := toDomainQuestionBatch(row)
		if err != nil {
			return nil, err
		}
		batch.DeliveryStatus = question.DeliveryAwaitingResume
		batch.DeliveryProcessRunID = nil
		batches = append(batches, batch)
	}
	return batches, nil
}

func (r *QuestionRepository) CancelUndeliveredBySession(ctx context.Context, sessionID question.SessionID) ([]question.Batch, error) {
	rows, err := r.client.QuestionBatch.Query().
		Where(
			entquestionbatch.SessionIDEQ(string(sessionID)),
			entquestionbatch.StatusEQ(string(question.BatchAnswered)),
			entquestionbatch.DeliveryStatusIn(string(question.DeliveryAwaitingResume), string(question.DeliveryInflight)),
		).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list undelivered question answers: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if _, err := r.client.QuestionBatch.Update().
		Where(
			entquestionbatch.SessionIDEQ(string(sessionID)),
			entquestionbatch.StatusEQ(string(question.BatchAnswered)),
			entquestionbatch.DeliveryStatusIn(string(question.DeliveryAwaitingResume), string(question.DeliveryInflight)),
		).
		SetDeliveryStatus(string(question.DeliveryNone)).
		SetDeliveryProcessRunID("").
		ClearDeliveredAt().
		Save(ctx); err != nil {
		return nil, fmt.Errorf("cancel undelivered question answers: %w", err)
	}
	batches := make([]question.Batch, 0, len(rows))
	for _, row := range rows {
		batch, err := toDomainQuestionBatch(row)
		if err != nil {
			return nil, err
		}
		batch.DeliveryStatus = question.DeliveryNone
		batch.DeliveryProcessRunID = nil
		batch.DeliveredAt = nil
		batches = append(batches, batch)
	}
	return batches, nil
}

func (r *QuestionRepository) SubmitAnswers(ctx context.Context, id question.BatchID, answers []question.Answer) (question.Batch, bool, error) {
	row, err := r.client.QuestionBatch.Get(ctx, string(id))
	if err != nil {
		return question.Batch{}, false, fmt.Errorf("find question batch for submit: %w", err)
	}
	batch, err := toDomainQuestionBatch(row)
	if err != nil {
		return question.Batch{}, false, err
	}
	if batch.Status != question.BatchPending {
		return batch, false, nil
	}
	applyAnswersToQuestions(&batch, answers)
	questions, err := questionsToJSON(batch.Questions)
	if err != nil {
		return question.Batch{}, false, fmt.Errorf("encode answered questions: %w", err)
	}
	answerJSON, err := answersToJSON(answers)
	if err != nil {
		return question.Batch{}, false, fmt.Errorf("encode question answers: %w", err)
	}
	answeredAt := time.Now()
	updated, err := r.client.QuestionBatch.Update().
		Where(
			entquestionbatch.IDEQ(string(id)),
			entquestionbatch.StatusEQ(string(question.BatchPending)),
		).
		SetStatus(string(question.BatchAnswered)).
		SetQuestions(questions).
		SetAnswers(answerJSON).
		SetAnsweredAt(answeredAt).
		Save(ctx)
	if err != nil {
		return question.Batch{}, false, fmt.Errorf("submit question answers: %w", err)
	}
	if updated == 0 {
		current, err := r.FindBatch(ctx, id)
		if err != nil {
			return question.Batch{}, false, err
		}
		return current, false, nil
	}
	batch.Status = question.BatchAnswered
	batch.AnsweredAt = &answeredAt
	return batch, true, nil
}

func (r *QuestionRepository) CancelPendingBySession(ctx context.Context, sessionID question.SessionID, reason string) ([]question.Batch, error) {
	if r.inTx {
		return cancelPendingQuestionBatches(ctx, r.client, sessionID, reason)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin cancel question batches: %w", err)
	}
	cancelled, err := cancelPendingQuestionBatches(ctx, tx.Client(), sessionID, reason)
	if err != nil {
		return nil, rollbackQuestionTx(tx, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit cancelled question batches: %w", err)
	}
	return cancelled, nil
}

func cancelPendingQuestionBatches(ctx context.Context, client *ent.Client, sessionID question.SessionID, reason string) ([]question.Batch, error) {
	rows, err := client.QuestionBatch.Query().
		Where(
			entquestionbatch.SessionIDEQ(string(sessionID)),
			entquestionbatch.StatusEQ(string(question.BatchPending)),
		).
		Order(ent.Asc(entquestionbatch.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending question batches for cancel: %w", err)
	}
	cancelled := make([]question.Batch, 0, len(rows))
	for _, row := range rows {
		updated, err := client.QuestionBatch.Update().
			Where(
				entquestionbatch.IDEQ(row.ID),
				entquestionbatch.StatusEQ(string(question.BatchPending)),
			).
			SetStatus(string(question.BatchCancelled)).
			SetCancelReason(reason).
			Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("cancel question batch %s: %w", row.ID, err)
		}
		if updated == 0 {
			continue
		}
		batch, err := toDomainQuestionBatch(row)
		if err != nil {
			return nil, err
		}
		batch.Status = question.BatchCancelled
		cancelled = append(cancelled, batch)
	}
	return cancelled, nil
}

func rollbackQuestionTx(tx *ent.Tx, err error) error {
	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		return fmt.Errorf("%w: rollback question tx: %v", err, rollbackErr)
	}
	return err
}

func toDomainQuestionBatch(row *ent.QuestionBatch) (question.Batch, error) {
	questions, err := questionsFromJSON(row.Questions)
	if err != nil {
		return question.Batch{}, fmt.Errorf("decode batch questions: %w", err)
	}
	var workflowRunID *question.WorkflowRunID
	if row.WorkflowRunID != nil {
		value := question.WorkflowRunID(*row.WorkflowRunID)
		workflowRunID = &value
	}
	var originProcessRunID *question.ProcessRunID
	if row.OriginProcessRunID != "" {
		value := question.ProcessRunID(row.OriginProcessRunID)
		originProcessRunID = &value
	}
	var deliveryProcessRunID *question.ProcessRunID
	if row.DeliveryProcessRunID != "" {
		value := question.ProcessRunID(row.DeliveryProcessRunID)
		deliveryProcessRunID = &value
	}
	return question.Batch{
		ID:                   question.BatchID(row.ID),
		SessionID:            question.SessionID(row.SessionID),
		WorkflowRunID:        workflowRunID,
		OriginProcessRunID:   originProcessRunID,
		Status:               question.BatchStatus(row.Status),
		DeliveryStatus:       normalizeDeliveryStatus(question.DeliveryStatus(row.DeliveryStatus)),
		DeliveryProcessRunID: deliveryProcessRunID,
		Questions:            questions,
		CreatedAt:            row.CreatedAt,
		AnsweredAt:           row.AnsweredAt,
		DeliveredAt:          row.DeliveredAt,
	}, nil
}

func normalizeDeliveryStatus(status question.DeliveryStatus) question.DeliveryStatus {
	switch status {
	case question.DeliveryAwaitingResume, question.DeliveryInflight, question.DeliveryDelivered:
		return status
	default:
		return question.DeliveryNone
	}
}

func applyAnswersToQuestions(batch *question.Batch, answers []question.Answer) {
	byQuestionID := make(map[question.QuestionID]question.Answer, len(answers))
	for _, answer := range answers {
		byQuestionID[answer.QuestionID] = answer
	}
	for i := range batch.Questions {
		answer, ok := byQuestionID[batch.Questions[i].ID]
		if !ok {
			continue
		}
		batch.Questions[i].SelectedOptionID = answer.SelectedOptionID
		batch.Questions[i].CustomAnswer = answer.CustomAnswer
		batch.Questions[i].Answer = payloadOrEmpty(answer.Payload)
		batch.Questions[i].Status = string(question.BatchAnswered)
	}
}

func questionsToJSON(questions []question.Question) ([]map[string]any, error) {
	var raw []map[string]any
	if err := roundTripJSON(questions, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		return []map[string]any{}, nil
	}
	return raw, nil
}

func questionsFromJSON(raw []map[string]any) ([]question.Question, error) {
	var questions []question.Question
	if err := roundTripJSON(raw, &questions); err != nil {
		return nil, err
	}
	if questions == nil {
		return []question.Question{}, nil
	}
	return questions, nil
}

func answersToJSON(answers []question.Answer) ([]map[string]any, error) {
	var raw []map[string]any
	if err := roundTripJSON(answers, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		return []map[string]any{}, nil
	}
	return raw, nil
}

func roundTripJSON(input any, output any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, output)
}

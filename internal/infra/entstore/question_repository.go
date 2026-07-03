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

type QuestionRepository struct {
	client *ent.Client
}

func NewQuestionRepository(client *ent.Client) *QuestionRepository {
	return &QuestionRepository{client: client}
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
		SetQuestions(questions)
	if batch.WorkflowRunID != nil {
		create.SetWorkflowRunID(string(*batch.WorkflowRunID))
	}
	if !batch.CreatedAt.IsZero() {
		create.SetCreatedAt(batch.CreatedAt)
	}
	if batch.AnsweredAt != nil {
		create.SetAnsweredAt(*batch.AnsweredAt)
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

func (r *QuestionRepository) SubmitAnswers(ctx context.Context, id question.BatchID, answers []question.Answer) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin submit question answers: %w", err)
	}
	row, err := tx.QuestionBatch.Get(ctx, string(id))
	if err != nil {
		return rollbackQuestionTx(tx, fmt.Errorf("find question batch for submit: %w", err))
	}
	batch, err := toDomainQuestionBatch(row)
	if err != nil {
		return rollbackQuestionTx(tx, err)
	}
	applyAnswersToQuestions(&batch, answers)
	questions, err := questionsToJSON(batch.Questions)
	if err != nil {
		return rollbackQuestionTx(tx, fmt.Errorf("encode answered questions: %w", err))
	}
	answerJSON, err := answersToJSON(answers)
	if err != nil {
		return rollbackQuestionTx(tx, fmt.Errorf("encode question answers: %w", err))
	}
	answeredAt := time.Now()
	if err := tx.QuestionBatch.UpdateOneID(string(id)).
		SetStatus(string(question.BatchAnswered)).
		SetQuestions(questions).
		SetAnswers(answerJSON).
		SetAnsweredAt(answeredAt).
		Exec(ctx); err != nil {
		return rollbackQuestionTx(tx, fmt.Errorf("submit question answers: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit question answers: %w", err)
	}
	return nil
}

func (r *QuestionRepository) CancelPendingBySession(ctx context.Context, sessionID question.SessionID, reason string) error {
	if _, err := r.client.QuestionBatch.Update().
		Where(
			entquestionbatch.SessionIDEQ(string(sessionID)),
			entquestionbatch.StatusEQ(string(question.BatchPending)),
		).
		SetStatus(string(question.BatchCancelled)).
		SetCancelReason(reason).
		Save(ctx); err != nil {
		return fmt.Errorf("cancel pending question batches: %w", err)
	}
	return nil
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
	return question.Batch{
		ID:            question.BatchID(row.ID),
		SessionID:     question.SessionID(row.SessionID),
		WorkflowRunID: workflowRunID,
		Status:        question.BatchStatus(row.Status),
		Questions:     questions,
		CreatedAt:     row.CreatedAt,
		AnsweredAt:    row.AnsweredAt,
	}, nil
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

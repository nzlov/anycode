package entstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nzlov/anycode/internal/domain/question"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	entquestionrequest "github.com/nzlov/anycode/internal/infra/entstore/ent/questionrequest"
)

var _ question.Repository = (*QuestionRepository)(nil)

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

func (r *QuestionRepository) CreateRequest(ctx context.Context, request question.Request) error {
	questions, err := questionsToJSON(request.Questions)
	if err != nil {
		return fmt.Errorf("encode request questions: %w", err)
	}
	create := r.client.QuestionRequest.Create().
		SetID(string(request.ID)).
		SetSessionID(string(request.SessionID)).
		SetStatus(string(request.Status)).
		SetQuestions(questions)
	if request.OriginProcessRunID != nil {
		create.SetOriginProcessRunID(string(*request.OriginProcessRunID))
	}
	if !request.CreatedAt.IsZero() {
		create.SetCreatedAt(request.CreatedAt)
	}
	if request.AnsweredAt != nil {
		create.SetAnsweredAt(*request.AnsweredAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("create question request: %w", err)
	}
	return nil
}

func (r *QuestionRepository) FindRequest(ctx context.Context, id question.RequestID) (question.Request, error) {
	row, err := r.client.QuestionRequest.Get(ctx, string(id))
	if err != nil {
		return question.Request{}, fmt.Errorf("find question request: %w", err)
	}
	return toDomainQuestionRequest(row)
}

func (r *QuestionRepository) ListPendingRequestsBySession(ctx context.Context, sessionID question.SessionID) ([]question.Request, error) {
	rows, err := r.client.QuestionRequest.Query().
		Where(
			entquestionrequest.SessionIDEQ(string(sessionID)),
			entquestionrequest.StatusEQ(string(question.RequestPending)),
		).
		Order(ent.Asc(entquestionrequest.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending question requests: %w", err)
	}
	requests := make([]question.Request, 0, len(rows))
	for _, row := range rows {
		request, err := toDomainQuestionRequest(row)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, nil
}

func (r *QuestionRepository) FindLatestRequestBySession(ctx context.Context, sessionID question.SessionID) (question.Request, bool, error) {
	row, err := r.client.QuestionRequest.Query().
		Where(entquestionrequest.SessionIDEQ(string(sessionID))).
		Order(ent.Desc(entquestionrequest.FieldCreatedAt), ent.Desc(entquestionrequest.FieldID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return question.Request{}, false, nil
		}
		return question.Request{}, false, fmt.Errorf("find latest question request: %w", err)
	}
	request, err := toDomainQuestionRequest(row)
	return request, err == nil, err
}

func (r *QuestionRepository) CancelPendingRequest(ctx context.Context, id question.RequestID, reason string) (question.Request, bool, error) {
	row, err := r.client.QuestionRequest.Get(ctx, string(id))
	if err != nil {
		return question.Request{}, false, fmt.Errorf("find question request for cancel: %w", err)
	}
	request, err := toDomainQuestionRequest(row)
	if err != nil {
		return question.Request{}, false, err
	}
	if request.Status != question.RequestPending {
		return request, false, nil
	}
	updated, err := r.client.QuestionRequest.Update().
		Where(
			entquestionrequest.IDEQ(string(id)),
			entquestionrequest.StatusEQ(string(question.RequestPending)),
		).
		SetStatus(string(question.RequestCancelled)).
		SetCancelReason(reason).
		Save(ctx)
	if err != nil {
		return question.Request{}, false, fmt.Errorf("cancel question request: %w", err)
	}
	if updated == 0 {
		current, err := r.FindRequest(ctx, id)
		if err != nil {
			return question.Request{}, false, err
		}
		return current, false, nil
	}
	request.Status = question.RequestCancelled
	return request, true, nil
}

func (r *QuestionRepository) FindPendingRequestByOriginProcessRun(ctx context.Context, processRunID question.ProcessRunID) (question.Request, bool, error) {
	row, err := r.client.QuestionRequest.Query().
		Where(
			entquestionrequest.OriginProcessRunIDEQ(string(processRunID)),
			entquestionrequest.StatusEQ(string(question.RequestPending)),
		).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return question.Request{}, false, nil
		}
		return question.Request{}, false, fmt.Errorf("find pending question request by process run: %w", err)
	}
	request, err := toDomainQuestionRequest(row)
	return request, err == nil, err
}

func (r *QuestionRepository) SubmitAnswers(ctx context.Context, id question.RequestID, answers []question.Answer) (question.Request, bool, error) {
	row, err := r.client.QuestionRequest.Get(ctx, string(id))
	if err != nil {
		return question.Request{}, false, fmt.Errorf("find question request for submit: %w", err)
	}
	request, err := toDomainQuestionRequest(row)
	if err != nil {
		return question.Request{}, false, err
	}
	if request.Status != question.RequestPending {
		return request, false, nil
	}
	applyAnswersToQuestions(&request, answers)
	questions, err := questionsToJSON(request.Questions)
	if err != nil {
		return question.Request{}, false, fmt.Errorf("encode answered questions: %w", err)
	}
	answerJSON, err := answersToJSON(answers)
	if err != nil {
		return question.Request{}, false, fmt.Errorf("encode question answers: %w", err)
	}
	answeredAt := time.Now()
	updated, err := r.client.QuestionRequest.Update().
		Where(
			entquestionrequest.IDEQ(string(id)),
			entquestionrequest.StatusEQ(string(question.RequestPending)),
		).
		SetStatus(string(question.RequestAnswered)).
		SetQuestions(questions).
		SetAnswers(answerJSON).
		SetAnsweredAt(answeredAt).
		Save(ctx)
	if err != nil {
		return question.Request{}, false, fmt.Errorf("submit question answers: %w", err)
	}
	if updated == 0 {
		current, err := r.FindRequest(ctx, id)
		if err != nil {
			return question.Request{}, false, err
		}
		return current, false, nil
	}
	request.Status = question.RequestAnswered
	request.AnsweredAt = &answeredAt
	return request, true, nil
}

func (r *QuestionRepository) CancelPendingRequestsBySession(ctx context.Context, sessionID question.SessionID, reason string) ([]question.Request, error) {
	if r.inTx {
		return cancelPendingQuestionRequests(ctx, r.client, sessionID, reason)
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin cancel question requests: %w", err)
	}
	cancelled, err := cancelPendingQuestionRequests(ctx, tx.Client(), sessionID, reason)
	if err != nil {
		return nil, rollbackQuestionTx(tx, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit cancelled question requests: %w", err)
	}
	return cancelled, nil
}

func cancelPendingQuestionRequests(ctx context.Context, client *ent.Client, sessionID question.SessionID, reason string) ([]question.Request, error) {
	rows, err := client.QuestionRequest.Query().
		Where(
			entquestionrequest.SessionIDEQ(string(sessionID)),
			entquestionrequest.StatusEQ(string(question.RequestPending)),
		).
		Order(ent.Asc(entquestionrequest.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending question requests for cancel: %w", err)
	}
	cancelled := make([]question.Request, 0, len(rows))
	for _, row := range rows {
		updated, err := client.QuestionRequest.Update().
			Where(
				entquestionrequest.IDEQ(row.ID),
				entquestionrequest.StatusEQ(string(question.RequestPending)),
			).
			SetStatus(string(question.RequestCancelled)).
			SetCancelReason(reason).
			Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("cancel question request %s: %w", row.ID, err)
		}
		if updated == 0 {
			continue
		}
		request, err := toDomainQuestionRequest(row)
		if err != nil {
			return nil, err
		}
		request.Status = question.RequestCancelled
		cancelled = append(cancelled, request)
	}
	return cancelled, nil
}

func rollbackQuestionTx(tx *ent.Tx, err error) error {
	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		return fmt.Errorf("%w: rollback question tx: %v", err, rollbackErr)
	}
	return err
}

func toDomainQuestionRequest(row *ent.QuestionRequest) (question.Request, error) {
	questions, err := questionsFromJSON(row.Questions)
	if err != nil {
		return question.Request{}, fmt.Errorf("decode request questions: %w", err)
	}
	var originProcessRunID *question.ProcessRunID
	if row.OriginProcessRunID != "" {
		value := question.ProcessRunID(row.OriginProcessRunID)
		originProcessRunID = &value
	}
	return question.Request{
		ID:                 question.RequestID(row.ID),
		SessionID:          question.SessionID(row.SessionID),
		OriginProcessRunID: originProcessRunID,
		Status:             question.RequestStatus(row.Status),
		Questions:          questions,
		CreatedAt:          row.CreatedAt,
		AnsweredAt:         row.AnsweredAt,
	}, nil
}

func applyAnswersToQuestions(request *question.Request, answers []question.Answer) {
	byQuestionID := make(map[question.QuestionID]question.Answer, len(answers))
	for _, answer := range answers {
		byQuestionID[answer.QuestionID] = answer
	}
	for i := range request.Questions {
		answer, ok := byQuestionID[request.Questions[i].ID]
		if !ok {
			continue
		}
		request.Questions[i].SelectedOptionID = answer.SelectedOptionID
		request.Questions[i].CustomAnswer = answer.CustomAnswer
		request.Questions[i].Answer = payloadOrEmpty(answer.Payload)
		request.Questions[i].Status = string(question.RequestAnswered)
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

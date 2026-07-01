package question

import (
	"context"

	domain "github.com/nzlov/anycode/internal/domain/question"
)

type UseCase interface {
	SubmitBatch(ctx context.Context, input SubmitBatchInput) (BatchDTO, error)
	GetBatch(ctx context.Context, id domain.BatchID) (BatchDTO, error)
}

type SubmitBatchInput struct {
	BatchID domain.BatchID
	Answers []domain.Answer
}

type BatchDTO struct {
	ID        domain.BatchID
	SessionID domain.SessionID
	Status    domain.BatchStatus
	Questions []domain.Question
}

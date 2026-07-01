package workflow

import (
	"context"

	domain "github.com/nzlov/anycode/internal/domain/workflow"
)

type UseCase interface {
	SaveDefinition(ctx context.Context, input SaveDefinitionInput) (DefinitionDTO, error)
	ActivateDefinition(ctx context.Context, id domain.DefinitionID) error
	SubmitApproval(ctx context.Context, input SubmitApprovalInput) (RunDTO, error)
}

type SaveDefinitionInput struct {
	ProjectID domain.ProjectID
	Name      string
	Graph     domain.Graph
}

type SubmitApprovalInput struct {
	WorkflowRunID domain.RunID
	NodeID        string
	Approved      bool
	Comment       string
}

type DefinitionDTO struct {
	ID        domain.DefinitionID
	ProjectID domain.ProjectID
	Name      string
	Version   int
	Graph     domain.Graph
	Active    bool
}

type RunDTO struct {
	ID            domain.RunID
	SessionID     domain.SessionID
	Status        domain.RunStatus
	CurrentNodeID string
	Context       domain.Context
}

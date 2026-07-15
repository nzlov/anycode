package workflow

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nzlov/anycode/internal/application/apperror"
	"github.com/nzlov/anycode/internal/application/port"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	domain "github.com/nzlov/anycode/internal/domain/workflow"
)

type UseCase interface {
	GetDefinition(ctx context.Context, id domain.DefinitionID) (DefinitionDTO, error)
	SaveDefinition(ctx context.Context, input SaveDefinitionInput) (DefinitionDTO, error)
	ActivateDefinition(ctx context.Context, id domain.DefinitionID) error
	SubmitApproval(ctx context.Context, input SubmitApprovalInput) (RunDTO, error)
	StartForSession(ctx context.Context, input sessiondomain.WorkflowStartInput) (sessiondomain.WorkflowStart, error)
	CompleteNode(ctx context.Context, input sessiondomain.WorkflowNodeCompleteInput) (sessiondomain.WorkflowAdvance, error)
	FailNode(ctx context.Context, input sessiondomain.WorkflowNodeFailInput) (sessiondomain.WorkflowAdvance, error)
	RecoverProcessExit(ctx context.Context, input sessiondomain.WorkflowProcessExitInput) (sessiondomain.WorkflowAdvance, error)
	MarkResumeFailedForSession(ctx context.Context, input sessiondomain.WorkflowResumeFailureInput) (sessiondomain.WorkflowRunSnapshot, error)
	ResumeCurrentNodeForSession(ctx context.Context, input sessiondomain.WorkflowResumeCurrentNodeInput) (sessiondomain.WorkflowAdvance, error)
	RerunCurrentNodeForSession(ctx context.Context, input sessiondomain.WorkflowRerunCurrentNodeInput) (sessiondomain.WorkflowAdvance, error)
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

type Service struct {
	repo       domain.Repository
	uow        port.UnitOfWork
	events     eventdomain.Store
	publisher  eventdomain.Publisher
	now        func() time.Time
	generateID func() (string, error)
}

type Option func(*Service)

func WithUnitOfWork(uow port.UnitOfWork) Option {
	return func(s *Service) {
		s.uow = uow
	}
}

func WithEvents(events eventdomain.Store) Option {
	return func(s *Service) {
		s.events = events
	}
}

func WithEventPublisher(publisher eventdomain.Publisher) Option {
	return func(s *Service) {
		s.publisher = publisher
	}
}

func New(repo domain.Repository, options ...Option) *Service {
	service := &Service{
		repo:       repo,
		now:        time.Now,
		generateID: generateID,
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) GetDefinition(ctx context.Context, id domain.DefinitionID) (DefinitionDTO, error) {
	if s == nil {
		return DefinitionDTO{}, errors.New("workflow usecase: nil service")
	}
	if s.repo == nil {
		return DefinitionDTO{}, errors.New("workflow repository is required")
	}
	if id == "" {
		return DefinitionDTO{}, errors.New("workflow definition id is required")
	}
	definition, err := s.loadDefinition(ctx, id)
	if err != nil {
		return DefinitionDTO{}, err
	}
	return toDefinitionDTO(definition), nil
}

func workflowValidationError(message string) *apperror.Error {
	return apperror.New(apperror.CodeValidationFailed, apperror.CategoryWorkflowError, message)
}

func (s *Service) SaveDefinition(ctx context.Context, input SaveDefinitionInput) (DefinitionDTO, error) {
	if s == nil {
		return DefinitionDTO{}, errors.New("workflow usecase: nil service")
	}
	if s.repo == nil {
		return DefinitionDTO{}, errors.New("workflow repository is required")
	}
	if input.ProjectID == "" {
		return DefinitionDTO{}, workflowValidationError("project id is required")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return DefinitionDTO{}, workflowValidationError("workflow definition name is required")
	}
	graph := domain.CanonicalGraph(input.Graph)
	if err := validateGraph(graph); err != nil {
		return DefinitionDTO{}, workflowValidationError(err.Error())
	}
	id, err := s.generateID()
	if err != nil {
		return DefinitionDTO{}, fmt.Errorf("generate workflow definition id: %w", err)
	}
	now := s.now()
	definition := domain.Definition{
		ID:        domain.DefinitionID(id),
		ProjectID: input.ProjectID,
		Name:      name,
		Version:   1,
		Graph:     graph,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repo.SaveDefinition(ctx, definition); err != nil {
		return DefinitionDTO{}, err
	}
	return toDefinitionDTO(definition), nil
}

func validateGraph(graph domain.Graph) error {
	nodes := make(map[string]struct{}, len(graph.Nodes))
	for _, node := range graph.Nodes {
		id := strings.TrimSpace(node.ID)
		if id == "" {
			return errors.New("workflow node id is required")
		}
		if _, exists := nodes[id]; exists {
			return fmt.Errorf("workflow node %q is duplicated", id)
		}
		nodes[id] = struct{}{}
		if isApprovalNode(node) && node.Approval.AfterRun {
			return fmt.Errorf("workflow approval node %q cannot require after-run approval", id)
		}
		for _, field := range node.OutputFields {
			if strings.TrimSpace(field.Key) == "" {
				return fmt.Errorf("workflow node %q output field key is required", id)
			}
			switch strings.TrimSpace(field.ValueType) {
			case "", "string", "number", "boolean", "object", "array", "any":
			default:
				return fmt.Errorf("workflow node %q output field %q has unsupported value type %q", id, field.Key, field.ValueType)
			}
		}
	}
	for _, edge := range graph.Edges {
		if strings.TrimSpace(edge.From) == "" || strings.TrimSpace(edge.To) == "" {
			return errors.New("workflow edge from and to are required")
		}
		if _, ok := nodes[edge.From]; !ok {
			return fmt.Errorf("workflow edge references unknown from node %q", edge.From)
		}
		if _, ok := nodes[edge.To]; !ok {
			return fmt.Errorf("workflow edge references unknown to node %q", edge.To)
		}
		if err := domain.ValidateCondition(edge.Condition); err != nil {
			return fmt.Errorf("workflow edge %s -> %s condition: %w", edge.From, edge.To, err)
		}
	}
	return nil
}

func (s *Service) ActivateDefinition(ctx context.Context, id domain.DefinitionID) error {
	if s == nil {
		return errors.New("workflow usecase: nil service")
	}
	if s.repo == nil {
		return errors.New("workflow repository is required")
	}
	if id == "" {
		return errors.New("workflow definition id is required")
	}
	return s.repo.ActivateDefinition(ctx, id)
}

func (s *Service) SubmitApproval(ctx context.Context, input SubmitApprovalInput) (RunDTO, error) {
	run, _, _, err := s.submitApproval(ctx, domain.RunID(input.WorkflowRunID), input.NodeID, input.Approved, input.Comment)
	if err != nil {
		return RunDTO{}, err
	}
	return toRunDTO(run), nil
}

func (s *Service) SubmitApprovalForSession(ctx context.Context, input sessiondomain.WorkflowApprovalInput) (sessiondomain.WorkflowApprovalResult, error) {
	run, advance, rejectedAfterRun, err := s.submitApproval(ctx, domain.RunID(input.WorkflowRunID), input.NodeID, input.Approved, input.Comment)
	if err != nil {
		return sessiondomain.WorkflowApprovalResult{}, err
	}
	return sessiondomain.WorkflowApprovalResult{
		Run:              toSessionWorkflowRunSnapshot(run),
		Advance:          advance,
		RejectedAfterRun: rejectedAfterRun,
	}, nil
}

func (s *Service) SubmitApprovalForSessionWithRepositories(ctx context.Context, input sessiondomain.WorkflowApprovalInput, repo domain.Repository, events eventdomain.Store) (sessiondomain.WorkflowApprovalResult, []eventdomain.DomainEvent, error) {
	if repo == nil {
		return sessiondomain.WorkflowApprovalResult{}, nil, errors.New("workflow repository is required")
	}
	recorder := &workflowEventRecorder{store: events}
	clone := *s
	clone.repo = repo
	clone.uow = nil
	clone.events = recorder
	clone.publisher = nil
	result, err := clone.SubmitApprovalForSession(ctx, input)
	if err != nil {
		return sessiondomain.WorkflowApprovalResult{}, nil, err
	}
	return result, append([]eventdomain.DomainEvent(nil), recorder.events...), nil
}

func (s *Service) submitApproval(ctx context.Context, workflowRunID domain.RunID, nodeID string, approved bool, comment string) (domain.Run, sessiondomain.WorkflowAdvance, bool, error) {
	if s == nil {
		return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, errors.New("workflow usecase: nil service")
	}
	if s.repo == nil {
		return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, errors.New("workflow repository is required")
	}
	if workflowRunID == "" {
		return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, errors.New("workflow run id is required")
	}
	if strings.TrimSpace(nodeID) == "" {
		return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, errors.New("workflow node id is required")
	}
	run, err := s.repo.FindRun(ctx, workflowRunID)
	if err != nil {
		return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, err
	}
	if run.Status != domain.RunWaitingApproval {
		return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, fmt.Errorf("workflow run cannot accept approval from status %q", run.Status)
	}
	if run.CurrentNodeID != nodeID {
		return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, fmt.Errorf("workflow run is waiting on node %q, not %q", run.CurrentNodeID, nodeID)
	}
	nodeRun, err := s.repo.FindLatestNodeRun(ctx, run.ID, nodeID)
	if err != nil {
		return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, err
	}
	if nodeRun.Status != domain.NodeWaitingApproval {
		return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, fmt.Errorf("node run cannot accept approval from status %q", nodeRun.Status)
	}
	definition, err := s.loadDefinition(ctx, run.WorkflowDefinitionID)
	if err != nil {
		return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, err
	}
	if err := validateGraph(definition.Graph); err != nil {
		return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, workflowValidationError(err.Error())
	}
	node, err := findNode(definition.Graph, run.CurrentNodeID)
	if err != nil {
		return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, err
	}
	now := s.now()
	approval := approvalValue(approved, comment)
	if isAfterRunApproval(node, nodeRun) && !approved {
		failedNodeRun := domain.NodeRun{
			ID:            nodeRun.ID,
			WorkflowRunID: nodeRun.WorkflowRunID,
			NodeID:        nodeRun.NodeID,
			Status:        domain.NodeFailed,
			Attempt:       nodeRun.Attempt,
			ProcessRunID:  nodeRun.ProcessRunID,
			StartedAt:     nodeRun.StartedAt,
			FinishedAt:    &now,
			Result:        nodeRun.Result,
		}
		run.Context = contextWithApproval(contextAfterFailedNode(run.Context, nodeRun.Result), approval)
		nextNodeRun, advance, err := s.nextNodeRunForNode(&run, node, nodeRun.Attempt+1, now, false, true)
		if err != nil {
			return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, err
		}
		if err := s.saveWorkflowMutation(ctx, definition, run, workflowEventInputFromAdvance(advance), func(ctx context.Context, repo domain.Repository) error {
			return repo.CompleteNodeAndAdvance(ctx, failedNodeRun, run, &nextNodeRun)
		}); err != nil {
			return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, err
		}
		advancedRun, err := s.repo.FindRun(ctx, domain.RunID(advance.WorkflowRunID))
		if err != nil {
			return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, err
		}
		return advancedRun, advance, true, nil
	}
	if isBeforeRunApproval(node, nodeRun) && approved {
		if isCloseNode(node) {
			nodeRun.Status = domain.NodeSucceeded
			nodeRun.FinishedAt = &now
			run.Status = domain.RunCompleted
			run.StoppedAt = &now
			run.Context = contextWithApproval(run.Context, approval)
			nodeRunID := sessiondomain.NodeRunID(nodeRun.ID)
			advance := sessiondomain.WorkflowAdvance{
				WorkflowRunID:    sessiondomain.WorkflowRunID(run.ID),
				NodeRunID:        &nodeRunID,
				CurrentNodeID:    node.ID,
				CurrentNodeTitle: node.Title,
				Status:           string(run.Status),
				Close:            true,
			}
			if err := s.saveWorkflowMutation(ctx, definition, run, workflowEventInputFromAdvance(advance), func(ctx context.Context, repo domain.Repository) error {
				return repo.CompleteNodeAndAdvance(ctx, nodeRun, run, nil)
			}); err != nil {
				return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, err
			}
			return run, advance, false, nil
		}
		nodeRun.Status = domain.NodeRunning
		run.Status = domain.RunRunning
		run.Context = contextWithApproval(run.Context, approval)
		nodeRunID := sessiondomain.NodeRunID(nodeRun.ID)
		advance := sessiondomain.WorkflowAdvance{
			WorkflowRunID:    sessiondomain.WorkflowRunID(run.ID),
			NodeRunID:        &nodeRunID,
			CurrentNodeID:    node.ID,
			CurrentNodeTitle: node.Title,
			Status:           string(run.Status),
			RequiresCodex:    isCodexNode(node),
			Prompt:           nodePrompt("", node, paramsFromContext(run.Context)),
			Merge:            mergeRequest(node),
			Expr:             exprRequest(node, paramsFromContext(run.Context)),
		}
		if advance.Merge != nil || advance.Expr != nil {
			advance.RequiresCodex = false
		}
		if err := s.saveWorkflowMutation(ctx, definition, run, workflowEventInputFromAdvance(advance), func(ctx context.Context, repo domain.Repository) error {
			return repo.CompleteNodeAndAdvance(ctx, nodeRun, run, nil)
		}); err != nil {
			return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, err
		}
		advancedRun, err := s.repo.FindRun(ctx, domain.RunID(advance.WorkflowRunID))
		if err != nil {
			return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, err
		}
		return advancedRun, advance, false, nil
	}
	if !approved {
		nodeRun.Status = domain.NodeFailed
		nodeRun.FinishedAt = &now
		run.Status = domain.RunBlocked
		run.Context = contextWithApproval(run.Context, approval)
		run.Context.Values["blockedReason"] = "approval rejected"
		advance := sessiondomain.WorkflowAdvance{
			WorkflowRunID: sessiondomain.WorkflowRunID(run.ID),
			Status:        string(run.Status),
			Blocked:       true,
			BlockedReason: "approval rejected",
		}
		if err := s.saveWorkflowMutation(ctx, definition, run, workflowEventInputFromAdvance(advance), func(ctx context.Context, repo domain.Repository) error {
			return repo.CompleteNodeAndAdvance(ctx, nodeRun, run, nil)
		}); err != nil {
			return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, err
		}
		return run, advance, false, nil
	}
	run.Context = contextWithApproval(run.Context, approval)
	advance, err := s.completeNodeWithOptions(ctx, run, nodeRun.ID, nodeRun.Result, completeNodeOptions{skipAfterRunApproval: isAfterRunApproval(node, nodeRun)})
	if err != nil {
		return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, err
	}
	advancedRun, err := s.repo.FindRun(ctx, domain.RunID(advance.WorkflowRunID))
	if err != nil {
		return domain.Run{}, sessiondomain.WorkflowAdvance{}, false, err
	}
	return advancedRun, advance, false, nil
}

func (s *Service) StartForSession(ctx context.Context, input sessiondomain.WorkflowStartInput) (sessiondomain.WorkflowStart, error) {
	if s == nil {
		return sessiondomain.WorkflowStart{}, errors.New("workflow usecase: nil service")
	}
	if s.repo == nil {
		return sessiondomain.WorkflowStart{}, errors.New("workflow repository is required")
	}
	definition, err := s.definitionForStart(ctx, input)
	if err != nil {
		return sessiondomain.WorkflowStart{}, err
	}
	node, err := firstNode(definition.Graph)
	if err != nil {
		return sessiondomain.WorkflowStart{}, workflowValidationError(err.Error())
	}
	runID, err := s.generateID()
	if err != nil {
		return sessiondomain.WorkflowStart{}, fmt.Errorf("generate workflow run id: %w", err)
	}
	nodeRunID, err := s.generateID()
	if err != nil {
		return sessiondomain.WorkflowStart{}, fmt.Errorf("generate workflow node run id: %w", err)
	}
	now := s.now()
	runStatus := domain.RunRunning
	nodeStatus := domain.NodeRunning
	requiresCodex := true
	closeNode := isCloseNode(node)
	merge := mergeRequest(node)
	params := map[string]any{"requirement": strings.TrimSpace(input.Requirement)}
	expr := exprRequest(node, params)
	if requiresApproval(node) {
		runStatus = domain.RunWaitingApproval
		nodeStatus = domain.NodeWaitingApproval
		requiresCodex = false
	} else if closeNode {
		runStatus = domain.RunCompleted
		nodeStatus = domain.NodeSucceeded
		requiresCodex = false
	} else if merge != nil {
		requiresCodex = false
	} else if expr != nil {
		requiresCodex = false
	}
	contextValue := domain.Context{Values: map[string]any{
		"requirement": strings.TrimSpace(input.Requirement),
		"params":      params,
		"node": map[string]any{
			"id":    node.ID,
			"title": node.Title,
			"type":  node.Type,
		},
	}}
	run := domain.Run{
		ID:                   domain.RunID(runID),
		SessionID:            domain.SessionID(input.SessionID),
		WorkflowDefinitionID: definition.ID,
		Status:               runStatus,
		CurrentNodeID:        node.ID,
		Context:              contextValue,
		StartedAt:            &now,
	}
	if closeNode && runStatus == domain.RunCompleted {
		run.StoppedAt = &now
	}
	nodeRun := domain.NodeRun{
		ID:            domain.NodeRunID(nodeRunID),
		WorkflowRunID: run.ID,
		NodeID:        node.ID,
		Status:        nodeStatus,
		Attempt:       1,
		StartedAt:     &now,
	}
	if closeNode && nodeStatus == domain.NodeSucceeded {
		nodeRun.FinishedAt = &now
	}
	resultNodeRunID := sessiondomain.NodeRunID(nodeRun.ID)
	start := sessiondomain.WorkflowStart{
		WorkflowRunID:    sessiondomain.WorkflowRunID(run.ID),
		NodeRunID:        &resultNodeRunID,
		CurrentNodeID:    node.ID,
		CurrentNodeTitle: node.Title,
		Status:           string(runStatus),
		RequiresCodex:    requiresCodex,
		Prompt:           nodePrompt(input.Requirement, node, params),
		Merge:            merge,
		Expr:             expr,
		Close:            closeNode && runStatus == domain.RunCompleted,
	}
	if runStatus == domain.RunWaitingApproval {
		start.ApprovalPhase = "before_run"
		start.Merge = nil
		start.Expr = nil
	}
	if err := s.saveWorkflowMutation(ctx, definition, run, workflowEventInputFromStart(start), func(ctx context.Context, repo domain.Repository) error {
		return repo.CreateInitialRun(ctx, run, nodeRun)
	}); err != nil {
		return sessiondomain.WorkflowStart{}, err
	}
	return start, nil
}

func (s *Service) MarkStartFailed(ctx context.Context, input sessiondomain.WorkflowStartFailureInput) error {
	if s == nil {
		return errors.New("workflow usecase: nil service")
	}
	if s.repo == nil {
		return errors.New("workflow repository is required")
	}
	if input.WorkflowRunID == "" || input.NodeRunID == nil {
		return nil
	}
	run, err := s.repo.FindRun(ctx, domain.RunID(input.WorkflowRunID))
	if err != nil {
		return err
	}
	definition, err := s.loadDefinition(ctx, run.WorkflowDefinitionID)
	if err != nil {
		return err
	}
	finishedAt := s.now()
	failure := domain.NodeFailure{
		Code:    strings.TrimSpace(input.Code),
		Message: strings.TrimSpace(input.Message),
	}
	run.Status = domain.RunFailed
	run.StoppedAt = &finishedAt
	return s.saveWorkflowMutation(ctx, definition, run, workflowEventInput{
		eventType: "workflow.failed",
		payload: map[string]any{
			"workflowRunId": string(input.WorkflowRunID),
			"nodeRunId":     string(*input.NodeRunID),
			"code":          failure.Code,
			"message":       failure.Message,
		},
	}, func(ctx context.Context, repo domain.Repository) error {
		return repo.MarkRunFailed(ctx, domain.RunID(input.WorkflowRunID), domain.NodeRunID(*input.NodeRunID), failure, finishedAt)
	})
}

func (s *Service) MarkResumeFailedForSession(ctx context.Context, input sessiondomain.WorkflowResumeFailureInput) (sessiondomain.WorkflowRunSnapshot, error) {
	if s == nil {
		return sessiondomain.WorkflowRunSnapshot{}, errors.New("workflow usecase: nil service")
	}
	if s.repo == nil {
		return sessiondomain.WorkflowRunSnapshot{}, errors.New("workflow repository is required")
	}
	if input.SessionID == "" {
		return sessiondomain.WorkflowRunSnapshot{}, errors.New("session id is required")
	}
	run, err := s.repo.FindLatestRunBySession(ctx, domain.SessionID(input.SessionID))
	if err != nil {
		return sessiondomain.WorkflowRunSnapshot{}, err
	}
	if run.Status == domain.RunWaitingResumeAction {
		return toSessionWorkflowRunSnapshot(run), nil
	}
	definition, err := s.loadDefinition(ctx, run.WorkflowDefinitionID)
	if err != nil {
		return sessiondomain.WorkflowRunSnapshot{}, err
	}
	run.Status = domain.RunWaitingResumeAction
	run.Context = contextAfterResumeFailure(run.Context, strings.TrimSpace(input.Code), strings.TrimSpace(input.Message))
	eventInput := workflowEventInput{
		eventType: "workflow.waiting_resume_action",
		payload: map[string]any{
			"workflowRunId": string(run.ID),
			"currentNodeId": run.CurrentNodeID,
			"code":          strings.TrimSpace(input.Code),
			"message":       strings.TrimSpace(input.Message),
		},
	}
	if err := s.saveWorkflowMutation(ctx, definition, run, eventInput, func(ctx context.Context, repo domain.Repository) error {
		return repo.UpdateRunState(ctx, run)
	}); err != nil {
		return sessiondomain.WorkflowRunSnapshot{}, err
	}
	return toSessionWorkflowRunSnapshot(run), nil
}

func (s *Service) MarkResumeFailedForSessionWithRepositories(ctx context.Context, input sessiondomain.WorkflowResumeFailureInput, repo domain.Repository, events eventdomain.Store) (sessiondomain.WorkflowRunSnapshot, []eventdomain.DomainEvent, error) {
	if repo == nil {
		return sessiondomain.WorkflowRunSnapshot{}, nil, errors.New("workflow repository is required")
	}
	recorder := &workflowEventRecorder{store: events}
	clone := *s
	clone.repo = repo
	clone.uow = nil
	clone.events = recorder
	clone.publisher = nil
	result, err := clone.MarkResumeFailedForSession(ctx, input)
	if err != nil {
		return sessiondomain.WorkflowRunSnapshot{}, nil, err
	}
	return result, append([]eventdomain.DomainEvent(nil), recorder.events...), nil
}

func (s *Service) ResumeCurrentNodeForSession(ctx context.Context, input sessiondomain.WorkflowResumeCurrentNodeInput) (sessiondomain.WorkflowAdvance, error) {
	if s == nil {
		return sessiondomain.WorkflowAdvance{}, errors.New("workflow usecase: nil service")
	}
	if s.repo == nil {
		return sessiondomain.WorkflowAdvance{}, errors.New("workflow repository is required")
	}
	if input.SessionID == "" {
		return sessiondomain.WorkflowAdvance{}, errors.New("session id is required")
	}
	run, err := s.repo.FindLatestRunBySession(ctx, domain.SessionID(input.SessionID))
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	switch run.Status {
	case domain.RunRunning, domain.RunWaitingResumeAction:
	default:
		return sessiondomain.WorkflowAdvance{}, fmt.Errorf("workflow run cannot resume current node from status %q", run.Status)
	}
	definition, err := s.loadDefinition(ctx, run.WorkflowDefinitionID)
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	node, err := findNode(definition.Graph, run.CurrentNodeID)
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	if !isCodexNode(node) {
		return sessiondomain.WorkflowAdvance{}, fmt.Errorf("workflow node %q cannot resume a Codex process", node.ID)
	}
	nodeRun, err := s.repo.FindLatestNodeRun(ctx, run.ID, run.CurrentNodeID)
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	run.Status = domain.RunRunning
	run.Context = contextAfterResumeRetry(run.Context, strings.TrimSpace(input.Reason))
	resultNodeRunID := sessiondomain.NodeRunID(nodeRun.ID)
	advance := sessiondomain.WorkflowAdvance{
		WorkflowRunID:    sessiondomain.WorkflowRunID(run.ID),
		NodeRunID:        &resultNodeRunID,
		CurrentNodeID:    node.ID,
		CurrentNodeTitle: node.Title,
		Status:           string(run.Status),
		RequiresCodex:    true,
		Prompt:           nodePrompt("", node, paramsFromContext(run.Context)),
	}
	if err := s.saveWorkflowMutation(ctx, definition, run, workflowEventInput{
		eventType: "workflow.resume_current_node",
		payload: map[string]any{
			"workflowRunId":    string(run.ID),
			"nodeRunId":        string(nodeRun.ID),
			"currentNodeId":    node.ID,
			"currentNodeTitle": node.Title,
			"reason":           strings.TrimSpace(input.Reason),
		},
	}, func(ctx context.Context, repo domain.Repository) error {
		return repo.UpdateRunState(ctx, run)
	}); err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	return advance, nil
}

func (s *Service) RerunCurrentNodeForSession(ctx context.Context, input sessiondomain.WorkflowRerunCurrentNodeInput) (sessiondomain.WorkflowAdvance, error) {
	if s == nil {
		return sessiondomain.WorkflowAdvance{}, errors.New("workflow usecase: nil service")
	}
	if s.repo == nil {
		return sessiondomain.WorkflowAdvance{}, errors.New("workflow repository is required")
	}
	if input.SessionID == "" {
		return sessiondomain.WorkflowAdvance{}, errors.New("session id is required")
	}
	run, err := s.repo.FindLatestRunBySession(ctx, domain.SessionID(input.SessionID))
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	if run.Status != domain.RunWaitingResumeAction {
		return sessiondomain.WorkflowAdvance{}, fmt.Errorf("workflow run cannot rerun current node from status %q", run.Status)
	}
	definition, err := s.loadDefinition(ctx, run.WorkflowDefinitionID)
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	node, err := findNode(definition.Graph, run.CurrentNodeID)
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	nodeRun, err := s.repo.FindLatestNodeRun(ctx, run.ID, run.CurrentNodeID)
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	failure := domain.NodeFailure{
		Code:    "resume_rerun",
		Message: strings.TrimSpace(input.Reason),
	}
	run.Context = contextAfterResumeRerun(run.Context, strings.TrimSpace(input.Reason))
	planner := domain.DefaultPlanner{}
	if !planner.ShouldRetry(node, nodeRun.Attempt, failure) {
		reason := "workflow node retry limit reached"
		run.Status = domain.RunBlocked
		run.Context.Values["blockedReason"] = reason
		advance := sessiondomain.WorkflowAdvance{
			WorkflowRunID: sessiondomain.WorkflowRunID(run.ID),
			Status:        string(run.Status),
			Blocked:       true,
			BlockedReason: reason,
		}
		if err := s.saveWorkflowMutation(ctx, definition, run, workflowEventInputFromAdvance(advance), func(ctx context.Context, repo domain.Repository) error {
			return repo.UpdateRunState(ctx, run)
		}); err != nil {
			return sessiondomain.WorkflowAdvance{}, err
		}
		return advance, nil
	}
	now := s.now()
	nextNodeRun, advance, err := s.nextNodeRunForNode(&run, node, nodeRun.Attempt+1, now, false, false)
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	if err := s.saveWorkflowMutation(ctx, definition, run, workflowEventInput{
		eventType: "workflow.rerun_current_node",
		payload: map[string]any{
			"workflowRunId":    string(advance.WorkflowRunID),
			"nodeRunId":        stringValuePtr(advance.NodeRunID),
			"currentNodeId":    advance.CurrentNodeID,
			"currentNodeTitle": advance.CurrentNodeTitle,
			"reason":           strings.TrimSpace(input.Reason),
		},
	}, func(ctx context.Context, repo domain.Repository) error {
		return repo.CreateNodeRunAndUpdateRun(ctx, run, nextNodeRun)
	}); err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	return advance, nil
}

func (s *Service) CompleteNode(ctx context.Context, input sessiondomain.WorkflowNodeCompleteInput) (sessiondomain.WorkflowAdvance, error) {
	if s == nil {
		return sessiondomain.WorkflowAdvance{}, errors.New("workflow usecase: nil service")
	}
	if s.repo == nil {
		return sessiondomain.WorkflowAdvance{}, errors.New("workflow repository is required")
	}
	if input.WorkflowRunID == "" {
		return sessiondomain.WorkflowAdvance{}, errors.New("workflow run id is required")
	}
	if input.NodeRunID == "" {
		return sessiondomain.WorkflowAdvance{}, errors.New("node run id is required")
	}
	run, err := s.repo.FindRun(ctx, domain.RunID(input.WorkflowRunID))
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	if run.Status != domain.RunRunning {
		return s.workflowAdvanceFromPersistedRun(ctx, run)
	}
	nodeRun, err := s.repo.FindLatestNodeRun(ctx, run.ID, run.CurrentNodeID)
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	if nodeRun.ID != domain.NodeRunID(input.NodeRunID) || (nodeRun.Status != domain.NodeRunning && nodeRun.Status != domain.NodeWaitingUser) {
		return s.workflowAdvanceFromPersistedRun(ctx, run)
	}
	return s.completeNode(ctx, run, domain.NodeRunID(input.NodeRunID), input.Output)
}

func (s *Service) FailNode(ctx context.Context, input sessiondomain.WorkflowNodeFailInput) (sessiondomain.WorkflowAdvance, error) {
	if s == nil {
		return sessiondomain.WorkflowAdvance{}, errors.New("workflow usecase: nil service")
	}
	if s.repo == nil {
		return sessiondomain.WorkflowAdvance{}, errors.New("workflow repository is required")
	}
	if input.WorkflowRunID == "" {
		return sessiondomain.WorkflowAdvance{}, errors.New("workflow run id is required")
	}
	if input.NodeRunID == "" {
		return sessiondomain.WorkflowAdvance{}, errors.New("node run id is required")
	}
	run, err := s.repo.FindRun(ctx, domain.RunID(input.WorkflowRunID))
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	return s.failNode(ctx, run, domain.NodeRunID(input.NodeRunID), domain.NodeFailure{
		Code:    strings.TrimSpace(input.Code),
		Message: strings.TrimSpace(input.Message),
	}, input.Output)
}

func (s *Service) RecoverProcessExit(ctx context.Context, input sessiondomain.WorkflowProcessExitInput) (sessiondomain.WorkflowAdvance, error) {
	if s == nil {
		return sessiondomain.WorkflowAdvance{}, errors.New("workflow usecase: nil service")
	}
	if s.repo == nil {
		return sessiondomain.WorkflowAdvance{}, errors.New("workflow repository is required")
	}
	if input.WorkflowRunID == "" {
		return sessiondomain.WorkflowAdvance{}, errors.New("workflow run id is required")
	}
	if input.NodeRunID == "" {
		return sessiondomain.WorkflowAdvance{}, errors.New("node run id is required")
	}
	run, err := s.repo.FindRun(ctx, domain.RunID(input.WorkflowRunID))
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	if run.CurrentNodeID != "" {
		nodeRun, findErr := s.repo.FindLatestNodeRun(ctx, run.ID, run.CurrentNodeID)
		if findErr != nil {
			return sessiondomain.WorkflowAdvance{}, findErr
		}
		if nodeRun.ID == domain.NodeRunID(input.NodeRunID) && (nodeRun.Status == domain.NodeRunning || nodeRun.Status == domain.NodeWaitingUser) {
			if input.Failed {
				return s.failNode(ctx, run, nodeRun.ID, domain.NodeFailure{
					Code:    strings.TrimSpace(input.FailureCode),
					Message: strings.TrimSpace(input.FailureMessage),
				}, input.Output)
			}
			return s.completeNode(ctx, run, nodeRun.ID, input.Output)
		}
	}
	return s.workflowAdvanceFromPersistedRun(ctx, run)
}

func (s *Service) workflowAdvanceFromPersistedRun(ctx context.Context, run domain.Run) (sessiondomain.WorkflowAdvance, error) {
	advance := sessiondomain.WorkflowAdvance{
		WorkflowRunID: sessiondomain.WorkflowRunID(run.ID),
		Status:        string(run.Status),
	}
	switch run.Status {
	case domain.RunBlocked:
		advance.Blocked = true
		advance.BlockedReason, _ = run.Context.Values["blockedReason"].(string)
		return advance, nil
	case domain.RunCompleted:
		if run.CurrentNodeID == "" {
			advance.Completed = true
			return advance, nil
		}
		definition, err := s.loadDefinition(ctx, run.WorkflowDefinitionID)
		if err != nil {
			return sessiondomain.WorkflowAdvance{}, err
		}
		node, err := findNode(definition.Graph, run.CurrentNodeID)
		if err != nil {
			return sessiondomain.WorkflowAdvance{}, err
		}
		if isCloseNode(node) {
			nodeRun, err := s.repo.FindLatestNodeRun(ctx, run.ID, run.CurrentNodeID)
			if err != nil {
				return sessiondomain.WorkflowAdvance{}, err
			}
			nodeRunID := sessiondomain.NodeRunID(nodeRun.ID)
			advance.NodeRunID = &nodeRunID
			advance.CurrentNodeID = node.ID
			advance.CurrentNodeTitle = node.Title
			advance.Close = true
			return advance, nil
		}
		advance.Completed = true
		return advance, nil
	case domain.RunWaitingResumeAction:
		return advance, nil
	case domain.RunFailed:
		return advance, nil
	case domain.RunRunning, domain.RunWaitingApproval:
	default:
		return sessiondomain.WorkflowAdvance{}, fmt.Errorf("workflow run cannot recover process exit from status %q", run.Status)
	}
	definition, err := s.loadDefinition(ctx, run.WorkflowDefinitionID)
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	node, err := findNode(definition.Graph, run.CurrentNodeID)
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	nodeRun, err := s.repo.FindLatestNodeRun(ctx, run.ID, run.CurrentNodeID)
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	nodeRunID := sessiondomain.NodeRunID(nodeRun.ID)
	advance.NodeRunID = &nodeRunID
	advance.CurrentNodeID = node.ID
	advance.CurrentNodeTitle = node.Title
	if run.Status == domain.RunWaitingApproval {
		advance.RequiresCodex = false
		advance.ApprovalPhase = "before_run"
		if nodeRun.Result != nil {
			advance.ApprovalPhase = "after_run"
			advance.Result = resultMap(nodeRun.Result)
		} else {
			advance.Result = nil
		}
		return advance, nil
	}
	advance.RequiresCodex = run.Status == domain.RunRunning
	advance.Prompt = nodePrompt("", node, paramsFromContext(run.Context))
	advance.Merge = mergeRequest(node)
	advance.Expr = exprRequest(node, paramsFromContext(run.Context))
	if advance.Merge != nil || advance.Expr != nil {
		advance.RequiresCodex = false
	}
	return advance, nil
}

func (s *Service) completeNode(ctx context.Context, run domain.Run, nodeRunID domain.NodeRunID, output map[string]any) (sessiondomain.WorkflowAdvance, error) {
	definition, err := s.loadDefinition(ctx, run.WorkflowDefinitionID)
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	currentNode, err := findNode(definition.Graph, run.CurrentNodeID)
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	result, err := decodeWorkflowResult(output, currentNode)
	if err != nil {
		if !isCodexNode(currentNode) {
			return sessiondomain.WorkflowAdvance{}, apperror.Wrap(err, apperror.CodeWorkflowResultInvalid, apperror.CategoryWorkflowError, "workflow node result is invalid")
		}
		if boolOutputValue(output, "resultRetry") {
			return sessiondomain.WorkflowAdvance{}, apperror.Wrap(err, apperror.CodeWorkflowResultInvalid, apperror.CategoryWorkflowError, "workflow node result is invalid after correction")
		}
		resultNodeRunID := sessiondomain.NodeRunID(nodeRunID)
		return sessiondomain.WorkflowAdvance{
			WorkflowRunID:      sessiondomain.WorkflowRunID(run.ID),
			NodeRunID:          &resultNodeRunID,
			CurrentNodeID:      currentNode.ID,
			CurrentNodeTitle:   currentNode.Title,
			Status:             string(domain.RunRunning),
			RequiresCodex:      true,
			RequireResultRetry: true,
			Prompt:             resultRetryPrompt(currentNode, err),
		}, nil
	}
	return s.completeNodeWithOptions(ctx, run, nodeRunID, result, completeNodeOptions{})
}

type completeNodeOptions struct {
	skipAfterRunApproval bool
}

func (s *Service) completeNodeWithOptions(ctx context.Context, run domain.Run, nodeRunID domain.NodeRunID, result *domain.Result, options completeNodeOptions) (sessiondomain.WorkflowAdvance, error) {
	definition, err := s.loadDefinition(ctx, run.WorkflowDefinitionID)
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	currentNode, err := findNode(definition.Graph, run.CurrentNodeID)
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	now := s.now()
	completedNodeRun := domain.NodeRun{
		ID:            nodeRunID,
		WorkflowRunID: run.ID,
		NodeID:        run.CurrentNodeID,
		Status:        domain.NodeSucceeded,
		FinishedAt:    &now,
		Result:        result,
	}
	contextValue := contextAfterNode(run.Context, completedNodeRun.Result)
	if currentNode.Approval.AfterRun && !options.skipAfterRunApproval {
		completedNodeRun.Status = domain.NodeWaitingApproval
		completedNodeRun.FinishedAt = nil
		run.Status = domain.RunWaitingApproval
		run.Context = contextValue
		resultNodeRunID := sessiondomain.NodeRunID(completedNodeRun.ID)
		advance := sessiondomain.WorkflowAdvance{
			WorkflowRunID:    sessiondomain.WorkflowRunID(run.ID),
			NodeRunID:        &resultNodeRunID,
			CurrentNodeID:    currentNode.ID,
			CurrentNodeTitle: currentNode.Title,
			Status:           string(run.Status),
			RequiresCodex:    false,
			ApprovalPhase:    "after_run",
			Result:           resultMap(completedNodeRun.Result),
		}
		if err := s.saveWorkflowMutation(ctx, definition, run, workflowEventInputFromAdvance(advance), func(ctx context.Context, repo domain.Repository) error {
			return repo.CompleteNodeAndAdvance(ctx, completedNodeRun, run, nil)
		}); err != nil {
			return sessiondomain.WorkflowAdvance{}, err
		}
		return advance, nil
	}
	planner := domain.DefaultPlanner{}
	decision, err := planner.NextNode(definition, run, contextValue)
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	run.Context = contextValue
	if decision.Blocked {
		run.Status = domain.RunBlocked
		run.Context.Values["blockedReason"] = decision.Reason
		advance := sessiondomain.WorkflowAdvance{
			WorkflowRunID: sessiondomain.WorkflowRunID(run.ID),
			Status:        string(run.Status),
			Blocked:       true,
			BlockedReason: decision.Reason,
		}
		if err := s.saveWorkflowMutation(ctx, definition, run, workflowEventInputFromAdvance(advance), func(ctx context.Context, repo domain.Repository) error {
			return repo.CompleteNodeAndAdvance(ctx, completedNodeRun, run, nil)
		}); err != nil {
			return sessiondomain.WorkflowAdvance{}, err
		}
		return advance, nil
	}
	if decision.NextNodeID == "" {
		run.Status = domain.RunCompleted
		run.StoppedAt = &now
		advance := sessiondomain.WorkflowAdvance{
			WorkflowRunID: sessiondomain.WorkflowRunID(run.ID),
			Status:        string(run.Status),
			Completed:     true,
		}
		if err := s.saveWorkflowMutation(ctx, definition, run, workflowEventInputFromAdvance(advance), func(ctx context.Context, repo domain.Repository) error {
			return repo.CompleteNodeAndAdvance(ctx, completedNodeRun, run, nil)
		}); err != nil {
			return sessiondomain.WorkflowAdvance{}, err
		}
		return advance, nil
	}
	node, err := findNode(definition.Graph, decision.NextNodeID)
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	if isCloseNode(node) && !requiresApproval(node) {
		nextNodeRunID, err := s.generateID()
		if err != nil {
			return sessiondomain.WorkflowAdvance{}, fmt.Errorf("generate workflow node run id: %w", err)
		}
		run.CurrentNodeID = node.ID
		run.Status = domain.RunCompleted
		run.StoppedAt = &now
		run.Context = contextForNextNode(run.Context)
		nextNodeRun := domain.NodeRun{
			ID:            domain.NodeRunID(nextNodeRunID),
			WorkflowRunID: run.ID,
			NodeID:        node.ID,
			Status:        domain.NodeSucceeded,
			Attempt:       1,
			StartedAt:     &now,
			FinishedAt:    &now,
		}
		resultNodeRunID := sessiondomain.NodeRunID(nextNodeRun.ID)
		advance := sessiondomain.WorkflowAdvance{
			WorkflowRunID:    sessiondomain.WorkflowRunID(run.ID),
			NodeRunID:        &resultNodeRunID,
			CurrentNodeID:    node.ID,
			CurrentNodeTitle: node.Title,
			Status:           string(run.Status),
			Close:            true,
		}
		if err := s.saveWorkflowMutation(ctx, definition, run, workflowEventInputFromAdvance(advance), func(ctx context.Context, repo domain.Repository) error {
			return repo.CompleteNodeAndAdvance(ctx, completedNodeRun, run, &nextNodeRun)
		}); err != nil {
			return sessiondomain.WorkflowAdvance{}, err
		}
		return advance, nil
	}
	nextNodeRunID, err := s.generateID()
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, fmt.Errorf("generate workflow node run id: %w", err)
	}
	run.CurrentNodeID = node.ID
	run.Status = domain.RunRunning
	run.Context = contextForNextNode(run.Context)
	nodeStatus := domain.NodeRunning
	requiresCodex := true
	merge := mergeRequest(node)
	expr := exprRequest(node, paramsFromContext(run.Context))
	if requiresApproval(node) {
		run.Status = domain.RunWaitingApproval
		nodeStatus = domain.NodeWaitingApproval
		requiresCodex = false
	} else if merge != nil {
		requiresCodex = false
	} else if expr != nil {
		requiresCodex = false
	}
	nextNodeRun := domain.NodeRun{
		ID:            domain.NodeRunID(nextNodeRunID),
		WorkflowRunID: run.ID,
		NodeID:        node.ID,
		Status:        nodeStatus,
		Attempt:       1,
		StartedAt:     &now,
	}
	resultNodeRunID := sessiondomain.NodeRunID(nextNodeRun.ID)
	advance := sessiondomain.WorkflowAdvance{
		WorkflowRunID:    sessiondomain.WorkflowRunID(run.ID),
		NodeRunID:        &resultNodeRunID,
		CurrentNodeID:    node.ID,
		CurrentNodeTitle: node.Title,
		Status:           string(run.Status),
		RequiresCodex:    requiresCodex,
		Prompt:           nodePrompt("", node, paramsFromContext(run.Context)),
		Merge:            merge,
		Expr:             expr,
	}
	if run.Status == domain.RunWaitingApproval {
		advance.ApprovalPhase = "before_run"
		advance.Merge = nil
		advance.Expr = nil
	}
	if err := s.saveWorkflowMutation(ctx, definition, run, workflowEventInputFromAdvance(advance), func(ctx context.Context, repo domain.Repository) error {
		return repo.CompleteNodeAndAdvance(ctx, completedNodeRun, run, &nextNodeRun)
	}); err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	return advance, nil
}

func (s *Service) failNode(ctx context.Context, run domain.Run, nodeRunID domain.NodeRunID, failure domain.NodeFailure, output map[string]any) (sessiondomain.WorkflowAdvance, error) {
	definition, err := s.loadDefinition(ctx, run.WorkflowDefinitionID)
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	node, err := findNode(definition.Graph, run.CurrentNodeID)
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	nodeRun, err := s.repo.FindLatestNodeRun(ctx, run.ID, run.CurrentNodeID)
	if err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	if nodeRun.ID != nodeRunID {
		return sessiondomain.WorkflowAdvance{}, fmt.Errorf("workflow run is on node run %q, not %q", nodeRun.ID, nodeRunID)
	}
	now := s.now()
	failureResult := failureNodeResult(failure, output)
	failedNodeRun := domain.NodeRun{
		ID:            nodeRun.ID,
		WorkflowRunID: run.ID,
		NodeID:        nodeRun.NodeID,
		Status:        domain.NodeFailed,
		Attempt:       nodeRun.Attempt,
		FinishedAt:    &now,
		Result:        failureResult,
	}
	contextValue := contextAfterFailedNode(run.Context, failureResult)
	planner := domain.DefaultPlanner{}
	run.Context = contextValue
	if planner.ShouldRetry(node, nodeRun.Attempt, failure) {
		nextNodeRun, advance, err := s.nextNodeRunForNode(&run, node, nodeRun.Attempt+1, now, false, false)
		if err != nil {
			return sessiondomain.WorkflowAdvance{}, err
		}
		if err := s.saveWorkflowMutation(ctx, definition, run, workflowEventInputFromAdvance(advance), func(ctx context.Context, repo domain.Repository) error {
			return repo.CompleteNodeAndAdvance(ctx, failedNodeRun, run, &nextNodeRun)
		}); err != nil {
			return sessiondomain.WorkflowAdvance{}, err
		}
		return advance, nil
	}
	reason := strings.TrimSpace(failure.Message)
	if reason == "" {
		reason = "workflow node failed"
	}
	run.Status = domain.RunBlocked
	run.Context.Values["blockedReason"] = reason
	run.Context.Values["blockedFailure"] = map[string]any{
		"code":    failure.Code,
		"message": failure.Message,
	}
	advance := sessiondomain.WorkflowAdvance{
		WorkflowRunID:  sessiondomain.WorkflowRunID(run.ID),
		Status:         string(run.Status),
		Blocked:        true,
		BlockedReason:  reason,
		BlockedCode:    failure.Code,
		BlockedMessage: failure.Message,
	}
	if err := s.saveWorkflowMutation(ctx, definition, run, workflowEventInputFromAdvance(advance), func(ctx context.Context, repo domain.Repository) error {
		return repo.CompleteNodeAndAdvance(ctx, failedNodeRun, run, nil)
	}); err != nil {
		return sessiondomain.WorkflowAdvance{}, err
	}
	return advance, nil
}

func failureNodeResult(failure domain.NodeFailure, output map[string]any) *domain.Result {
	data := map[string]any{
		"failure": map[string]any{"code": failure.Code, "message": failure.Message},
	}
	if raw, ok := output["results"].(map[string]any); ok {
		if nested, ok := raw["data"].(map[string]any); ok {
			for key, value := range nested {
				data[key] = value
			}
		}
	}
	summary := strings.TrimSpace(failure.Message)
	if summary == "" {
		summary = "Workflow node failed"
	}
	return &domain.Result{Version: domain.ResultVersion, Outcome: domain.ResultFailure, Summary: summary, Data: data}
}

func (s *Service) nextNodeRunForNode(run *domain.Run, node domain.Node, attempt int, now time.Time, updateCurrentNode bool, skipBeforeApproval bool) (domain.NodeRun, sessiondomain.WorkflowAdvance, error) {
	nextNodeRunID, err := s.generateID()
	if err != nil {
		return domain.NodeRun{}, sessiondomain.WorkflowAdvance{}, fmt.Errorf("generate workflow node run id: %w", err)
	}
	if updateCurrentNode {
		run.CurrentNodeID = node.ID
		run.Context = contextForNextNode(run.Context)
	}
	if isCloseNode(node) && (!requiresApproval(node) || skipBeforeApproval) {
		run.Status = domain.RunCompleted
		run.StoppedAt = &now
		nextNodeRun := domain.NodeRun{
			ID:            domain.NodeRunID(nextNodeRunID),
			WorkflowRunID: run.ID,
			NodeID:        node.ID,
			Status:        domain.NodeSucceeded,
			Attempt:       attempt,
			StartedAt:     &now,
			FinishedAt:    &now,
		}
		resultNodeRunID := sessiondomain.NodeRunID(nextNodeRun.ID)
		return nextNodeRun, sessiondomain.WorkflowAdvance{
			WorkflowRunID:    sessiondomain.WorkflowRunID(run.ID),
			NodeRunID:        &resultNodeRunID,
			CurrentNodeID:    node.ID,
			CurrentNodeTitle: node.Title,
			Status:           string(run.Status),
			Close:            true,
		}, nil
	}
	run.Status = domain.RunRunning
	nodeStatus := domain.NodeRunning
	requiresCodex := true
	merge := mergeRequest(node)
	expr := exprRequest(node, paramsFromContext(run.Context))
	if requiresApproval(node) && !skipBeforeApproval {
		run.Status = domain.RunWaitingApproval
		nodeStatus = domain.NodeWaitingApproval
		requiresCodex = false
	} else if merge != nil {
		requiresCodex = false
	} else if expr != nil {
		requiresCodex = false
	}
	nextNodeRun := domain.NodeRun{
		ID:            domain.NodeRunID(nextNodeRunID),
		WorkflowRunID: run.ID,
		NodeID:        node.ID,
		Status:        nodeStatus,
		Attempt:       attempt,
		StartedAt:     &now,
	}
	resultNodeRunID := sessiondomain.NodeRunID(nextNodeRun.ID)
	advance := sessiondomain.WorkflowAdvance{
		WorkflowRunID:    sessiondomain.WorkflowRunID(run.ID),
		NodeRunID:        &resultNodeRunID,
		CurrentNodeID:    node.ID,
		CurrentNodeTitle: node.Title,
		Status:           string(run.Status),
		RequiresCodex:    requiresCodex,
		Prompt:           nodePrompt("", node, paramsFromContext(run.Context)),
		Merge:            merge,
		Expr:             expr,
	}
	if run.Status == domain.RunWaitingApproval {
		advance.ApprovalPhase = "before_run"
		advance.Merge = nil
		advance.Expr = nil
	}
	return nextNodeRun, advance, nil
}

type workflowEventInput struct {
	eventType string
	payload   map[string]any
}

func workflowEventInputFromStart(start sessiondomain.WorkflowStart) workflowEventInput {
	advance := sessiondomain.WorkflowAdvance{
		WorkflowRunID:      start.WorkflowRunID,
		NodeRunID:          start.NodeRunID,
		CurrentNodeID:      start.CurrentNodeID,
		CurrentNodeTitle:   start.CurrentNodeTitle,
		Status:             start.Status,
		RequiresCodex:      start.RequiresCodex,
		RequireResultRetry: start.RequireResultRetry,
		ApprovalPhase:      start.ApprovalPhase,
		Result:             start.Result,
		Merge:              start.Merge,
		Expr:               start.Expr,
		Close:              start.Close,
	}
	return workflowEventInputFromAdvance(advance)
}

func workflowEventInputFromAdvance(advance sessiondomain.WorkflowAdvance) workflowEventInput {
	payload := map[string]any{
		"workflowRunId": string(advance.WorkflowRunID),
		"status":        advance.Status,
	}
	if advance.NodeRunID != nil {
		payload["nodeRunId"] = string(*advance.NodeRunID)
	}
	if strings.TrimSpace(advance.CurrentNodeID) != "" {
		payload["currentNodeId"] = advance.CurrentNodeID
	}
	if strings.TrimSpace(advance.CurrentNodeTitle) != "" {
		payload["currentNodeTitle"] = advance.CurrentNodeTitle
	}
	switch {
	case advance.Blocked:
		payload["reason"] = advance.BlockedReason
		if strings.TrimSpace(advance.BlockedCode) != "" {
			payload["failureCode"] = advance.BlockedCode
		}
		if strings.TrimSpace(advance.BlockedMessage) != "" {
			payload["failureMessage"] = advance.BlockedMessage
		}
		return workflowEventInput{eventType: "workflow.blocked", payload: payload}
	case advance.Close:
		return workflowEventInput{eventType: "workflow.closed", payload: payload}
	case advance.Completed:
		return workflowEventInput{eventType: "workflow.completed", payload: payload}
	case advance.Merge != nil:
		payload["strategy"] = advance.Merge.Strategy
		return workflowEventInput{eventType: "workflow.merge_ready", payload: payload}
	case advance.Expr != nil:
		return workflowEventInput{eventType: "workflow.expr_ready", payload: payload}
	case !advance.RequiresCodex:
		payload["approvalPhase"] = advance.ApprovalPhase
		payload["result"] = advance.Result
		return workflowEventInput{eventType: "workflow.waiting_approval", payload: payload}
	default:
		return workflowEventInput{eventType: "workflow.started", payload: payload}
	}
}

func (s *Service) saveWorkflowMutation(ctx context.Context, definition domain.Definition, run domain.Run, eventInput workflowEventInput, mutate func(context.Context, domain.Repository) error) error {
	event, ok, err := s.newWorkflowEvent(run, definition, eventInput)
	if err != nil {
		return err
	}
	if s.uow != nil && s.events != nil {
		if err := s.uow.Do(ctx, func(ctx context.Context, tx port.Tx) error {
			if err := mutate(ctx, tx.Workflows()); err != nil {
				return err
			}
			if ok {
				if err := tx.Events().Append(ctx, event); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		if ok {
			s.publishWorkflowEvent(ctx, event)
		}
		return nil
	}
	if err := mutate(ctx, s.repo); err != nil {
		return err
	}
	if ok {
		if err := s.events.Append(ctx, event); err != nil {
			return err
		}
		s.publishWorkflowEvent(ctx, event)
	}
	return nil
}

func (s *Service) newWorkflowEvent(run domain.Run, definition domain.Definition, input workflowEventInput) (eventdomain.DomainEvent, bool, error) {
	if s.events == nil || strings.TrimSpace(input.eventType) == "" {
		return eventdomain.DomainEvent{}, false, nil
	}
	id, err := s.generateID()
	if err != nil {
		return eventdomain.DomainEvent{}, false, err
	}
	sessionID := eventdomain.SessionID(run.SessionID)
	payload := map[string]any{}
	for key, value := range input.payload {
		payload[key] = value
	}
	payload["status"] = string(run.Status)
	return eventdomain.DomainEvent{
		ID: eventdomain.ID(id),
		Scope: eventdomain.Scope{
			ProjectID: string(definition.ProjectID),
			SessionID: &sessionID,
		},
		SessionID: &sessionID,
		Type:      input.eventType,
		Payload:   payload,
		CreatedAt: s.now(),
	}, true, nil
}

func (s *Service) publishWorkflowEvent(ctx context.Context, event eventdomain.DomainEvent) {
	if s.publisher == nil {
		return
	}
	_ = s.publisher.PublishAfterCommit(ctx, event)
}

type workflowEventRecorder struct {
	store  eventdomain.Store
	events []eventdomain.DomainEvent
}

func (r *workflowEventRecorder) Append(ctx context.Context, event eventdomain.DomainEvent) error {
	if r.store != nil {
		if err := r.store.Append(ctx, event); err != nil {
			return err
		}
	}
	r.events = append(r.events, event)
	return nil
}

func (r *workflowEventRecorder) List(ctx context.Context, scope eventdomain.Scope) ([]eventdomain.DomainEvent, error) {
	if r.store == nil {
		return nil, errors.New("workflow event store is required")
	}
	return r.store.List(ctx, scope)
}

func (r *workflowEventRecorder) After(ctx context.Context, scope eventdomain.Scope, after eventdomain.ID) ([]eventdomain.DomainEvent, error) {
	if r.store == nil {
		return nil, errors.New("workflow event store is required")
	}
	return r.store.After(ctx, scope, after)
}

func (r *workflowEventRecorder) Before(ctx context.Context, scope eventdomain.Scope, before eventdomain.ID, limit int) ([]eventdomain.DomainEvent, int, bool, error) {
	if r.store == nil {
		return nil, 0, false, errors.New("workflow event store is required")
	}
	return r.store.Before(ctx, scope, before, limit)
}

func stringValuePtr(value *sessiondomain.NodeRunID) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func (s *Service) definitionForStart(ctx context.Context, input sessiondomain.WorkflowStartInput) (domain.Definition, error) {
	if input.ProjectID == "" {
		return domain.Definition{}, errors.New("project id is required")
	}
	if input.SessionID == "" {
		return domain.Definition{}, errors.New("session id is required")
	}
	if input.WorkflowDefinitionID != "" {
		definition, err := s.loadDefinition(ctx, domain.DefinitionID(input.WorkflowDefinitionID))
		if err != nil {
			return domain.Definition{}, err
		}
		if definition.ProjectID != domain.ProjectID(input.ProjectID) {
			return domain.Definition{}, errors.New("workflow definition does not belong to project")
		}
		return definition, nil
	}
	return s.loadActiveDefinition(ctx, domain.ProjectID(input.ProjectID))
}

func (s *Service) loadDefinition(ctx context.Context, id domain.DefinitionID) (domain.Definition, error) {
	definition, err := s.repo.FindDefinition(ctx, id)
	if err != nil {
		return domain.Definition{}, err
	}
	return canonicalDefinition(definition)
}

func (s *Service) loadActiveDefinition(ctx context.Context, projectID domain.ProjectID) (domain.Definition, error) {
	definition, err := s.repo.FindActive(ctx, projectID)
	if err != nil {
		return domain.Definition{}, err
	}
	return canonicalDefinition(definition)
}

func canonicalDefinition(definition domain.Definition) (domain.Definition, error) {
	definition.Graph = domain.CanonicalGraph(definition.Graph)
	if err := validateGraph(definition.Graph); err != nil {
		return domain.Definition{}, workflowValidationError(err.Error())
	}
	return definition, nil
}

func firstNode(graph domain.Graph) (domain.Node, error) {
	if len(graph.Nodes) == 0 {
		return domain.Node{}, errors.New("workflow graph has no nodes")
	}
	hasIncoming := map[string]bool{}
	for _, edge := range graph.Edges {
		hasIncoming[edge.To] = true
	}
	startNodes := make([]domain.Node, 0, 1)
	for _, node := range graph.Nodes {
		if !hasIncoming[node.ID] {
			startNodes = append(startNodes, node)
		}
	}
	if len(startNodes) != 1 {
		return domain.Node{}, fmt.Errorf("workflow graph must have exactly one start node, got %d", len(startNodes))
	}
	return startNodes[0], nil
}

func findNode(graph domain.Graph, id string) (domain.Node, error) {
	for _, node := range graph.Nodes {
		if node.ID == id {
			return node, nil
		}
	}
	return domain.Node{}, fmt.Errorf("workflow node %q was not found", id)
}

func contextAfterNode(contextValue domain.Context, result *domain.Result) domain.Context {
	values := map[string]any{}
	for key, value := range contextValue.Values {
		values[key] = value
	}
	results := resultMap(result)
	values["results"] = results
	values["last"] = map[string]any{
		"status": "succeeded",
		"output": results,
	}
	return domain.Context{Values: values}
}

func contextAfterFailedNode(contextValue domain.Context, result *domain.Result) domain.Context {
	values := map[string]any{}
	for key, value := range contextValue.Values {
		values[key] = value
	}
	results := resultMap(result)
	values["results"] = results
	values["last"] = map[string]any{
		"status": "failed",
		"output": results,
	}
	return domain.Context{Values: values}
}

func contextWithApproval(contextValue domain.Context, approval map[string]any) domain.Context {
	values := map[string]any{}
	for key, value := range contextValue.Values {
		values[key] = value
	}
	values["approval"] = approval
	return domain.Context{Values: values}
}

func contextForNextNode(contextValue domain.Context) domain.Context {
	values := map[string]any{}
	for key, value := range contextValue.Values {
		values[key] = value
	}
	values["params"] = paramsFromResults(values)
	delete(values, "approval")
	return domain.Context{Values: values}
}

func paramsFromResults(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	if results, ok := values["results"].(map[string]any); ok && results != nil {
		return copyMap(results)
	}
	return map[string]any{}
}

func paramsFromContext(contextValue domain.Context) map[string]any {
	if params, ok := contextValue.Values["params"].(map[string]any); ok && params != nil {
		return copyMap(params)
	}
	return map[string]any{}
}

func copyMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func contextAfterResumeFailure(contextValue domain.Context, code string, message string) domain.Context {
	values := map[string]any{}
	for key, value := range contextValue.Values {
		values[key] = value
	}
	values["resume"] = map[string]any{
		"status":  "failed",
		"code":    code,
		"message": message,
	}
	return domain.Context{Values: values}
}

func contextAfterResumeRerun(contextValue domain.Context, reason string) domain.Context {
	values := map[string]any{}
	for key, value := range contextValue.Values {
		values[key] = value
	}
	values["resume"] = map[string]any{
		"status": "rerun_requested",
		"reason": reason,
	}
	return domain.Context{Values: values}
}

func contextAfterResumeRetry(contextValue domain.Context, reason string) domain.Context {
	values := map[string]any{}
	for key, value := range contextValue.Values {
		values[key] = value
	}
	values["resume"] = map[string]any{
		"status": "retry_requested",
		"reason": reason,
	}
	return domain.Context{Values: values}
}

func payloadOrEmpty(payload map[string]any) map[string]any {
	if payload == nil {
		return map[string]any{}
	}
	return payload
}

func approvalValue(approved bool, comment string) map[string]any {
	return map[string]any{
		"approved": approved,
		"comment":  strings.TrimSpace(comment),
	}
}

func isAfterRunApproval(node domain.Node, nodeRun domain.NodeRun) bool {
	return node.Approval.AfterRun && (nodeRun.Result != nil || !requiresApproval(node))
}

func isBeforeRunApproval(node domain.Node, nodeRun domain.NodeRun) bool {
	if isApprovalNode(node) {
		return false
	}
	return node.Approval.BeforeRun && nodeRun.Result == nil
}

func requiresApproval(node domain.Node) bool {
	nodeType := strings.TrimSpace(strings.ToLower(node.Type))
	return node.Approval.BeforeRun || nodeType == "approval" || nodeType == "manual_approval"
}

func isApprovalNode(node domain.Node) bool {
	nodeType := strings.TrimSpace(strings.ToLower(node.Type))
	return nodeType == "approval" || nodeType == "manual_approval"
}

func exprRequest(node domain.Node, params map[string]any) *sessiondomain.WorkflowExpr {
	if !isExprNode(node) {
		return nil
	}
	return &sessiondomain.WorkflowExpr{
		Script: strings.TrimSpace(node.Prompt),
		Params: payloadOrEmpty(params),
	}
}

func isExprNode(node domain.Node) bool {
	return strings.TrimSpace(strings.ToLower(node.Type)) == "expr"
}

func isCloseNode(node domain.Node) bool {
	nodeType := strings.TrimSpace(strings.ToLower(node.Type))
	return nodeType == "close"
}

func isCodexNode(node domain.Node) bool {
	nodeType := strings.TrimSpace(strings.ToLower(node.Type))
	return nodeType == "" || nodeType == "codex"
}

func mergeRequest(node domain.Node) *sessiondomain.WorkflowMerge {
	nodeType := strings.TrimSpace(strings.ToLower(node.Type))
	if node.Merge == nil && nodeType != "merge" {
		return nil
	}
	strategy := "merge"
	if node.Merge != nil && strings.TrimSpace(node.Merge.Strategy) != "" {
		strategy = strings.TrimSpace(strings.ToLower(node.Merge.Strategy))
	}
	if strategy != "rebase" {
		strategy = "merge"
	}
	return &sessiondomain.WorkflowMerge{Strategy: strategy}
}

func boolOutputValue(output map[string]any, key string) bool {
	value, ok := output[key].(bool)
	return ok && value
}

func resultRetryPrompt(node domain.Node, reason error) string {
	var builder strings.Builder
	builder.WriteString("ANYCODE_WORKFLOW_RESULT_RETRY\n")
	builder.WriteString("Your previous response did not satisfy the workflow result protocol: ")
	builder.WriteString(reason.Error())
	builder.WriteString("\nIf any question, uncertainty, or user decision within this node's task remains, call `answer_user` now and wait for the answer before finishing.\n")
	builder.WriteString(workflowResultContract(node))
	if len(node.OutputFields) > 0 {
		builder.WriteString("\n\nRequired `results.data` fields:\n")
		builder.WriteString(outputFieldList(node.OutputFields))
	}
	return builder.String()
}

func nodePrompt(requirement string, node domain.Node, params map[string]any) string {
	prompt := strings.TrimSpace(node.Prompt)
	requirement = strings.TrimSpace(requirement)
	sections := make([]string, 0, 4)
	if prompt != "" {
		sections = append(sections, prompt)
	}
	if requirement != "" {
		sections = append(sections, "User requirement:\n"+requirement)
	}
	sections = append(sections, "Workflow input params JSON:\n"+jsonBlock(payloadOrEmpty(params)))
	if isCodexNode(node) {
		contract := workflowResultContract(node)
		if len(node.OutputFields) > 0 {
			contract += "\n\nRequired `results.data` fields:\n" + outputFieldList(node.OutputFields)
		}
		sections = append(sections, contract)
	}
	if len(sections) == 0 {
		return ""
	}
	return strings.Join(sections, "\n\n")
}

func jsonBlock(payload map[string]any) string {
	data, err := json.MarshalIndent(payloadOrEmpty(payload), "", "  ")
	if err != nil {
		return "```json\n{}\n```"
	}
	return "```json\n" + string(data) + "\n```"
}

func outputFieldList(fields []domain.OutputField) string {
	lines := make([]string, 0, len(fields))
	for _, field := range fields {
		valueType := strings.TrimSpace(field.ValueType)
		if valueType == "" {
			valueType = "any"
		}
		description := strings.TrimSpace(field.Description)
		if description == "" {
			description = "-"
		}
		lines = append(lines, fmt.Sprintf("- `%s` (%s): %s", field.Key, valueType, description))
	}
	return strings.Join(lines, "\n")
}

func workflowResultContract(node domain.Node) string {
	return "Workflow result contract:\nBefore finishing, resolve every question, uncertainty, or user decision within this node's task through `answer_user`. Do not describe pending questions in the final result. Workflow-managed before-run or after-run approval is handled by the runner after you return `results`; do not call `answer_user` merely to obtain that approval.\nReturn only one JSON object with exactly this shape and no extra fields:\n" +
		`{"results":{"version":1,"outcome":"success|partial|failure","summary":"concise review summary","data":{},"checks":[{"id":"...","label":"...","status":"passed|warning|failed","detail":"...","source":"agent"}],"warnings":[{"code":"...","message":"..."}],"artifacts":[{"kind":"...","label":"...","ref":"..."}]}}` +
		"\nThe workflow owns human approval. Never return `approval`, `approved`, `questions`, `pendingQuestions`, or `needs_input` in `results`."
}

func decodeWorkflowResult(output map[string]any, node domain.Node) (*domain.Result, error) {
	raw, ok := output["results"]
	if !ok {
		return nil, errors.New("top-level results object is required")
	}
	rawMap, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("top-level results must be an object")
	}
	for _, key := range []string{"version", "outcome", "summary", "data", "checks", "warnings", "artifacts"} {
		if _, exists := rawMap[key]; !exists {
			return nil, fmt.Errorf("results.%s is required", key)
		}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("encode workflow result: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var result domain.Result
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("decode workflow result: %w", err)
	}
	result.Normalize()
	if err := result.Validate(); err != nil {
		return nil, err
	}
	source := "system"
	if isCodexNode(node) {
		source = "agent"
	}
	for index := range result.Checks {
		result.Checks[index].Source = source
	}
	if err := validateResultData(result.Data, node.OutputFields); err != nil {
		return nil, err
	}
	return &result, nil
}

func validateResultData(data map[string]any, fields []domain.OutputField) error {
	for _, field := range fields {
		value, ok := lookupResultField(data, strings.TrimSpace(field.Key))
		if !ok {
			return fmt.Errorf("results.data.%s is required", field.Key)
		}
		if !matchesResultType(value, strings.TrimSpace(field.ValueType)) {
			return fmt.Errorf("results.data.%s must be %s", field.Key, field.ValueType)
		}
	}
	return nil
}

func lookupResultField(data map[string]any, key string) (any, bool) {
	var current any = data
	for _, part := range strings.Split(key, ".") {
		mapped, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = mapped[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func matchesResultType(value any, valueType string) bool {
	switch valueType {
	case "", "any":
		return true
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		switch value.(type) {
		case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			return true
		default:
			return false
		}
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	default:
		return false
	}
}

func resultMap(result *domain.Result) map[string]any {
	if result == nil {
		return map[string]any{}
	}
	canonical := *result
	canonical.Normalize()
	data, err := json.Marshal(canonical)
	if err != nil {
		return map[string]any{}
	}
	var mapped map[string]any
	if json.Unmarshal(data, &mapped) != nil {
		return map[string]any{}
	}
	return mapped
}

func toDefinitionDTO(definition domain.Definition) DefinitionDTO {
	return DefinitionDTO{
		ID:        definition.ID,
		ProjectID: definition.ProjectID,
		Name:      definition.Name,
		Version:   definition.Version,
		Graph:     definition.Graph,
		Active:    definition.Active,
	}
}

func toRunDTO(run domain.Run) RunDTO {
	return RunDTO{
		ID:            run.ID,
		SessionID:     run.SessionID,
		Status:        run.Status,
		CurrentNodeID: run.CurrentNodeID,
		Context:       run.Context,
	}
}

func toSessionWorkflowRunSnapshot(run domain.Run) sessiondomain.WorkflowRunSnapshot {
	values := map[string]any{}
	for key, value := range run.Context.Values {
		values[key] = value
	}
	return sessiondomain.WorkflowRunSnapshot{
		ID:            sessiondomain.WorkflowRunID(run.ID),
		SessionID:     sessiondomain.ID(run.SessionID),
		Status:        string(run.Status),
		CurrentNodeID: run.CurrentNodeID,
		Context:       values,
	}
}

func generateID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

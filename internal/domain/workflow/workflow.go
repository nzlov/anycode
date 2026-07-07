package workflow

import (
	"context"
	"time"
)

type DefinitionID string
type RunID string
type NodeRunID string
type ProjectID string
type SessionID string
type ProcessRunID string

type Definition struct {
	ID        DefinitionID
	ProjectID ProjectID
	Name      string
	Version   int
	Graph     Graph
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Graph struct {
	Nodes []Node
	Edges []Edge
}

type Node struct {
	ID           string
	Type         string
	Title        string
	Prompt       string
	Position     Position
	OutputFields []OutputField
	Approval     ApprovalConfig
	Retry        RetryConfig
	Merge        *MergeConfig
}

type Position struct {
	X float64
	Y float64
}

type OutputField struct {
	Key         string
	Description string
	ValueType   string
}

type ApprovalConfig struct {
	BeforeRun bool
	AfterRun  bool
}

type RetryConfig struct {
	MaxAttempts int
}

type MergeConfig struct {
	Strategy string
}

type Edge struct {
	From      string
	To        string
	Priority  int
	Condition Condition
}

type Condition struct {
	Mode  string
	Field string
	Op    string
	Value any
	Expr  string
	All   []Condition
	Any   []Condition
	Not   *Condition
}

type RunStatus string

const (
	RunCreated             RunStatus = "created"
	RunRunning             RunStatus = "running"
	RunWaitingApproval     RunStatus = "waiting_approval"
	RunWaitingResumeAction RunStatus = "waiting_resume_action"
	RunBlocked             RunStatus = "blocked"
	RunFailed              RunStatus = "failed"
	RunCompleted           RunStatus = "completed"
	RunStopped             RunStatus = "stopped"
)

type Run struct {
	ID                   RunID
	SessionID            SessionID
	WorkflowDefinitionID DefinitionID
	Status               RunStatus
	CurrentNodeID        string
	Context              Context
	StartedAt            *time.Time
	StoppedAt            *time.Time
}

type NodeRunStatus string

const (
	NodePending         NodeRunStatus = "pending"
	NodeRunning         NodeRunStatus = "running"
	NodeWaitingApproval NodeRunStatus = "waiting_approval"
	NodeWaitingUser     NodeRunStatus = "waiting_user"
	NodeSucceeded       NodeRunStatus = "succeeded"
	NodeFailed          NodeRunStatus = "failed"
	NodeCanceled        NodeRunStatus = "canceled"
)

type NodeRun struct {
	ID            NodeRunID
	WorkflowRunID RunID
	NodeID        string
	Status        NodeRunStatus
	Attempt       int
	ProcessRunID  *ProcessRunID
	StartedAt     *time.Time
	FinishedAt    *time.Time
	Output        map[string]any
}

type Context struct {
	Values map[string]any
}

type NodeFailure struct {
	Code    string
	Message string
}

type NodeDecision struct {
	NextNodeID string
	Blocked    bool
	Reason     string
}

type Planner interface {
	NextNode(def Definition, run Run, context Context) (NodeDecision, error)
	ShouldRetry(node Node, attempts int, failure NodeFailure) bool
}

type ConditionEvaluator interface {
	Evaluate(condition Condition, context Context) (bool, error)
}

type Repository interface {
	SaveDefinition(ctx context.Context, definition Definition) error
	FindDefinition(ctx context.Context, id DefinitionID) (Definition, error)
	FindActive(ctx context.Context, projectID ProjectID) (Definition, error)
	FindRun(ctx context.Context, id RunID) (Run, error)
	FindLatestRunBySession(ctx context.Context, sessionID SessionID) (Run, error)
	FindLatestNodeRun(ctx context.Context, runID RunID, nodeID string) (NodeRun, error)
	ActivateDefinition(ctx context.Context, id DefinitionID) error
	CreateInitialRun(ctx context.Context, run Run, nodeRun NodeRun) error
	CreateRun(ctx context.Context, run Run) error
	UpdateRunState(ctx context.Context, run Run) error
	SaveNodeRun(ctx context.Context, run NodeRun) error
	CreateNodeRunAndUpdateRun(ctx context.Context, run Run, nodeRun NodeRun) error
	CompleteNodeAndAdvance(ctx context.Context, completedNodeRun NodeRun, run Run, nextNodeRun *NodeRun) error
	MarkRunFailed(ctx context.Context, runID RunID, nodeRunID NodeRunID, failure NodeFailure, finishedAt time.Time) error
	UpdateRunContext(ctx context.Context, id RunID, context Context) error
}

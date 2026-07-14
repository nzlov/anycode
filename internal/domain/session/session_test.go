package session

import (
	"errors"
	"testing"
	"time"
)

func TestQueueExecutionOwnsQueueState(t *testing.T) {
	now := time.Unix(20, 0).UTC()
	session := Session{Status: StatusStopped}
	intent := QueueIntent{Kind: QueueKindResume, Priority: QueuePriorityHigh, Prompt: "continue"}

	if err := session.QueueExecution(intent, now); err != nil {
		t.Fatalf("QueueExecution() error = %v", err)
	}
	if session.Status != StatusQueued || session.Queue != intent {
		t.Fatalf("queued session = %#v", session)
	}
	if session.QueuedAt == nil || !session.QueuedAt.Equal(now) || session.LastRunAt == nil || !session.LastRunAt.Equal(now) {
		t.Fatalf("queue timestamps = queuedAt=%v lastRunAt=%v", session.QueuedAt, session.LastRunAt)
	}
}

func TestInterruptedStartingSessionCanReturnToResumeQueue(t *testing.T) {
	now := time.Unix(25, 0).UTC()
	session := Session{Status: StatusStarting}
	intent := QueueIntent{Kind: QueueKindResume, Priority: QueuePriorityMedium, ResumeCodexSessionID: "codex-1"}

	if err := session.QueueExecution(intent, now); err != nil {
		t.Fatalf("QueueExecution() error = %v", err)
	}
	if session.Status != StatusQueued || session.Queue != intent {
		t.Fatalf("queued interrupted session = %#v", session)
	}
}

func TestInterruptedAnswerQueueCanReturnToWaitingUser(t *testing.T) {
	now := time.Unix(26, 0).UTC()
	session := Session{
		Status:   StatusQueued,
		Queue:    QueueIntent{Kind: QueueKindAnswerUser, Priority: QueuePriorityImmediate},
		QueuedAt: timePtr(time.Unix(25, 0).UTC()),
	}

	if err := session.TransitionTo(StatusWaitingUser, now); err != nil {
		t.Fatalf("TransitionTo() error = %v", err)
	}
	if session.Status != StatusWaitingUser || session.Queue.Kind != "" || session.QueuedAt != nil {
		t.Fatalf("restored waiting-user session = %#v", session)
	}
}

func TestRegularQueueCannotTransitionToWaitingUser(t *testing.T) {
	session := Session{
		Status: StatusQueued,
		Queue:  QueueIntent{Kind: QueueKindStart, Priority: QueuePriorityMedium},
	}

	err := session.TransitionTo(StatusWaitingUser, time.Unix(27, 0).UTC())
	if !errors.Is(err, ErrInvalidStatusTransition) {
		t.Fatalf("TransitionTo() error = %v", err)
	}
}

func TestTransitionClearsQueueWhenExecutionLeavesQueue(t *testing.T) {
	now := time.Unix(30, 0).UTC()
	queuedAt := time.Unix(20, 0).UTC()
	session := Session{
		Status:   StatusQueued,
		QueuedAt: &queuedAt,
		Queue:    QueueIntent{Kind: QueueKindStart, Priority: QueuePriorityMedium},
	}

	if err := session.TransitionTo(StatusStarting, now); err != nil {
		t.Fatalf("TransitionTo() error = %v", err)
	}
	if session.Status != StatusStarting || session.QueuedAt != nil || session.Queue != (QueueIntent{}) {
		t.Fatalf("started session = %#v", session)
	}
	if session.LastRunAt == nil || !session.LastRunAt.Equal(now) {
		t.Fatalf("last run at = %v", session.LastRunAt)
	}
}

func TestMatchesLifecycleSnapshotComparesIntentValues(t *testing.T) {
	nodeRunID := NodeRunID("node-run-1")
	queuedAt := time.Unix(20, 0).UTC()
	reason := CloseReasonUserClosed
	current := Session{
		ID: "session-1", Status: StatusStopping, UpdatedAt: time.Unix(30, 0).UTC(), QueuedAt: &queuedAt, CloseReason: &reason,
		Queue: QueueIntent{Kind: QueueKindResume, Priority: QueuePriorityHigh, NodeRunID: &nodeRunID, Prompt: "continue"},
	}
	otherNodeRunID := NodeRunID("node-run-1")
	otherQueuedAt := queuedAt
	otherReason := reason
	expected := current
	expected.Queue.NodeRunID = &otherNodeRunID
	expected.QueuedAt = &otherQueuedAt
	expected.CloseReason = &otherReason
	if !current.MatchesLifecycleSnapshot(expected) {
		t.Fatal("equal lifecycle values should match despite distinct pointers")
	}
	expected.CloseReason = nil
	if current.MatchesLifecycleSnapshot(expected) {
		t.Fatal("different close intent should not match")
	}
}

func TestTransitionRejectsInvalidLifecycleMove(t *testing.T) {
	session := Session{Status: StatusClosed}

	err := session.TransitionTo(StatusRunning, time.Unix(40, 0).UTC())
	if !errors.Is(err, ErrInvalidStatusTransition) {
		t.Fatalf("TransitionTo() error = %v", err)
	}
}

func TestTransitionToQueuedRequiresQueueIntent(t *testing.T) {
	session := Session{Status: StatusStopped}

	err := session.TransitionTo(StatusQueued, time.Unix(50, 0).UTC())
	if !errors.Is(err, ErrInvalidQueueIntent) {
		t.Fatalf("TransitionTo() error = %v", err)
	}
}

func TestRunnableWorkflowCardsCanMoveDirectlyToWorkflowOutcomes(t *testing.T) {
	for _, current := range []Status{StatusCreated, StatusStopped, StatusFailed, StatusCompleted, StatusResumeFailed} {
		for _, next := range []Status{StatusWaitingApproval, StatusBlocked, StatusCompleted} {
			t.Run(string(current)+"_to_"+string(next), func(t *testing.T) {
				session := Session{Status: current}
				if err := session.TransitionTo(next, time.Unix(55, 0).UTC()); err != nil {
					t.Fatalf("TransitionTo() error = %v", err)
				}
			})
		}
	}
}

func TestInterruptedAndWorkflowWaitingStatesCanReachRecoveryOutcomes(t *testing.T) {
	tests := []struct {
		current Status
		next    Status
	}{
		{current: StatusWaitingUser, next: StatusResumeFailed},
		{current: StatusStopping, next: StatusResumeFailed},
		{current: StatusWaitingUser, next: StatusWaitingApproval},
	}

	for _, tt := range tests {
		t.Run(string(tt.current)+"_to_"+string(tt.next), func(t *testing.T) {
			session := Session{Status: tt.current}
			if err := session.TransitionTo(tt.next, time.Unix(57, 0).UTC()); err != nil {
				t.Fatalf("TransitionTo() error = %v", err)
			}
		})
	}
}

func TestCloseOwnsTerminalMetadata(t *testing.T) {
	now := time.Unix(60, 0).UTC()
	queuedAt := time.Unix(50, 0).UTC()
	session := Session{
		Status:   StatusQueued,
		QueuedAt: &queuedAt,
		Queue:    QueueIntent{Kind: QueueKindStart, Priority: QueuePriorityLow},
	}

	if err := session.Close(CloseReasonUserClosed, now); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if session.Status != StatusClosed || session.CloseReason == nil || *session.CloseReason != CloseReasonUserClosed {
		t.Fatalf("closed session = %#v", session)
	}
	if session.ClosedAt == nil || !session.ClosedAt.Equal(now) || session.QueuedAt != nil || session.Queue != (QueueIntent{}) {
		t.Fatalf("close metadata = closedAt=%v queuedAt=%v queue=%#v", session.ClosedAt, session.QueuedAt, session.Queue)
	}
}

func TestWorktreeCleanupLifecycle(t *testing.T) {
	now := time.Unix(70, 0).UTC()
	session := Session{ID: "session-1", BaseBranch: "main", WorktreeCleanup: WorktreeCleanup{Status: WorktreeCleanupNotApplicable}}

	if err := session.BeginWorktreeProvisioning("/data/worktrees/project-1/session-1", "session-1", "owner-token", now); err != nil {
		t.Fatalf("BeginWorktreeProvisioning() error = %v", err)
	}
	if err := session.ActivateWorktree(now.Add(time.Second)); err != nil {
		t.Fatalf("ActivateWorktree() error = %v", err)
	}
	if session.WorktreeCleanup.OwnershipConfirmedAt == nil || !session.WorktreeCleanup.OwnershipConfirmedAt.Equal(now.Add(time.Second)) {
		t.Fatalf("ownership confirmation = %#v", session.WorktreeCleanup.OwnershipConfirmedAt)
	}
	if err := session.RequestWorktreeCleanup(now.Add(2 * time.Second)); err != nil {
		t.Fatalf("RequestWorktreeCleanup() error = %v", err)
	}
	next := now.Add(time.Minute)
	if err := session.FailWorktreeCleanup("branch_checked_out", "branch is checked out", true, &next, now.Add(3*time.Second)); err != nil {
		t.Fatalf("FailWorktreeCleanup() error = %v", err)
	}
	if err := session.RequestWorktreeCleanup(now.Add(4 * time.Second)); err != nil {
		t.Fatalf("RequestWorktreeCleanup() retry error = %v", err)
	}
	if err := session.CompleteWorktreeCleanup(now.Add(5 * time.Second)); err != nil {
		t.Fatalf("CompleteWorktreeCleanup() error = %v", err)
	}
	if session.WorktreeCleanup.Status != WorktreeCleanupCleaned || session.WorktreeCleanup.Attempts != 2 || session.WorktreeCleanup.CompletedAt == nil {
		t.Fatalf("worktree cleanup = %#v", session.WorktreeCleanup)
	}
}

func TestConfirmWorktreeOwnershipWhileCleanupPending(t *testing.T) {
	now := time.Unix(75, 0).UTC()
	session := Session{
		ID:             "session-1",
		BaseBranch:     "main",
		WorktreePath:   "/data/worktrees/project-1/session-1",
		WorktreeBranch: "session-1",
		WorktreeCleanup: WorktreeCleanup{
			Status:         WorktreeCleanupPending,
			OwnershipToken: "owner-token",
		},
	}

	if err := session.ConfirmWorktreeOwnership(now); err != nil {
		t.Fatalf("ConfirmWorktreeOwnership() error = %v", err)
	}
	if session.WorktreeCleanup.OwnershipConfirmedAt == nil || !session.WorktreeCleanup.OwnershipConfirmedAt.Equal(now) {
		t.Fatalf("ownership confirmation = %#v", session.WorktreeCleanup.OwnershipConfirmedAt)
	}
}

func TestWorktreeCleanupRejectsUnownedBranch(t *testing.T) {
	session := Session{ID: "session-1", BaseBranch: "main", WorktreeCleanup: WorktreeCleanup{Status: WorktreeCleanupNotApplicable}}

	err := session.BeginWorktreeProvisioning("/data/worktrees/project-1/session-1", "merge-session-1", "owner-token", time.Unix(80, 0).UTC())
	if !errors.Is(err, ErrInvalidWorktreeCleanup) {
		t.Fatalf("BeginWorktreeProvisioning() error = %v", err)
	}
}

func TestRequireActiveWorktreeRejectsCleanupStates(t *testing.T) {
	for _, status := range []WorktreeCleanupStatus{WorktreeCleanupProvisioning, WorktreeCleanupPending, WorktreeCleanupFailed, WorktreeCleanupCleaned} {
		t.Run(string(status), func(t *testing.T) {
			session := Session{
				BaseBranch:      "main",
				WorktreePath:    "/data/worktrees/project-1/session-1",
				WorktreeBranch:  "session-1",
				WorktreeCleanup: WorktreeCleanup{Status: status},
			}
			if err := session.RequireActiveWorktree(); !errors.Is(err, ErrWorktreeUnavailable) {
				t.Fatalf("RequireActiveWorktree() error = %v", err)
			}
		})
	}
}

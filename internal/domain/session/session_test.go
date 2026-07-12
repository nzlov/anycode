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

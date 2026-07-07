package apperror

import (
	"errors"
	"fmt"

	"github.com/nzlov/anycode/internal/domain/redaction"
)

type Category string

const (
	CategoryUserActionRequired Category = "user_action_required"
	CategoryInfraError         Category = "infra_error"
	CategoryCodexError         Category = "codex_error"
	CategoryWorkflowError      Category = "workflow_error"
	CategoryAuthError          Category = "auth_error"
	CategoryValidationError    Category = "validation_error"
)

const (
	CodeWorktreeFailed       = "worktree_failed"
	CodeMergeConflict        = "merge_conflict"
	CodeMergeFailed          = "merge_failed"
	CodeCodexStartFailed     = "codex_start_failed"
	CodeCodexParamRejected   = "codex_param_rejected"
	CodeResumeFailed         = "resume_failed"
	CodeAttachmentFailed     = "attachment_failed"
	CodeAnswerUserCancelled  = "answer_user_cancelled"
	CodeWorkflowBlocked      = "workflow_blocked"
	CodeWorkflowJSONRequired = "workflow_json_required"
	CodeAuthFailed           = "auth_failed"
	CodePermissionDenied     = "permission_denied"
	CodeDiffUnavailable      = "diff_unavailable"
	CodeCloseFailed          = "close_failed"
	CodeValidationFailed     = "validation_failed"
	CodeNotFound             = "not_found"
	CodeInternal             = "internal_error"
)

type Error struct {
	Code       string
	Category   Category
	Message    string
	Details    map[string]any
	Retryable  bool
	UserAction string
	err        error
}

func New(code string, category Category, message string) *Error {
	return &Error{Code: code, Category: category, Message: message}
}

func Wrap(err error, code string, category Category, message string) *Error {
	if err == nil {
		return New(code, category, message)
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return &Error{Code: code, Category: category, Message: message, err: err}
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" && e.err != nil {
		return e.err.Error()
	}
	if e.err == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.err)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *Error) WithDetails(details map[string]any) *Error {
	e.Details = details
	return e
}

func (e *Error) WithRetryable(retryable bool) *Error {
	e.Retryable = retryable
	return e
}

func (e *Error) WithUserAction(action string) *Error {
	e.UserAction = action
	return e
}

func (e *Error) PublicMessage() string {
	if e == nil {
		return ""
	}
	message := e.Message
	if message == "" {
		message = e.Error()
	}
	return redaction.Text(message)
}

func (e *Error) PublicDetails() map[string]any {
	if e == nil || len(e.Details) == 0 {
		return nil
	}
	return redaction.Map(e.Details)
}

func From(err error) (*Error, bool) {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}

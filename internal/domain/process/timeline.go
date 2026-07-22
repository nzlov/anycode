package process

type CodexPhase string

const (
	CodexPhaseStandalone CodexPhase = "standalone"
	CodexPhaseStarted    CodexPhase = "started"
	CodexPhaseProgress   CodexPhase = "progress"
	CodexPhaseCompleted  CodexPhase = "completed"
	CodexPhaseFailed     CodexPhase = "failed"
	CodexPhaseCancelled  CodexPhase = "cancelled"
)

type CodexTextFormat string

const (
	CodexTextPlain    CodexTextFormat = "plain"
	CodexTextMarkdown CodexTextFormat = "markdown"
	CodexTextJSON     CodexTextFormat = "json"
	CodexTextANSI     CodexTextFormat = "ansi"
)

type CodexEventContent interface {
	isCodexEventContent()
}

func (PlanUpdate) isCodexEventContent() {}

func (ExitResult) isCodexEventContent() {}

type CodexImage struct {
	Source     string
	Detail     string
	SourceKind string
	MimeType   string
}

type CodexStructuredText struct {
	Format CodexTextFormat
	Text   string
}

type CodexMessageContent struct {
	Role   string
	Text   string
	Format CodexTextFormat
	Images []CodexImage
}

func (CodexMessageContent) isCodexEventContent() {}

type CodexReasoningContent struct {
	Text string
}

func (CodexReasoningContent) isCodexEventContent() {}

type CodexCommandInvocation struct {
	Command    string
	Workdir    string
	HasOutput  bool
	Output     string
	ExitCode   *int
	DurationMS *int
}

type CodexCommandKind string

const (
	CodexCommandExec  CodexCommandKind = "exec"
	CodexCommandShell CodexCommandKind = "shell"
)

type CodexCommandContent struct {
	Kind       CodexCommandKind
	Commands   []CodexCommandInvocation
	DurationMS *int
}

func (CodexCommandContent) isCodexEventContent() {}

type CodexToolContent struct {
	QualifiedName string
	Category      string
	Input         CodexStructuredText
	Output        CodexStructuredText
	Images        []CodexImage
}

func (CodexToolContent) isCodexEventContent() {}

type CodexFileChange struct {
	Kind        string
	Path        string
	MovePath    string
	UnifiedDiff string
}

type CodexFileChangeContent struct {
	Changes []CodexFileChange
}

func (CodexFileChangeContent) isCodexEventContent() {}

type CodexStatusContent struct {
	Code    string
	Level   string
	Message string
	Details map[string]any
}

func (CodexStatusContent) isCodexEventContent() {}

type CodexUsageContent struct {
	InputTokens                  int
	CachedInputTokens            int
	OutputTokens                 int
	ReasoningOutputTokens        int
	TotalTokens                  int
	ContextWindow                int
	CurrentInputTokens           int
	CurrentCachedInputTokens     int
	CurrentOutputTokens          int
	CurrentReasoningOutputTokens int
	CurrentTotalTokens           int
}

func (CodexUsageContent) isCodexEventContent() {}

type CodexUnknownContent struct {
	RawType string
	Payload map[string]any
}

func (CodexUnknownContent) isCodexEventContent() {}

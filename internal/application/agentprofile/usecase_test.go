package agentprofile

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	domain "github.com/nzlov/anycode/internal/domain/agentprofile"
	"github.com/nzlov/anycode/internal/domain/process"
)

type memoryRepo struct {
	mu          sync.Mutex
	p           domain.Profile
	appends     []domain.PromptAppend
	threads     map[string]string
	threadReads int
}

func (r *memoryRepo) Get(context.Context) (domain.Profile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.p, nil
}
func (r *memoryRepo) SaveConfiguration(_ context.Context, c domain.Configuration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.p.Configuration = c
	return nil
}
func (r *memoryRepo) SaveAnalysis(_ context.Context, a domain.Analysis) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.p.Analysis = a
	return nil
}
func (r *memoryRepo) ListPromptAppends(context.Context, time.Time, time.Time) ([]domain.PromptAppend, error) {
	return r.appends, nil
}
func (r *memoryRepo) ThreadForPrompt(_ context.Context, id string) (string, error) {
	r.threadReads++
	return r.threads[id], nil
}

type historyStub struct {
	pages  map[string]process.CodexHistoryPage
	detail process.CodexEvent
	err    error
}

func (h historyStub) HistoryPage(_ context.Context, in process.CodexHistoryPageInput) (process.CodexHistoryPage, error) {
	return h.pages[in.ThreadID+in.Cursor], h.err
}
func (h historyStub) HistoryEvent(context.Context, process.CodexHistoryEventInput) (process.CodexEvent, error) {
	return h.detail, h.err
}

type analyzerFunc func(context.Context, string, string, string, string) (string, error)

func (f analyzerFunc) AnalyzeProfile(ctx context.Context, p, h, m, effort string) (domain.AnalysisResult, error) {
	text, err := f(ctx, p, h, m, effort)
	return domain.AnalysisResult{Prompt: text}, err
}

func message(at time.Time, offset int64, text string) process.CodexEvent {
	return process.CodexEvent{EventID: text, CreatedAt: at, SourceOffset: offset, Content: process.CodexMessageContent{Role: "user", Text: text}}
}

func TestConfigureModelAndReasoningEffort(t *testing.T) {
	repo := &memoryRepo{p: domain.Default()}
	s := New(repo, historyStub{}, nil, []process.CodexModel{{
		Slug: "catalog-model", SupportedReasoningLevels: []process.CodexReasoningLevel{{Effort: "medium"}, {Effort: "high"}},
	}})
	defer s.Close()
	ctx := context.Background()
	got, err := s.Configure(ctx, domain.Configuration{Enabled: true, Model: " catalog-model ", ReasoningEffort: " high "})
	if err != nil || got.Configuration.Model != "catalog-model" || got.Configuration.ReasoningEffort != "high" {
		t.Fatalf("normalized selection = %#v, %v", got.Configuration, err)
	}
	if _, err := s.Configure(ctx, domain.Configuration{Enabled: true, Model: "catalog-model", ReasoningEffort: "low"}); err == nil {
		t.Fatal("unsupported effort accepted for catalog model")
	}
	saved, _ := s.Get(ctx)
	if saved.Configuration != got.Configuration {
		t.Fatalf("invalid selection changed saved configuration: %#v", saved.Configuration)
	}
	// The shared selector also supports model names absent from the advertised catalog.
	got, err = s.Configure(ctx, domain.Configuration{Enabled: true, Model: " custom-model ", ReasoningEffort: "high"})
	if err != nil || got.Configuration.Model != "custom-model" || got.Configuration.ReasoningEffort != "high" {
		t.Fatalf("custom model = %#v, %v", got.Configuration, err)
	}
	if _, err := s.Configure(ctx, domain.Configuration{Enabled: true}); err != nil {
		t.Fatalf("existing default-model configuration rejected: %v", err)
	}
}

func TestIncrementalAnalysisUsesOnlyNewPromptAppends(t *testing.T) {
	now := time.Now()
	since := now.Add(-time.Hour)
	p := domain.Default()
	p.Analysis.Prompt = "原有要求"
	p.Analysis.Through = &since
	repo := &memoryRepo{p: p, appends: []domain.PromptAppend{
		{ID: "old", SessionID: "s", Body: "旧记录", CreatedAt: since},
		{ID: "new1", SessionID: "s1", Body: "新增纠正", CreatedAt: since.Add(time.Second)},
		{ID: "new2", SessionID: "s2", Body: "重复反馈", CreatedAt: now},
		{ID: "future", SessionID: "s", Body: "留待下次", CreatedAt: now.Add(time.Second)},
	}}
	var input string
	s := New(repo, historyStub{err: errors.New("must not read raw history")}, analyzerFunc(func(_ context.Context, previous, batch, model string, _ string) (string, error) {
		if previous != "原有要求" {
			t.Errorf("lost previous profile: %q", previous)
		}
		input += batch
		return "合并后的要求", nil
	}), nil)
	defer s.Close()
	got, sessions, messages, err := s.generate(context.Background(), p, now, nil)
	if err != nil || got != "合并后的要求" || sessions != 2 || messages != 2 {
		t.Fatalf("result=%q,%d,%d,%v", got, sessions, messages, err)
	}
	if !strings.Contains(input, "新增纠正") || !strings.Contains(input, "重复反馈") || strings.Contains(input, "旧记录") || strings.Contains(input, "留待下次") || repo.threadReads != 0 {
		t.Fatalf("unexpected input or history reads: %s / %d", input, repo.threadReads)
	}
}

func TestAnalysisBatchesAllUTF8ContentWithoutTruncation(t *testing.T) {
	now := time.Now()
	p := domain.Default()
	repo := &memoryRepo{p: p, appends: []domain.PromptAppend{{ID: "a", SessionID: "s", Body: "纠正", CreatedAt: now}}}
	history := historyStub{err: errors.New("raw history must not be read")}
	repo.appends[0].Body = strings.Repeat("中", batchBytes) + "END"
	calls, chars, end := 0, 0, false
	s := New(repo, history, analyzerFunc(func(_ context.Context, previous, batch, _ string, _ string) (string, error) {
		if len(batch) > batchBytes+256 || !utf8.ValidString(batch) {
			t.Fatalf("invalid batch: %d bytes", len(batch))
		}
		if calls > 0 && previous != "merged" {
			t.Fatal("batch lost accumulated profile")
		}
		calls++
		chars += strings.Count(batch, "中")
		end = end || strings.Contains(batch, "END")
		return "merged", nil
	}), nil)
	defer s.Close()
	if _, _, _, err := s.generate(context.Background(), p, now, nil); err != nil {
		t.Fatal(err)
	}
	if calls < 3 || chars != batchBytes || !end {
		t.Fatalf("truncated history: calls=%d chars=%d end=%t", calls, chars, end)
	}
}

func TestAnalysisProgressIsQueryableBetweenBatchesAndRetainedOnFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "completed"
		if fail {
			name = "failed"
		}
		t.Run(name, func(t *testing.T) {
			now := time.Now()
			since := now.Add(-time.Hour)
			p := domain.Default()
			p.Analysis.Prompt = "old"
			p.Analysis.Through = &since
			repo := &memoryRepo{p: p, appends: []domain.PromptAppend{
				{ID: "a", SessionID: "s1", Body: "明确纠正", CreatedAt: now},
				{ID: "b", SessionID: "s2", Body: strings.Repeat("中", batchBytes), CreatedAt: now},
			}}
			entered, release := make(chan int, 10), make(chan struct{})
			calls := 0
			s := New(repo, historyStub{}, analyzerFunc(func(ctx context.Context, _, _, _ string, _ string) (string, error) {
				calls++
				entered <- calls
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-release:
				}
				if fail && calls == 2 {
					return "", errors.New("second batch failed")
				}
				return "merged", nil
			}), nil)
			s.now = func() time.Time { return now }
			defer s.Close()
			started, err := s.Analyze(context.Background())
			if err != nil || started.Analysis.Progress != (domain.AnalysisProgress{Stage: "preparing"}) {
				t.Fatalf("initial progress = %#v, %v", started.Analysis.Progress, err)
			}
			waitForBatch := func(want int) {
				t.Helper()
				select {
				case got := <-entered:
					if got != want {
						t.Fatalf("batch = %d, want %d", got, want)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("analysis did not reach batch")
				}
			}
			waitForBatch(1)
			first, err := s.Get(context.Background())
			if err != nil || first.Analysis.Progress.Stage != "analyzing" || first.Analysis.Progress.Percent != 0 || first.Analysis.Progress.TotalMessages != 2 {
				t.Fatalf("first batch = %#v, %v", first.Analysis.Progress, err)
			}
			release <- struct{}{}
			waitForBatch(2)
			second, err := s.Get(context.Background())
			if err != nil || second.Analysis.Progress.Percent <= 0 || second.Analysis.Progress.Percent >= 100 || second.Analysis.Progress.CompletedMessages != 1 {
				t.Fatalf("second batch = %#v, %v", second.Analysis.Progress, err)
			}
			// Reopening a dialog or starting again reads the same persisted running task.
			reopened, err := s.Analyze(context.Background())
			if err != nil || reopened.Analysis.Progress != second.Analysis.Progress || reopened.Analysis.Prompt != "old" || !reopened.Analysis.Through.Equal(since) {
				t.Fatalf("running state = %#v, %v", reopened.Analysis, err)
			}
			close(release)
			s.wg.Wait()
			finished, err := s.Get(context.Background())
			if err != nil || finished.Analysis.Status != name {
				t.Fatalf("final status = %#v, %v", finished.Analysis, err)
			}
			if fail {
				if finished.Analysis.Progress != second.Analysis.Progress || finished.Analysis.Prompt != "old" || !finished.Analysis.Through.Equal(since) {
					t.Fatalf("failure lost progress/checkpoint: %#v", finished.Analysis)
				}
			} else if finished.Analysis.Progress != (domain.AnalysisProgress{Stage: "completed", Percent: 100, CompletedMessages: 2, TotalMessages: 2}) {
				t.Fatalf("completion = %#v", finished.Analysis.Progress)
			}
		})
	}
}

func TestConcurrentAnalysisFailureKeepsCheckpointAndRetryUsesSavedConfig(t *testing.T) {
	now := time.Now()
	since := now.Add(-time.Hour)
	p := domain.Default()
	p.Configuration.Model = "original-model"
	p.Configuration.ReasoningEffort = "low"
	p.Analysis.Prompt = "old"
	p.Analysis.Through = &since
	repo := &memoryRepo{p: p, appends: []domain.PromptAppend{{ID: "a", SessionID: "s", Body: "纠正", CreatedAt: now}}}
	history := historyStub{pages: map[string]process.CodexHistoryPage{"t": {Events: []process.CodexEvent{message(now.Add(-time.Minute), 1, "纠正")}}}}
	entered, release := make(chan struct{}), make(chan struct{})
	s := New(repo, history, analyzerFunc(func(_ context.Context, _, _, model, effort string) (string, error) {
		if model != "original-model" || effort != "low" {
			t.Errorf("running analysis lost original selection: %q / %q", model, effort)
		}
		close(entered)
		<-release
		return "partial", errors.New("model failed")
	}), nil)
	s.now = func() time.Time { return now }
	defer s.Close()
	if _, err := s.Analyze(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-entered
	if dto, err := s.Analyze(context.Background()); err != nil || dto.Analysis.Status != "running" {
		t.Fatalf("duplicate = %#v, %v", dto, err)
	}
	if _, err := s.Configure(context.Background(), domain.Configuration{Enabled: false, Cron: "0 3 * * *", Model: "updated-model", ReasoningEffort: "high"}); err != nil {
		t.Fatal(err)
	}
	close(release)
	s.wg.Wait()
	got, _ := repo.Get(context.Background())
	if got.Analysis.Status != "failed" || got.Analysis.Prompt != "old" || !got.Analysis.Through.Equal(since) || got.Configuration.Enabled || got.Configuration.Cron == "" {
		t.Fatalf("failure lost state: %#v", got)
	}
	s.analyzer = analyzerFunc(func(_ context.Context, _, _, model, effort string) (string, error) {
		if model != "updated-model" || effort != "high" {
			t.Errorf("retry did not use saved selection: %q / %q", model, effort)
		}
		return "new", nil
	})
	if _, err := s.Analyze(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.wg.Wait()
	got, _ = repo.Get(context.Background())
	if got.Analysis.Status != "completed" || got.Analysis.Prompt != "new" || !got.Analysis.Through.Equal(now) {
		t.Fatalf("retry = %#v", got)
	}
	if text, err := s.Instructions(context.Background()); err != nil || text != "" {
		t.Fatalf("disabled injection = %q, %v", text, err)
	}
}

func TestRestartRecoveryAndScheduledAnalysis(t *testing.T) {
	now := time.Date(2026, 11, 16, 3, 0, 1, 0, time.Local)
	p := domain.Default()
	p.Configuration.Cron = "0 3 * * *"
	p.Analysis.Status = "running"
	p.Analysis.Prompt = "保留要求"
	repo := &memoryRepo{p: p}
	s := New(repo, historyStub{}, analyzerFunc(func(context.Context, string, string, string, string) (string, error) {
		t.Fatal("empty history should not call model")
		return "", nil
	}), nil)
	s.now = func() time.Time { return now }
	defer s.Close()
	if err := s.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.Get(context.Background())
	if got.Analysis.Status != "failed" || got.Analysis.Prompt != "保留要求" {
		t.Fatalf("restart = %#v", got)
	}
	if err := s.runDue(context.Background(), now.Add(-5*time.Second), now); err != nil {
		t.Fatal(err)
	}
	s.wg.Wait()
	got, _ = repo.Get(context.Background())
	if got.Analysis.Status != "completed" || !got.Analysis.Through.Equal(now) {
		t.Fatalf("scheduled = %#v", got)
	}
	text, err := s.Instructions(context.Background())
	if err != nil || !strings.Contains(text, "保留要求") || !strings.Contains(text, "不构成任何操作授权") {
		t.Fatalf("instructions = %q, %v", text, err)
	}
}

type resultAnalyzerFunc func(context.Context, string, string, string, string) (domain.AnalysisResult, error)

func (f resultAnalyzerFunc) AnalyzeProfile(ctx context.Context, p, h, m, effort string) (domain.AnalysisResult, error) {
	return f(ctx, p, h, m, effort)
}

func TestOnlyRequestedContextIsLoadedAndArbitraryIDsAreRejected(t *testing.T) {
	now := time.Now()
	p := domain.Default()
	p.Configuration.Model = "context-model"
	p.Configuration.ReasoningEffort = "high"
	repo := &memoryRepo{p: p, appends: []domain.PromptAppend{{ID: "a", SessionID: "s", Body: "又犯这个问题了", CreatedAt: now}, {ID: "b", SessionID: "s2", Body: "明确要求", CreatedAt: now}}, threads: map[string]string{"a": "t", "b": "unused"}}
	history := historyStub{pages: map[string]process.CodexHistoryPage{
		"t": {Events: []process.CodexEvent{message(now.Add(time.Second), 3, "未来记录"), message(now.Add(-time.Minute), 1, "前文错误")}},
	}}
	calls := 0
	s := New(repo, history, resultAnalyzerFunc(func(_ context.Context, previous, batch, model, effort string) (domain.AnalysisResult, error) {
		if model != "context-model" || effort != "high" {
			t.Errorf("analysis pass lost model/effort: %q / %q", model, effort)
		}
		calls++
		if calls == 1 {
			if strings.Contains(batch, "前文错误") {
				t.Fatal("eager history read")
			}
			return domain.AnalysisResult{ContextPromptIDs: []string{"a"}}, nil
		}
		if !strings.Contains(batch, "前文错误") || strings.Contains(batch, "未来记录") || !strings.Contains(batch, "不再请求上下文") {
			t.Fatal(batch)
		}
		return domain.AnalysisResult{Prompt: "避免重犯"}, nil
	}), nil)
	defer s.Close()
	var stages []string
	got, _, _, err := s.generate(context.Background(), p, now, func(progress domain.AnalysisProgress) error {
		stages = append(stages, progress.Stage)
		return nil
	})
	if err != nil || got != "避免重犯" || repo.threadReads != 1 || calls != 2 {
		t.Fatalf("result=%q,%v reads=%d calls=%d", got, err, repo.threadReads, calls)
	}
	if strings.Join(stages, ",") != "analyzing,reading_context,refining,analyzing,saving" {
		t.Fatalf("context stages = %v", stages)
	}
	s.analyzer = resultAnalyzerFunc(func(context.Context, string, string, string, string) (domain.AnalysisResult, error) {
		return domain.AnalysisResult{ContextPromptIDs: []string{"foreign"}}, nil
	})
	if _, _, _, err := s.generate(context.Background(), p, now, nil); err == nil {
		t.Fatal("foreign context ID accepted")
	}
}

func TestRequestedHistoryFailureAndInvalidOutputPreserveState(t *testing.T) {
	now := time.Now()
	p := domain.Default()
	p.Analysis.Prompt = "old"
	repo := &memoryRepo{p: p, appends: []domain.PromptAppend{{ID: "a", SessionID: "s", Body: "又犯这个问题", CreatedAt: now}}, threads: map[string]string{"a": "t"}}
	s := New(repo, historyStub{err: errors.New("read failed")}, resultAnalyzerFunc(func(context.Context, string, string, string, string) (domain.AnalysisResult, error) {
		return domain.AnalysisResult{ContextPromptIDs: []string{"a"}}, nil
	}), nil)
	defer s.Close()
	if _, _, _, err := s.generate(context.Background(), p, now, nil); err == nil {
		t.Fatal("requested history failure ignored")
	}
	for _, output := range []string{"", strings.Repeat("x", maxPromptBytes+1)} {
		s.analyzer = analyzerFunc(func(context.Context, string, string, string, string) (string, error) { return output, nil })
		if _, _, _, err := s.generate(context.Background(), p, now, nil); err == nil {
			t.Fatal("invalid output accepted")
		}
	}
}

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	artifactapp "github.com/nzlov/anycode/internal/application/artifact"
	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
	codextoolapp "github.com/nzlov/anycode/internal/application/codextool"
	diffapp "github.com/nzlov/anycode/internal/application/diff"
	eventapp "github.com/nzlov/anycode/internal/application/event"
	notificationapp "github.com/nzlov/anycode/internal/application/notification"
	projectapp "github.com/nzlov/anycode/internal/application/project"
	promptcompletionapp "github.com/nzlov/anycode/internal/application/promptcompletion"
	questionapp "github.com/nzlov/anycode/internal/application/question"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	sessioneventapp "github.com/nzlov/anycode/internal/application/sessionevent"
	settingapp "github.com/nzlov/anycode/internal/application/setting"
	timelineapp "github.com/nzlov/anycode/internal/application/timeline"
	workflowapp "github.com/nzlov/anycode/internal/application/workflow"
	authdomain "github.com/nzlov/anycode/internal/domain/auth"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	"github.com/nzlov/anycode/internal/infra/codexcli"
	"github.com/nzlov/anycode/internal/infra/config"
	"github.com/nzlov/anycode/internal/infra/entstore"
	"github.com/nzlov/anycode/internal/infra/filestore"
	"github.com/nzlov/anycode/internal/infra/fsbrowser"
	"github.com/nzlov/anycode/internal/infra/gitcli"
	"github.com/nzlov/anycode/internal/infra/gitdiffcli"
	"github.com/nzlov/anycode/internal/infra/shellinit"
	webpushinfra "github.com/nzlov/anycode/internal/infra/webpush"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph"
	httpinterface "github.com/nzlov/anycode/internal/interfaces/http"
)

const databaseStartupTimeout = 30 * time.Second
const artifactReconcileInterval = 6 * time.Hour

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("load config: %s", err.Error())
	}
	ctx := context.Background()
	databaseCtx, cancelDatabase := context.WithTimeout(ctx, databaseStartupTimeout)

	store, err := entstore.Open(databaseCtx, entstore.OpenOptions{
		DatabaseURL: cfg.TursoDatabaseURL,
		AuthToken:   cfg.TursoAuthToken,
		DataDir:     cfg.DataDir,
	})
	if err != nil {
		log.Fatalf("open entstore: %s", err.Error())
	}
	defer store.Close()

	if err := store.Migrate(databaseCtx); err != nil {
		log.Fatalf("migrate entstore: %s", err.Error())
	}
	cancelDatabase()

	application, err := newApplication(store, cfg)
	if err != nil {
		log.Fatalf("wire application: %s", err.Error())
	}
	defer application.Close()
	useCases := application.useCases

	if useCases.Artifacts != nil {
		reconcileArtifactOutputs(ctx, useCases.Artifacts)
		go runArtifactReconciliation(ctx, useCases.Artifacts)
	}
	if notifications, ok := useCases.Notifications.(*notificationapp.Service); ok {
		go runNotificationDispatcher(ctx, notifications)
	}
	if err := reconcileInterruptedSessions(ctx, useCases.Sessions); err != nil {
		log.Fatalf("recover sessions: %s", err.Error())
	}
	if err := reconcileWorktreeCleanup(ctx, useCases.Sessions); err != nil {
		log.Fatalf("reconcile worktree cleanup: %s", err.Error())
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpinterface.NewHandler(cfg, httpinterface.WithGraphQLUseCases(useCases), httpinterface.WithAttachmentUseCase(useCases.Attachments), httpinterface.WithPlayground()),
		ReadHeaderTimeout: 5 * time.Second,
	}

	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		log.Fatalf("listen on %s: %s", cfg.HTTPAddr, err.Error())
	}
	log.Printf("anycode listening on %s", cfg.HTTPAddr)
	useCases.Sessions.StartWorktreeCleanupCoordinator()
	go func() {
		if err := drainQueuedSessions(context.Background(), useCases.Sessions); err != nil {
			log.Printf("drain queued sessions: %s", err.Error())
		}
	}()
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("anycode stopped: %s", err.Error())
	}
}

type artifactRecoveryUseCase interface {
	ReconcileQuarantines(ctx context.Context) (int, error)
	ReconcileOutputs(ctx context.Context) (int, error)
}

func reconcileArtifactOutputs(ctx context.Context, artifacts artifactRecoveryUseCase) {
	if count, err := artifacts.ReconcileQuarantines(ctx); err != nil {
		log.Printf("reconcile artifact quarantines: %s", err.Error())
	} else if count > 0 {
		log.Printf("reconciled artifact quarantines: count=%d", count)
	}
	count, err := artifacts.ReconcileOutputs(ctx)
	if err != nil {
		log.Printf("reconcile artifact outputs: %s", err.Error())
	}
	if count > 0 {
		log.Printf("reconciled artifact outputs: count=%d", count)
	}
}

func runArtifactReconciliation(ctx context.Context, artifacts artifactRecoveryUseCase) {
	ticker := time.NewTicker(artifactReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcileArtifactOutputs(ctx, artifacts)
		}
	}
}

type wiredApplication struct {
	useCases graph.UseCases
	codex    *codexcli.Client
}

func (a *wiredApplication) Close() {
	if a == nil {
		return
	}
	if closer, ok := a.useCases.Sessions.(interface{ Close() }); ok {
		closer.Close()
	}
	if a.codex != nil {
		if err := a.codex.Close(); err != nil {
			log.Printf("close codex app-server: %s", err.Error())
		}
	}
}

func newApplication(store *entstore.Store, cfg config.Config) (*wiredApplication, error) {
	if store == nil {
		return nil, errors.New("nil entstore")
	}
	files := filestore.New(cfg.DataDir)
	attachments := store.Attachments()
	artifacts := artifactapp.New(files, store.Sessions())
	artifacts.SetLimits(artifactapp.Limits{MaxFileBytes: cfg.ArtifactMaxFileBytes, MaxSessionBytes: cfg.ArtifactMaxSessionBytes})
	codex := codexcli.New(cfg.CodexBin, codexcli.WithObserver(codexMetricLogger{}))
	capabilities, err := ensureCodexReady(context.Background(), codex)
	if err != nil {
		_ = codex.Close()
		return nil, err
	}
	log.Printf("codex image generation capability: enabled=%t status=%s", capabilities.SupportsImageGeneration, capabilities.ImageGenerationStatus)
	events := store.Events()
	eventService := eventapp.New(eventapp.WithObserver(eventMetricLogger{}))
	processes := store.Processes()
	timelineService := timelineapp.New(eventService, store.Sessions(), codex, timelineapp.WithHistory(events))
	questions := store.Questions()
	questionService := questionapp.New(questions, questionapp.WithObserver(questionMetricLogger{}))
	workflowService := workflowapp.New(store.Workflows(), workflowapp.WithUnitOfWork(store), workflowapp.WithEvents(events), workflowapp.WithEventPublisher(eventService))
	gitdiffClient := gitdiffcli.New("")
	diffService := diffapp.New(store.Sessions(), store.Projects(), gitdiffClient)
	sessionService := sessionapp.New(store.Sessions(), store.Projects(), sessionapp.WithAttachments(attachments, files), sessionapp.WithArtifactPublisher(artifacts), sessionapp.WithWorktrees(gitcli.NewWorktrees(cfg.DataDir)), sessionapp.WithWorktreeInitializer(shellinit.New()), sessionapp.WithWorkflows(workflowService), sessionapp.WithMergePort(gitdiffClient), sessionapp.WithDiffCounter(diffService), sessionapp.WithProcesses(processes, codex), sessionapp.WithEvents(events), sessionapp.WithEventPublisher(eventService), sessionapp.WithQuestions(questionService), sessionapp.WithUnitOfWork(store), sessionapp.WithSessionHistoryPurger(store), sessionapp.WithSessionLocker(sessionapp.NewMemorySessionLocker()), sessionapp.WithMaxConcurrentAgents(cfg.AgentMaxConcurrent), sessionapp.WithAutoQueueDrain())
	codex.SetDynamicToolHandler(codextoolapp.New(sessionService, artifacts))
	pushClient := webpushinfra.New()
	principal := authdomain.NewAccessPrincipal(cfg.AccessKey, "web_push")
	notificationService := notificationapp.New(store.Notifications(), events, store.Sessions(), store.Projects(), pushClient, pushClient, principal.KeyHash)
	if err := notificationService.Initialize(context.Background()); err != nil {
		_ = codex.Close()
		return nil, fmt.Errorf("initialize web push notifications: %w", err)
	}
	sessionEventService := sessioneventapp.New(timelineService, eventService, sessionService)
	useCases := graph.UseCases{
		Projects:         projectapp.New(store.Projects(), fsbrowser.New(), gitcli.New("")),
		Sessions:         sessionService,
		Timeline:         timelineService,
		SessionEvents:    sessionEventService,
		Attachments:      attachmentapp.New(attachments, files),
		Artifacts:        artifacts,
		Diff:             diffService,
		Workflows:        workflowService,
		Questions:        questionService,
		Notifications:    notificationService,
		PromptCompletion: promptcompletionapp.New(store.Projects(), store.Sessions(), codex),
		Settings:         settingapp.New(store.Settings()),
		CodexModels:      capabilities.Models,
	}
	return &wiredApplication{useCases: useCases, codex: codex}, nil
}

func runNotificationDispatcher(ctx context.Context, service *notificationapp.Service) {
	for {
		err := service.Run(ctx)
		if errors.Is(err, context.Canceled) {
			return
		}
		log.Printf("notification dispatcher stopped: %s", err.Error())
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

type recoverySessionUseCase interface {
	RecoverInterruptedSessions(ctx context.Context) (int, error)
	DrainQueuedSessions(ctx context.Context) (int, error)
	ReconcileWorktreeCleanup(ctx context.Context) (int, error)
}

func reconcileInterruptedSessions(ctx context.Context, sessions recoverySessionUseCase) error {
	recoverableCount, err := sessions.RecoverInterruptedSessions(ctx)
	if err != nil {
		return err
	}
	if recoverableCount > 0 {
		log.Printf("reconciled interrupted codex sessions: count=%d", recoverableCount)
	}
	return nil
}

func drainQueuedSessions(ctx context.Context, sessions recoverySessionUseCase) error {
	drainedCount, err := sessions.DrainQueuedSessions(ctx)
	if err != nil {
		return err
	}
	if drainedCount > 0 {
		log.Printf("started queued codex sessions: count=%d", drainedCount)
	}
	return nil
}

func reconcileWorktreeCleanup(ctx context.Context, sessions recoverySessionUseCase) error {
	reconciledCount, err := sessions.ReconcileWorktreeCleanup(ctx)
	if err != nil {
		return err
	}
	if reconciledCount > 0 {
		log.Printf("reconciled provisioning session worktrees: count=%d", reconciledCount)
	}
	return nil
}

type codexProber interface {
	Probe(ctx context.Context) (processdomain.CodexCapabilities, error)
}

func ensureCodexReady(ctx context.Context, prober codexProber) (processdomain.CodexCapabilities, error) {
	if prober == nil {
		return processdomain.CodexCapabilities{}, errors.New("nil codex prober")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	capabilities, err := prober.Probe(probeCtx)
	if err != nil {
		return processdomain.CodexCapabilities{}, fmt.Errorf("probe codex cli: %w", err)
	}
	if !capabilities.SupportsAppServer {
		return processdomain.CodexCapabilities{}, errors.New("codex cli does not support app-server")
	}
	if len(capabilities.Models) == 0 {
		return processdomain.CodexCapabilities{}, errors.New("codex cli did not return model options")
	}
	log.Printf("codex cli ready: version=%s app_server=%t models=%d", capabilities.Version, capabilities.SupportsAppServer, len(capabilities.Models))
	return capabilities, nil
}

type codexMetricLogger struct{}

func (codexMetricLogger) Observe(observation codexcli.Observation) {
	log.Printf("metric=%s outcome=%s reason=%s duration_ms=%d bytes=%d", observation.Name, observation.Outcome, observation.Reason, observation.Duration.Milliseconds(), observation.Bytes)
}

type eventMetricLogger struct{}

func (eventMetricLogger) Observe(observation eventapp.Observation) {
	log.Printf("metric=%s outcome=%s", observation.Name, observation.Outcome)
}

type questionMetricLogger struct{}

func (questionMetricLogger) Observe(observation questionapp.Observation) {
	log.Printf("metric=%s outcome=%s duration_ms=%d", observation.Name, observation.Outcome, observation.Duration.Milliseconds())
}

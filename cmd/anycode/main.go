package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	artifactapp "github.com/nzlov/anycode/internal/application/artifact"
	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
	diffapp "github.com/nzlov/anycode/internal/application/diff"
	eventapp "github.com/nzlov/anycode/internal/application/event"
	projectapp "github.com/nzlov/anycode/internal/application/project"
	questionapp "github.com/nzlov/anycode/internal/application/question"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	settingapp "github.com/nzlov/anycode/internal/application/setting"
	timelineapp "github.com/nzlov/anycode/internal/application/timeline"
	workflowapp "github.com/nzlov/anycode/internal/application/workflow"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	"github.com/nzlov/anycode/internal/infra/codexcli"
	"github.com/nzlov/anycode/internal/infra/config"
	"github.com/nzlov/anycode/internal/infra/entstore"
	"github.com/nzlov/anycode/internal/infra/filestore"
	"github.com/nzlov/anycode/internal/infra/fsbrowser"
	"github.com/nzlov/anycode/internal/infra/gitcli"
	"github.com/nzlov/anycode/internal/infra/gitdiffcli"
	"github.com/nzlov/anycode/internal/infra/mcpstdio"
	"github.com/nzlov/anycode/internal/infra/shellinit"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph"
	httpinterface "github.com/nzlov/anycode/internal/interfaces/http"
)

const databaseStartupTimeout = 30 * time.Second
const artifactReconcileInterval = 6 * time.Hour

func main() {
	if len(os.Args) > 1 && os.Args[1] == "mcp-stdio" {
		if err := runMCPStdio(os.Args[2:]); err != nil {
			log.Fatalf("mcp stdio: %s", err.Error())
		}
		return
	}

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

	executable, err := os.Executable()
	if err != nil {
		log.Fatalf("resolve executable: %s", err.Error())
	}
	mcpSocket := localMCPSocketPath(os.Getuid(), os.Getpid())
	useCases, err := newGraphQLUseCases(store, cfg, executable, mcpSocket)
	if err != nil {
		log.Fatalf("wire graphql usecases: %s", err.Error())
	}
	if closer, ok := useCases.Sessions.(interface{ Close() }); ok {
		defer closer.Close()
	}
	stopMCP, err := startMCPUnixServer(cfg, useCases, mcpSocket)
	if err != nil {
		log.Fatalf("start mcp unix server: %s", err.Error())
	}
	defer stopMCP()

	if useCases.Artifacts != nil {
		reconcileArtifactOutputs(ctx, useCases.Artifacts)
		go runArtifactReconciliation(ctx, useCases.Artifacts)
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
	ReconcileDeletedArtifacts(ctx context.Context) (int, error)
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
	if count, err := artifacts.ReconcileDeletedArtifacts(ctx); err != nil {
		log.Printf("reconcile deleted artifacts: %s", err.Error())
	} else if count > 0 {
		log.Printf("reconciled deleted artifacts: count=%d", count)
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

func runMCPStdio(args []string) error {
	flags := flag.NewFlagSet("mcp-stdio", flag.ContinueOnError)
	sessionID := flags.String("session-id", "", "AnyCode session id")
	socket := flags.String("socket", "", "AnyCode MCP Unix socket")
	if err := flags.Parse(args); err != nil {
		return err
	}
	return mcpstdio.Run(context.Background(), os.Stdin, os.Stdout, mcpstdio.Config{
		SessionID: *sessionID,
		Socket:    *socket,
	})
}

func newGraphQLUseCases(store *entstore.Store, cfg config.Config, mcpCommand string, mcpSocket string) (graph.UseCases, error) {
	if store == nil {
		return graph.UseCases{}, errors.New("nil entstore")
	}
	files := filestore.New(cfg.DataDir)
	attachments := store.Attachments()
	artifacts := artifactapp.New(attachments, files, files, store.Sessions())
	artifacts.SetLimits(artifactapp.Limits{MaxFileBytes: cfg.ArtifactMaxFileBytes, MaxSessionBytes: cfg.ArtifactMaxSessionBytes})
	if cfg.PlaywrightMCPBin != "" {
		if _, err := exec.LookPath(cfg.PlaywrightMCPBin); err != nil {
			return graph.UseCases{}, fmt.Errorf("find Playwright MCP executable %q: %w", cfg.PlaywrightMCPBin, err)
		}
	}
	codexOptions := []codexcli.Option{codexcli.WithMCP(localHTTPBaseURL(cfg.HTTPAddr), cfg.AccessKey)}
	if cfg.PlaywrightMCPBin != "" {
		codexOptions = append(codexOptions, codexcli.WithPlaywrightMCP(cfg.PlaywrightMCPBin, cfg.ChromiumBin))
	}
	codex := codexcli.New(cfg.CodexBin, codexOptions...)
	if mcpCommand != "" && mcpSocket != "" {
		codexOptions = []codexcli.Option{codexcli.WithMCPStdio(mcpCommand, mcpSocket, cfg.AccessKey)}
		if cfg.PlaywrightMCPBin != "" {
			codexOptions = append(codexOptions, codexcli.WithPlaywrightMCP(cfg.PlaywrightMCPBin, cfg.ChromiumBin))
		}
		codex = codexcli.New(cfg.CodexBin, codexOptions...)
	}
	capabilities, err := ensureCodexReady(context.Background(), codex)
	if err != nil {
		return graph.UseCases{}, err
	}
	log.Printf("codex image generation capability: enabled=%t status=%s", capabilities.SupportsImageGeneration, capabilities.ImageGenerationStatus)
	events := store.Events()
	eventService := eventapp.New()
	artifacts.SetEvents(events, eventService)
	processes := store.Processes()
	timelineService := timelineapp.New(eventService, store.Sessions(), codex, processes, timelineapp.WithHistory(events))
	questions := store.Questions()
	questionService := questionapp.New(questions)
	workflowService := workflowapp.New(store.Workflows(), workflowapp.WithUnitOfWork(store), workflowapp.WithEvents(events), workflowapp.WithEventPublisher(eventService))
	gitdiffClient := gitdiffcli.New("")
	sessionService := sessionapp.New(store.Sessions(), store.Projects(), sessionapp.WithAttachments(attachments, files), sessionapp.WithArtifactScanner(artifacts), sessionapp.WithArtifactPublisher(artifacts), sessionapp.WithWorktrees(gitcli.NewWorktrees(cfg.DataDir)), sessionapp.WithWorktreeInitializer(shellinit.New()), sessionapp.WithWorkflows(workflowService), sessionapp.WithMergePort(gitdiffClient), sessionapp.WithProcesses(processes, codex), sessionapp.WithEvents(events), sessionapp.WithEventPublisher(eventService), sessionapp.WithQuestions(questionService), sessionapp.WithUnitOfWork(store), sessionapp.WithSessionLocker(sessionapp.NewMemorySessionLocker()), sessionapp.WithMaxConcurrentAgents(cfg.AgentMaxConcurrent), sessionapp.WithAutoQueueDrain())
	return graph.UseCases{
		Projects:    projectapp.New(store.Projects(), fsbrowser.New(), gitcli.New("")),
		Sessions:    sessionService,
		Events:      eventService,
		Timeline:    timelineService,
		Attachments: attachmentapp.New(attachments, files),
		Artifacts:   artifacts,
		Diff:        diffapp.New(store.Sessions(), store.Projects(), gitdiffClient),
		Workflows:   workflowService,
		Questions:   questionService,
		Settings:    settingapp.New(store.Settings()),
		CodexModels: capabilities.Models,
	}, nil
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

func startMCPUnixServer(cfg config.Config, useCases graph.UseCases, socketPath string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(filepath.Dir(socketPath), 0o700); err != nil {
		return nil, err
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(socketPath)
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("POST /mcp/sessions/{sessionID}", httpinterface.NewMCPHandler(cfg, useCases.Sessions, useCases.Artifacts))
	mux.Handle("POST /mcp/sessions/{sessionID}/deliveries/{batchID}/ack", httpinterface.NewMCPHandler(cfg, useCases.Sessions, useCases.Artifacts))
	mux.Handle("POST /mcp/sessions/{sessionID}/deliveries/{batchID}/{action}", httpinterface.NewMCPHandler(cfg, useCases.Sessions, useCases.Artifacts))
	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("mcp unix server stopped: %s", err.Error())
		}
	}()
	log.Printf("mcp unix server listening: pid=%d socket=%s", os.Getpid(), filepath.Base(socketPath))
	return func() {
		_ = server.Close()
		_ = listener.Close()
		_ = os.Remove(socketPath)
	}, nil
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
	if !capabilities.SupportsExec {
		return processdomain.CodexCapabilities{}, errors.New("codex cli does not support exec")
	}
	if !capabilities.SupportsResume {
		return processdomain.CodexCapabilities{}, errors.New("codex cli does not support exec resume")
	}
	if len(capabilities.Models) == 0 {
		return processdomain.CodexCapabilities{}, errors.New("codex cli did not return model options")
	}
	log.Printf("codex cli ready: version=%s exec=%t resume=%t mcp_tool_timeout=%t models=%d", capabilities.Version, capabilities.SupportsExec, capabilities.SupportsResume, capabilities.SupportsMCPToolTimeout, len(capabilities.Models))
	return capabilities, nil
}

func localHTTPBaseURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/")
	}
	if strings.HasPrefix(addr, ":") {
		return "http://127.0.0.1" + addr
	}
	return "http://" + addr
}

func localMCPSocketPath(uid int, pid int) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("anycode-%d", uid), fmt.Sprintf("mcp-%d.sock", pid))
}

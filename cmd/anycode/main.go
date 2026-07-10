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
	"path/filepath"
	"strings"
	"time"

	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
	diffapp "github.com/nzlov/anycode/internal/application/diff"
	eventapp "github.com/nzlov/anycode/internal/application/event"
	projectapp "github.com/nzlov/anycode/internal/application/project"
	questionapp "github.com/nzlov/anycode/internal/application/question"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
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
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph"
	httpinterface "github.com/nzlov/anycode/internal/interfaces/http"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "mcp-stdio" {
		if err := runMCPStdio(os.Args[2:]); err != nil {
			log.Fatalf("mcp stdio: %s", err.Error())
		}
		return
	}

	cfg := config.LoadFromEnv()
	ctx := context.Background()

	store, err := entstore.Open(ctx, entstore.OpenOptions{
		DatabaseURL: cfg.TursoDatabaseURL,
		AuthToken:   cfg.TursoAuthToken,
		DataDir:     cfg.DataDir,
	})
	if err != nil {
		log.Fatalf("open entstore: %s", err.Error())
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		log.Fatalf("migrate entstore: %s", err.Error())
	}

	executable, err := os.Executable()
	if err != nil {
		log.Fatalf("resolve executable: %s", err.Error())
	}
	mcpSocket := localMCPSocketPath()
	useCases, err := newGraphQLUseCases(store, cfg.DataDir, cfg.CodexBin, cfg.HTTPAddr, cfg.AccessKey, cfg.AgentMaxConcurrent, executable, mcpSocket)
	if err != nil {
		log.Fatalf("wire graphql usecases: %s", err.Error())
	}
	stopMCP, err := startMCPUnixServer(cfg, useCases, mcpSocket)
	if err != nil {
		log.Fatalf("start mcp unix server: %s", err.Error())
	}
	defer stopMCP()

	if err := recoverAndDrainSessions(ctx, useCases.Sessions); err != nil {
		log.Fatalf("recover sessions: %s", err.Error())
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpinterface.NewHandler(cfg, httpinterface.WithGraphQLUseCases(useCases), httpinterface.WithAttachmentUseCase(useCases.Attachments), httpinterface.WithPlayground()),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("anycode listening on %s", cfg.HTTPAddr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("anycode stopped: %s", err.Error())
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

func newGraphQLUseCases(store *entstore.Store, dataDir string, codexBin string, httpAddr string, accessKey string, maxConcurrentAgents int, mcpCommand string, mcpSocket string) (graph.UseCases, error) {
	if store == nil {
		return graph.UseCases{}, errors.New("nil entstore")
	}
	files := filestore.New(dataDir)
	attachments := store.Attachments()
	codex := codexcli.New(codexBin, codexcli.WithMCP(localHTTPBaseURL(httpAddr), accessKey))
	if mcpCommand != "" && mcpSocket != "" {
		codex = codexcli.New(codexBin, codexcli.WithMCPStdio(mcpCommand, mcpSocket, accessKey))
	}
	if err := ensureCodexReady(context.Background(), codex); err != nil {
		return graph.UseCases{}, err
	}
	events := store.Events()
	eventService := eventapp.New()
	processes := store.Processes()
	timelineService := timelineapp.New(eventService, store.Sessions(), codex, processes)
	questionWaiter := questionapp.NewMemoryAnswerWaiter()
	questionService := questionapp.New(store.Questions(), questionWaiter)
	workflowService := workflowapp.New(store.Workflows(), workflowapp.WithUnitOfWork(store), workflowapp.WithEvents(events), workflowapp.WithEventPublisher(eventService))
	gitdiffClient := gitdiffcli.New("")
	sessionService := sessionapp.New(store.Sessions(), store.Projects(), sessionapp.WithAttachments(attachments, files), sessionapp.WithWorktrees(gitcli.NewWorktrees(dataDir)), sessionapp.WithWorkflows(workflowService), sessionapp.WithMergePort(gitdiffClient), sessionapp.WithProcesses(processes, codex), sessionapp.WithEvents(events), sessionapp.WithEventPublisher(eventService), sessionapp.WithQuestions(questionService), sessionapp.WithUnitOfWork(store), sessionapp.WithSessionLocker(sessionapp.NewMemorySessionLocker()), sessionapp.WithMaxConcurrentAgents(maxConcurrentAgents), sessionapp.WithAutoQueueDrain())
	return graph.UseCases{
		Projects:    projectapp.New(store.Projects(), fsbrowser.New(), gitcli.New("")),
		Sessions:    sessionService,
		Events:      eventService,
		Timeline:    timelineService,
		Attachments: attachmentapp.New(attachments, files),
		Diff:        diffapp.New(store.Sessions(), store.Projects(), gitdiffClient),
		Workflows:   workflowService,
		Questions:   questionService,
	}, nil
}

func recoverAndDrainSessions(ctx context.Context, sessions sessionapp.UseCase) error {
	recoverableCount, err := sessions.MarkInterruptedSessionsRecoverable(ctx)
	if err != nil {
		return err
	}
	if recoverableCount > 0 {
		log.Printf("marked interrupted codex sessions recoverable: count=%d", recoverableCount)
	}
	drainedCount, err := sessions.DrainQueuedSessions(ctx)
	if err != nil {
		return err
	}
	if drainedCount > 0 {
		log.Printf("started queued codex sessions: count=%d", drainedCount)
	}
	return nil
}

func startMCPUnixServer(cfg config.Config, useCases graph.UseCases, socketPath string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return nil, err
	}
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		listener.Close()
		return nil, err
	}
	mux := http.NewServeMux()
	mux.Handle("POST /mcp/sessions/{sessionID}", httpinterface.NewMCPHandler(cfg, useCases.Questions, useCases.Sessions))
	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("mcp unix server stopped: %s", err.Error())
		}
	}()
	return func() {
		_ = server.Close()
		_ = listener.Close()
		_ = os.Remove(socketPath)
	}, nil
}

type codexProber interface {
	Probe(ctx context.Context) (processdomain.CodexCapabilities, error)
}

func ensureCodexReady(ctx context.Context, prober codexProber) error {
	if prober == nil {
		return errors.New("nil codex prober")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	capabilities, err := prober.Probe(probeCtx)
	if err != nil {
		return fmt.Errorf("probe codex cli: %w", err)
	}
	if !capabilities.SupportsExec {
		return errors.New("codex cli does not support exec")
	}
	if !capabilities.SupportsResume {
		return errors.New("codex cli does not support exec resume")
	}
	log.Printf("codex cli ready: version=%s exec=%t resume=%t", capabilities.Version, capabilities.SupportsExec, capabilities.SupportsResume)
	return nil
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

func localMCPSocketPath() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("anycode-%d", os.Getuid()), "mcp.sock")
}

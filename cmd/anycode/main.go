package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
	diffapp "github.com/nzlov/anycode/internal/application/diff"
	eventapp "github.com/nzlov/anycode/internal/application/event"
	projectapp "github.com/nzlov/anycode/internal/application/project"
	questionapp "github.com/nzlov/anycode/internal/application/question"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	workflowapp "github.com/nzlov/anycode/internal/application/workflow"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	"github.com/nzlov/anycode/internal/domain/redaction"
	"github.com/nzlov/anycode/internal/infra/codexcli"
	"github.com/nzlov/anycode/internal/infra/config"
	"github.com/nzlov/anycode/internal/infra/entstore"
	"github.com/nzlov/anycode/internal/infra/filestore"
	"github.com/nzlov/anycode/internal/infra/fsbrowser"
	"github.com/nzlov/anycode/internal/infra/gitcli"
	"github.com/nzlov/anycode/internal/infra/gitdiffcli"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph"
	httpinterface "github.com/nzlov/anycode/internal/interfaces/http"
)

func main() {
	cfg := config.LoadFromEnv()
	ctx := context.Background()

	store, err := entstore.Open(ctx, entstore.OpenOptions{
		DatabaseURL: cfg.TursoDatabaseURL,
		AuthToken:   cfg.TursoAuthToken,
		DataDir:     cfg.DataDir,
	})
	if err != nil {
		log.Fatalf("open entstore: %s", redaction.Text(err.Error()))
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		log.Fatalf("migrate entstore: %s", redaction.Text(err.Error()))
	}

	useCases, err := newGraphQLUseCases(store, cfg.DataDir, cfg.CodexBin, cfg.HTTPAddr, cfg.AccessKey)
	if err != nil {
		log.Fatalf("wire graphql usecases: %s", redaction.Text(err.Error()))
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpinterface.NewHandler(cfg, httpinterface.WithGraphQLUseCases(useCases), httpinterface.WithAttachmentUseCase(useCases.Attachments), httpinterface.WithPlayground()),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("anycode listening on %s", cfg.HTTPAddr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("anycode stopped: %s", redaction.Text(err.Error()))
	}
}

func newGraphQLUseCases(store *entstore.Store, dataDir string, codexBin string, httpAddr string, accessKey string) (graph.UseCases, error) {
	if store == nil {
		return graph.UseCases{}, errors.New("nil entstore")
	}
	files := filestore.New(dataDir)
	attachments := store.Attachments()
	codex := codexcli.New(codexBin, codexcli.WithMCP(localHTTPBaseURL(httpAddr), accessKey))
	if err := ensureCodexReady(context.Background(), codex); err != nil {
		return graph.UseCases{}, err
	}
	events := store.Events()
	eventService := eventapp.New(events)
	questionWaiter := questionapp.NewMemoryAnswerWaiter()
	questionService := questionapp.New(store.Questions(), questionWaiter)
	workflowService := workflowapp.New(store.Workflows(), workflowapp.WithUnitOfWork(store), workflowapp.WithEvents(events), workflowapp.WithEventPublisher(eventService))
	gitdiffClient := gitdiffcli.New("")
	sessionService := sessionapp.New(store.Sessions(), store.Projects(), sessionapp.WithAttachments(attachments, files), sessionapp.WithWorktrees(gitcli.NewWorktrees(dataDir)), sessionapp.WithWorkflows(workflowService), sessionapp.WithMergePort(gitdiffClient), sessionapp.WithProcesses(store.Processes(), codex), sessionapp.WithEvents(events), sessionapp.WithEventPublisher(eventService), sessionapp.WithQuestions(questionService), sessionapp.WithUnitOfWork(store), sessionapp.WithSessionLocker(sessionapp.NewMemorySessionLocker()))
	recoverableCount, err := sessionService.MarkInterruptedSessionsRecoverable(context.Background())
	if err != nil {
		return graph.UseCases{}, err
	}
	if recoverableCount > 0 {
		log.Printf("marked interrupted codex sessions recoverable: count=%d", recoverableCount)
	}
	return graph.UseCases{
		Projects:    projectapp.New(store.Projects(), fsbrowser.New(), gitcli.New("")),
		Sessions:    sessionService,
		Events:      eventService,
		Attachments: attachmentapp.New(attachments, files),
		Diff:        diffapp.New(store.Sessions(), store.Projects(), gitdiffClient),
		Workflows:   workflowService,
		Questions:   questionService,
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

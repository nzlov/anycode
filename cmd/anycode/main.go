package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
	eventapp "github.com/nzlov/anycode/internal/application/event"
	projectapp "github.com/nzlov/anycode/internal/application/project"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	"github.com/nzlov/anycode/internal/infra/config"
	"github.com/nzlov/anycode/internal/infra/entstore"
	"github.com/nzlov/anycode/internal/infra/filestore"
	"github.com/nzlov/anycode/internal/infra/fsbrowser"
	"github.com/nzlov/anycode/internal/infra/gitcli"
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
		log.Fatalf("open entstore: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		log.Fatalf("migrate entstore: %v", err)
	}

	useCases, err := newGraphQLUseCases(store, cfg.DataDir)
	if err != nil {
		log.Fatalf("wire graphql usecases: %v", err)
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpinterface.NewHandler(cfg, httpinterface.WithGraphQLUseCases(useCases), httpinterface.WithPlayground()),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("anycode listening on %s", cfg.HTTPAddr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("anycode stopped: %v", err)
	}
}

func newGraphQLUseCases(store *entstore.Store, dataDir string) (graph.UseCases, error) {
	if store == nil {
		return graph.UseCases{}, errors.New("nil entstore")
	}
	files := filestore.New(dataDir)
	attachments := store.Attachments()
	return graph.UseCases{
		Projects:    projectapp.New(store.Projects(), fsbrowser.New(), gitcli.New("")),
		Sessions:    sessionapp.New(store.Sessions(), store.Projects(), sessionapp.WithAttachments(attachments, files)),
		Events:      eventapp.New(store.Events()),
		Attachments: attachmentapp.New(attachments, files),
	}, nil
}

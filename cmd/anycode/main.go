package main

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/nzlov/anycode/internal/infra/config"
	httpinterface "github.com/nzlov/anycode/internal/interfaces/http"
)

func main() {
	cfg := config.LoadFromEnv()

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpinterface.NewHandler(cfg),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("anycode listening on %s", cfg.HTTPAddr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("anycode stopped: %v", err)
	}
}

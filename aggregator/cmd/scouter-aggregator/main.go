// Command scouter-aggregator collects CI and deploy status and streams it to the Scouter phone.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/drilonrecica/scouter/aggregator/internal/github"
	"github.com/drilonrecica/scouter/aggregator/internal/server"
	"github.com/drilonrecica/scouter/aggregator/internal/state"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("exiting", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	addr := env("SCOUTER_ADDR", ":8080")
	dataDir := env("SCOUTER_DATA_DIR", "/data")
	phoneToken := os.Getenv("SCOUTER_TOKEN")
	ghToken := os.Getenv("SCOUTER_GITHUB_TOKEN")
	if phoneToken == "" || ghToken == "" {
		return errors.New("SCOUTER_TOKEN and SCOUTER_GITHUB_TOKEN are required")
	}
	var ignore []string
	for _, r := range strings.Split(os.Getenv("SCOUTER_IGNORE_REPOS"), ",") {
		if r = strings.TrimSpace(r); r != "" {
			ignore = append(ignore, r)
		}
	}

	snapshot := ""
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Warn("no data dir, state will not survive restarts", "dir", dataDir, "err", err)
	} else {
		snapshot = filepath.Join(dataDir, "state.json")
	}
	store := state.NewStore(snapshot)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	gh := github.NewClient(env("SCOUTER_GITHUB_API", "https://api.github.com"), ghToken, &http.Client{Timeout: 20 * time.Second})
	go github.NewPoller(gh, store, ignore, log).Run(ctx)

	// No WriteTimeout: /v1/stream is a long-lived response.
	srv := &http.Server{Addr: addr, Handler: server.New(store, phoneToken), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Info("listening", "addr", addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

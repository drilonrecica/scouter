// Command scouter-aggregator collects CI and deploy status and streams it to the Scouter phone.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/drilonrecica/scouter/aggregator/internal/admin"
	"github.com/drilonrecica/scouter/aggregator/internal/coolify"
	"github.com/drilonrecica/scouter/aggregator/internal/github"
	"github.com/drilonrecica/scouter/aggregator/internal/icons"
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

	snapshot, settingsFile := "", ""
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Warn("no data dir, state and settings will not survive restarts", "dir", dataDir, "err", err)
	} else {
		snapshot = filepath.Join(dataDir, "state.json")
		settingsFile = filepath.Join(dataDir, "settings.json")
	}
	store := state.NewStore(snapshot)
	if settingsFile != "" {
		store.UseSettingsFile(settingsFile)
	}
	reportStorage(store, dataDir, log)
	store.UsePhoneFiles(filepath.Join(dataDir, "phone.json"))
	store.UseHistoryFile(filepath.Join(dataDir, "history.json"))
	apkPath := filepath.Join(dataDir, "app", "scouter.apk")
	if rel, err := state.DescribeAPK(apkPath); err == nil {
		store.SetApp(rel) // keep offering the last upload across restarts
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	gh := github.NewClient(env("SCOUTER_GITHUB_API", "https://api.github.com"), ghToken, &http.Client{Timeout: 20 * time.Second})
	poller := github.NewPoller(gh, store, ignore, log)
	go poller.Run(ctx)
	iconDir := filepath.Join(dataDir, "icons")
	go icons.NewPoller(gh, icons.SiteClient(), store, iconDir, log).Run(ctx)

	coolifyURL, coolifyToken := os.Getenv("SCOUTER_COOLIFY_URL"), os.Getenv("SCOUTER_COOLIFY_TOKEN")
	if coolifyURL != "" && coolifyToken != "" {
		go coolify.NewPoller(coolifyURL, coolifyToken, &http.Client{Timeout: 20 * time.Second}, store, log).Run(ctx)
	} else {
		log.Info("coolify source off: set SCOUTER_COOLIFY_URL and SCOUTER_COOLIFY_TOKEN for deploy status")
	}

	api := server.New(store, phoneToken)
	api.ServeAPK(apkPath)
	api.ServeIcons(iconDir)
	api.AcceptAgentEvents(store, os.Getenv("SCOUTER_HOOK_TOKEN"))
	api.AcceptReleases(store, apkPath, os.Getenv("SCOUTER_RELEASE_TOKEN"), log)
	adminPassword := os.Getenv("SCOUTER_ADMIN_PASSWORD")
	isSet := func(k string) bool { return os.Getenv(k) != "" }
	ui := admin.New(admin.Config{
		Password: adminPassword,
		Store:    store,
		APKPath:  apkPath,
		Log:      log,
		Status: func() admin.Status {
			return admin.Status{Clients: api.Clients(), Rate: gh.RateRemaining(), LastPoll: poller.LastPoll()}
		},
		Secrets: []admin.Secret{
			{Name: "SCOUTER_TOKEN", Purpose: "phone access", Set: true},
			{Name: "SCOUTER_GITHUB_TOKEN", Purpose: "GitHub API, read-only", Set: true},
			{Name: "SCOUTER_ADMIN_PASSWORD", Purpose: "this UI", Set: len(adminPassword) >= admin.MinPassword},
			{Name: "SCOUTER_COOLIFY_URL", Purpose: "Coolify base URL, for deploy status", Set: isSet("SCOUTER_COOLIFY_URL")},
			{Name: "SCOUTER_COOLIFY_TOKEN", Purpose: "Coolify API, read-only", Set: isSet("SCOUTER_COOLIFY_TOKEN")},
			{Name: "SCOUTER_HOOK_TOKEN", Purpose: "Claude Code hooks (optional; the phone token also works)", Set: isSet("SCOUTER_HOOK_TOKEN")},
			{Name: "SCOUTER_RELEASE_TOKEN", Purpose: "publishing APKs from the release script", Set: isSet("SCOUTER_RELEASE_TOKEN")},
			{Name: "SCOUTER_IGNORE_REPOS", Purpose: "repos hidden by env (in addition to settings)", Set: isSet("SCOUTER_IGNORE_REPOS")},
		},
	})
	api.Handle("/admin", ui)
	api.Handle("/admin/", ui)

	// No WriteTimeout: /v1/stream is a long-lived response.
	srv := &http.Server{Addr: addr, Handler: api, ReadHeaderTimeout: 10 * time.Second}
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

// reportStorage checks that the data dir is writable and publishes the result
// as the "storage" source, so a misowned volume shows on the admin status page
// instead of failing silently (settings could not be saved, state not kept).
func reportStorage(store *state.Store, dir string, log *slog.Logger) {
	src := state.Source{OK: true, UpdatedAt: time.Now().UTC()}
	probe := filepath.Join(dir, ".write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		src.OK = false
		src.Error = fmt.Sprintf("%s is not writable (%v): settings cannot be saved. The volume must be writable by uid 65532.", dir, err)
		log.Error("data dir not writable", "dir", dir, "uid", os.Getuid(), "err", err)
	} else {
		os.Remove(probe)
	}
	store.Update(func(in *state.Inputs) { in.Sources["storage"] = src })
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

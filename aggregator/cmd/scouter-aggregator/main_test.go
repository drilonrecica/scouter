package main

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestShutdownEndsOpenStreams runs the whole aggregator, holds a stream open
// like the phone does, and sends SIGTERM: run must return promptly instead of
// waiting out the shutdown timeout for the stream, and only after the stream
// has ended.
func TestShutdownEndsOpenStreams(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("[]"))
	}))
	t.Cleanup(gh.Close)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	t.Setenv("SCOUTER_ADDR", addr)
	t.Setenv("SCOUTER_DATA_DIR", t.TempDir())
	t.Setenv("SCOUTER_TOKEN", "tok")
	t.Setenv("SCOUTER_GITHUB_TOKEN", "gh")
	t.Setenv("SCOUTER_GITHUB_API", gh.URL)
	t.Setenv("SCOUTER_ADMIN_PASSWORD", "")
	t.Setenv("SCOUTER_COOLIFY_URL", "")

	done := make(chan error, 1)
	go func() { done <- run(slog.New(slog.NewTextHandler(io.Discard, nil))) }()

	var resp *http.Response
	for i := 0; ; i++ {
		req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/v1/stream", nil)
		req.Header.Set("Authorization", "Bearer tok")
		if resp, err = http.DefaultClient.Do(req); err == nil {
			break
		}
		if i == 50 {
			t.Fatalf("aggregator did not start: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer resp.Body.Close()
	body := bufio.NewReader(resp.Body)
	line, err := body.ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "id:") {
		t.Fatalf("first stream line %q, %v", line, err)
	}

	start := time.Now()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("shutdown took %v with a stream open", d)
		}
		// Returning is not enough: the stream must be over by then too.
		closed := make(chan struct{})
		go func() { io.Copy(io.Discard, body); close(closed) }()
		select {
		case <-closed:
		case <-time.After(500 * time.Millisecond):
			t.Fatal("run returned while the stream was still open")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("run did not return after SIGTERM")
	}
}

func TestHealthURL(t *testing.T) {
	for addr, want := range map[string]string{
		":8080":          "http://127.0.0.1:8080/healthz",
		"0.0.0.0:9000":   "http://127.0.0.1:9000/healthz",
		"[::]:8080":      "http://127.0.0.1:8080/healthz",
		"10.0.0.5:8080":  "http://10.0.0.5:8080/healthz",
		"localhost:8787": "http://localhost:8787/healthz",
	} {
		if got, err := healthURL(addr); err != nil || got != want {
			t.Errorf("healthURL(%q) = %q, %v; want %q", addr, got, err, want)
		}
	}
	if _, err := healthURL("8080"); err == nil {
		t.Error("an address without a port must be an error")
	}
}

func TestHealthcheckExitCode(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte("ok abc\n"))
	}))
	t.Cleanup(srv.Close)
	addr := strings.TrimPrefix(srv.URL, "http://")

	var out strings.Builder
	if code := healthcheck(addr, &out); code != 0 || out.String() != "ok abc\n" {
		t.Fatalf("healthy: exit %d, output %q", code, out.String())
	}
	status = http.StatusServiceUnavailable
	if code := healthcheck(addr, io.Discard); code != 1 {
		t.Fatalf("503: exit %d, want 1", code)
	}
	srv.Close()
	if code := healthcheck(addr, io.Discard); code != 1 {
		t.Fatalf("server down: exit %d, want 1", code)
	}
}

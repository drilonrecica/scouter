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

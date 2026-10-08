package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// healthcheck asks the running aggregator for /healthz. It is the image's
// Docker HEALTHCHECK: the distroless image has no shell, curl or wget, so
// the binary checks itself. Prints the answer and returns the exit code.
func healthcheck(addr string, out io.Writer) int {
	url, err := healthURL(addr)
	if err != nil {
		fmt.Fprintln(out, err)
		return 1
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		fmt.Fprintln(out, err)
		return 1
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	fmt.Fprint(out, strings.TrimSpace(string(body))+"\n")
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

// healthURL turns SCOUTER_ADDR (":8080", "0.0.0.0:8080", "[::]:8080", …)
// into a URL on the loopback interface.
func healthURL(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("SCOUTER_ADDR %q: %w", addr, err)
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz", nil
}

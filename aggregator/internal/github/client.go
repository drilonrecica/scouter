// Package github polls the GitHub REST API for repository activity and
// workflow results and writes them into the state store.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// Client is a minimal GitHub REST client. It remembers each URL's ETag and
// replays the cached body on 304 Not Modified, which GitHub does not count
// against the rate limit, so polling quiet repos is nearly free.
type Client struct {
	base  string
	token string
	http  *http.Client

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	etag string
	body []byte
}

func NewClient(base, token string, hc *http.Client) *Client {
	return &Client{base: base, token: token, http: hc, cache: map[string]cached{}}
}

func (c *Client) get(ctx context.Context, path string, v any) error {
	url := c.base + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+c.token)
	c.mu.Lock()
	prev, hit := c.cache[url]
	c.mu.Unlock()
	if hit {
		req.Header.Set("If-None-Match", prev.etag)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var body []byte
	switch {
	case resp.StatusCode == http.StatusNotModified && hit:
		body = prev.body
	case resp.StatusCode == http.StatusOK:
		body, err = io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if err != nil {
			return err
		}
		if etag := resp.Header.Get("ETag"); etag != "" {
			c.mu.Lock()
			c.cache[url] = cached{etag: etag, body: body}
			c.mu.Unlock()
		}
	default:
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GET %s: %s: %s", path, resp.Status, msg)
	}
	return json.Unmarshal(body, v)
}

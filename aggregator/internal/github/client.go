// Package github polls the GitHub REST API for repository activity and
// workflow results and writes them into the state store.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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

	rateRemaining atomic.Int64 // from X-RateLimit-Remaining; -1 until known
}

// RateRemaining is GitHub's remaining request budget for this hour, or -1.
func (c *Client) RateRemaining() int64 { return c.rateRemaining.Load() }

type cached struct {
	etag string
	body []byte
}

// maxCached keeps huge answers (a big repo's recursive tree) out of the
// cache: they are fetched in full again instead of held in memory.
const maxCached = 1 << 20

// Forget drops cached answers about a repo that is no longer tracked.
func (c *Client) Forget(fullName string) {
	prefix := c.base + "/repos/" + fullName + "/"
	c.mu.Lock()
	defer c.mu.Unlock()
	for u := range c.cache {
		if strings.HasPrefix(u, prefix) {
			delete(c.cache, u)
		}
	}
}

// RateLimitError is a 403/429 that GitHub sent because the budget is spent.
// Until is when it may be asked again.
type RateLimitError struct {
	Until time.Time
}

func (e *RateLimitError) Error() string {
	return "rate limited until " + e.Until.Format("15:04:05 UTC")
}

// rateLimited recognises a rate limit answer: a Retry-After (secondary
// limits) or an exhausted X-RateLimit-Remaining (the hourly budget). Other
// 403s, such as a token without access to a repo, are plain errors.
func rateLimited(resp *http.Response, now time.Time) *RateLimitError {
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusTooManyRequests {
		return nil
	}
	if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
		return &RateLimitError{Until: now.Add(time.Duration(s) * time.Second)}
	}
	if resp.Header.Get("X-RateLimit-Remaining") == "0" {
		until := now.Add(time.Minute)
		if reset, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			until = time.Unix(reset, 0).UTC()
		}
		return &RateLimitError{Until: until}
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return &RateLimitError{Until: now.Add(time.Minute)}
	}
	return nil
}

func NewClient(base, token string, hc *http.Client) *Client {
	c := &Client{base: base, token: token, http: hc, cache: map[string]cached{}}
	c.rateRemaining.Store(-1)
	return c
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
	if n, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Remaining"), 10, 64); err == nil {
		c.rateRemaining.Store(n)
	}

	var body []byte
	switch {
	case resp.StatusCode == http.StatusNotModified && hit:
		body = prev.body
	case resp.StatusCode == http.StatusOK:
		body, err = io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		if err != nil {
			return err
		}
		if etag := resp.Header.Get("ETag"); etag != "" && len(body) <= maxCached {
			c.mu.Lock()
			c.cache[url] = cached{etag: etag, body: body}
			c.mu.Unlock()
		}
	default:
		if rl := rateLimited(resp, time.Now().UTC()); rl != nil {
			return rl
		}
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GET %s: %s: %s", path, resp.Status, msg)
	}
	return json.Unmarshal(body, v)
}

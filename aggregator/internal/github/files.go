package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// File is one blob in a repository tree.
type File struct {
	Path string `json:"path"`
	Size int    `json:"size"`
}

// Tree lists every file at ref. It is ETag-cached like every other GET, so
// asking again for an unchanged repo costs no rate limit. A tree GitHub had
// to truncate (huge repos) is returned as far as it goes.
func (c *Client) Tree(ctx context.Context, fullName, ref string) ([]File, error) {
	var resp struct {
		Tree []struct {
			File
			Type string `json:"type"`
		} `json:"tree"`
	}
	if err := c.get(ctx, "/repos/"+fullName+"/git/trees/"+url.PathEscape(ref)+"?recursive=1", &resp); err != nil {
		return nil, err
	}
	var out []File
	for _, e := range resp.Tree {
		if e.Type == "blob" {
			out = append(out, e.File)
		}
	}
	return out, nil
}

// Raw returns one file's contents at ref, at most limit bytes.
func (c *Client) Raw(ctx context.Context, fullName, path, ref string, limit int64) ([]byte, error) {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	u := c.base + "/repos/" + fullName + "/contents/" + strings.Join(segs, "/") + "?ref=" + url.QueryEscape(ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.raw")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s: larger than %d bytes", path, limit)
	}
	return b, nil
}

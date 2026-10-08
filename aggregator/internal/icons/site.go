package icons

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const maxPage = 512 << 10

var (
	linkTag = regexp.MustCompile(`(?is)<link\b[^>]*>`)
	attr    = regexp.MustCompile(`(?is)\b(rel|href|sizes|type)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
)

// iconLinks returns the icon hrefs a page declares, largest first. SVG is
// left out: the aggregator cannot rasterize it.
func iconLinks(page string) []string {
	type link struct {
		href string
		size int
	}
	var links []link
	for _, tag := range linkTag.FindAllString(page, -1) {
		a := map[string]string{}
		for _, m := range attr.FindAllStringSubmatch(tag, -1) {
			a[strings.ToLower(m[1])] = m[2] + m[3] + m[4]
		}
		rel := " " + strings.ToLower(a["rel"]) + " "
		href := strings.TrimSpace(a["href"])
		touch := strings.Contains(rel, " apple-touch-icon")
		if href == "" || !(touch || strings.Contains(rel, " icon ")) ||
			strings.Contains(a["type"], "svg") || strings.HasSuffix(strings.ToLower(href), ".svg") || strings.HasPrefix(href, "data:") {
			continue
		}
		size := 0
		if w, _, ok := strings.Cut(strings.ToLower(a["sizes"]), "x"); ok {
			size, _ = strconv.Atoi(w)
		} else if touch {
			size = 180 // the default apple-touch-icon size
		}
		links = append(links, link{href, size})
	}
	slices.SortStableFunc(links, func(a, b link) int { return b.size - a.size })
	out := make([]string, len(links))
	for i, l := range links {
		out[i] = l.href
	}
	return out
}

// fromSite returns the normalized icon of the site at siteURL: the largest
// declared icon that decodes, else /favicon.ico. Nil without error when the
// site has none.
func fromSite(ctx context.Context, hc *http.Client, siteURL string) ([]byte, error) {
	page, final, err := fetch(ctx, hc, siteURL, maxPage)
	if err != nil {
		return nil, err
	}
	candidates := append(iconLinks(string(page)), "/favicon.ico")
	for _, href := range candidates {
		u, err := final.Parse(href)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		b, _, err := fetch(ctx, hc, u.String(), maxFile)
		if err != nil {
			continue
		}
		if icon, err := normalize(b); err == nil {
			return icon, nil
		}
	}
	return nil, nil
}

// fetch GETs u, at most limit bytes, and reports the URL it ended up at.
func fetch(ctx context.Context, hc *http.Client, u string, limit int64) ([]byte, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "scouter-aggregator (project icons)")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(b)) > limit {
		return nil, nil, errors.New("too large")
	}
	return b, resp.Request.URL, nil
}

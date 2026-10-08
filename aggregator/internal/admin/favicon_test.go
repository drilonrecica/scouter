package admin

import (
	"encoding/base64"
	"html"
	"strings"
	"testing"
	"time"

	"github.com/drilonrecica/scouter/aggregator/internal/state"
)

func TestLEDFollowsThePhonesPriority(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	running := []state.Project{{CI: &state.Run{Status: state.CIRunning}}}
	waiting := []state.Agent{{State: state.AgentWaiting}}
	alerts := []state.Alert{{ID: "ci:me/a:1"}}
	fresh := &state.Heartbeat{At: now.Add(-time.Minute)}
	silent := &state.Heartbeat{At: now.Add(-10 * time.Minute)}

	for _, c := range []struct {
		name string
		st   state.State
		hb   *state.Heartbeat
		want string
	}{
		{"quiet", state.State{}, fresh, ledGreen},
		{"never seen a phone is not an outage", state.State{}, nil, ledGreen},
		{"running on the default branch", state.State{Projects: running}, fresh, ledAmber},
		{"running on another branch", state.State{Projects: []state.Project{{Latest: &state.Run{Status: state.CIRunning}}}}, fresh, ledAmber},
		{"waiting beats running", state.State{Projects: running, Agents: waiting}, fresh, ledBlue},
		{"alert beats waiting", state.State{Projects: running, Agents: waiting, Alerts: alerts}, fresh, ledRed},
		{"silent phone beats everything", state.State{Alerts: alerts}, silent, ledPurple},
	} {
		if got := led(c.st, c.hb, now); got != c.want {
			t.Errorf("%s: led = %s, want %s", c.name, got, c.want)
		}
	}
}

func TestFaviconIsAnInlineSVGWithTheDotColour(t *testing.T) {
	u := string(favicon(ledRed))
	b64, ok := strings.CutPrefix(u, "data:image/svg+xml;base64,")
	if !ok {
		t.Fatalf("favicon = %.40q…, want a base64 SVG data URL", u)
	}
	svg, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(svg), `fill="`+ledRed+`"`) {
		t.Errorf("dot colour %s missing from %s", ledRed, svg)
	}
}

// html/template writes the + in "svg+xml" as &#43;, which browsers decode.
func TestLoginShowsIdleFaviconAndPagesShowStatus(t *testing.T) {
	srv, store := newUI(t, pw)
	store.AgentEvent("s1", "/home/me/Code/scouter", "Notification")

	if _, body := get(t, client(t, srv), srv, "/admin/login"); !strings.Contains(html.UnescapeString(body), string(favicon(ledIdle))) {
		t.Error("login page should show the idle favicon, not the status one")
	}
	c, _ := login(t, srv)
	if _, body := get(t, c, srv, "/admin/"); !strings.Contains(html.UnescapeString(body), string(favicon(ledBlue))) {
		t.Error("status page should show the waiting (blue) favicon")
	}
}

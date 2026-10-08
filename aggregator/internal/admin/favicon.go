package admin

import (
	"encoding/base64"
	"html/template"
	"slices"
	"time"

	"github.com/drilonrecica/scouter/aggregator/internal/state"
)

// phoneSilentAfter is how long without a heartbeat before the phone counts as gone.
const phoneSilentAfter = 5 * time.Minute

// Dot colours: the phone's screen palette (DashboardView), white when idle.
const (
	ledIdle   = "#E8E8E8"
	ledGreen  = "#30D158"
	ledAmber  = "#FFB340"
	ledRed    = "#FF453A"
	ledBlue   = "#4DA3FF"
	ledPurple = "#B06BFF"
)

// led mirrors the phone's status light (Logic.led), seen from the server: the
// server cannot know which alerts were dismissed on the phone, so any alert is
// red, and "no connection" means the phone has stopped checking in.
func led(st state.State, hb *state.Heartbeat, now time.Time) string {
	running := func(r *state.Run) bool { return r != nil && r.Status == state.CIRunning }
	switch {
	case hb != nil && now.Sub(hb.At) > phoneSilentAfter:
		return ledPurple
	case len(st.Alerts) > 0:
		return ledRed
	case slices.ContainsFunc(st.Agents, func(a state.Agent) bool { return a.State == state.AgentWaiting }):
		return ledBlue
	case slices.ContainsFunc(st.Projects, func(p state.Project) bool { return running(p.CI) || running(p.Latest) }):
		return ledAmber
	}
	return ledGreen
}

// favicon is the brand mark's small cut (lens and dot, branding/favicon.svg)
// as a data URL, so each page carries the status it was rendered with and the
// script-free UI needs no extra route. color is always one of the led consts.
func favicon(color string) template.URL {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="20 20 68 68">` +
		`<path d="M30,38 L66,32 L79,45 L79,71 L30,73 Z" fill="#061412" stroke="#45B5A0" stroke-width="7.5"/>` +
		`<circle cx="55" cy="54" r="16" fill="` + color + `" opacity=".18"/>` +
		`<circle cx="55" cy="54" r="10.5" fill="` + color + `"/></svg>`
	return template.URL("data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg)))
}

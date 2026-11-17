package ui

import (
	"context"
	"encoding/json"
	"farshmac/internal/fsops"
	"farshmac/internal/podtoast"
	"fmt"
	"strings"
	"time"
)

type trafficUpdate struct {
	state podtoast.TrafficStatus
	err   error
}
type trafficMonitor struct {
	client      *podtoast.Client
	cancel      context.CancelFunc
	updates     chan trafficUpdate
	state       podtoast.TrafficStatus
	title, err  string
	offset      int
	changed     bool
	pending     bool
	actionError string
}

func (a *App) trafficForm(p *Panel) {
	ref, ok := podToastTarget(p)
	if !ok {
		return
	}
	k := p.Kube
	if a.traffic != nil {
		a.Status = "Stop the current traffic experiment first"
		return
	}
	a.form("Pod network monitor", []Field{
		{Label: "PodToast image", Value: defaultPodToastImage},
		{Label: "Attach network agent if needed (no traffic rules until explicitly enabled)", Toggle: true, Checked: true},
	}, func(f []Field) {
		image, attach := strings.TrimSpace(f[0].Value), f[1].Checked

		a.backgroundTask("Starting traffic monitor", func(o *fsops.Operation) (func(), func(), error) {
			client, e := k.client.OpenTraffic(o.Context(), ref, image, attach)
			if e != nil {
				return nil, nil, e
			}
			var state podtoast.TrafficStatus
			var capabilities podtoast.Capability
			if e = client.JSON(o.Context(), "GET", "/v1/hello", nil, &capabilities); e == nil && (!capabilities.InteractiveTraffic || !capabilities.PortTraffic || !capabilities.TrafficOverview) {
				e = fmt.Errorf("interactive monitoring requires PodToast 0.7.0 or newer")
			}
			if e != nil {
				client.Close()
				return nil, nil, e
			}
			if e = client.JSON(o.Context(), "GET", "/v1/traffic/overview", nil, &state); e != nil {
				client.Close()
				return nil, nil, e
			}
			adopted := false
			return func() {
					if p.Kube != k {
						return
					}
					adopted = true
					a.beginTraffic(client, ref.Namespace+"/"+ref.Pod, state)
				}, func() {
					if !adopted {
						client.Close()
					}
				}, nil
		})
		if a.task != nil {
			a.task.reportError = true
		}
	})
	a.Form.Primary = "Open monitor"
}
func (a *App) beginTraffic(client *podtoast.Client, title string, state podtoast.TrafficStatus) {
	ctx, cancel := context.WithCancel(context.Background())
	m := &trafficMonitor{client: client, cancel: cancel, updates: make(chan trafficUpdate, 1), state: state, title: title, changed: true}
	a.traffic = m
	if !a.TrafficOnly {
		a.detachTraffic = true
		a.NewWindow = true
	}
	a.TermVisible = false
	a.dirty = true
	go func() {
		defer client.Close()
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			request, stop := context.WithTimeout(ctx, 4*time.Second)
			var state podtoast.TrafficStatus
			e := client.JSON(request, "POST", "/v1/traffic/overview", nil, &state)
			stop()
			select {
			case m.updates <- trafficUpdate{state, e}:
			case <-ctx.Done():
				return
			}
			if e != nil {
				return
			}
		}
	}()
}
func (a *App) pollTraffic() {
	if m := a.traffic; m != nil {
		select {
		case u := <-m.updates:
			if u.err != nil {
				m.err = u.err.Error()
			} else {
				m.state = u.state
			}
			m.changed = true
			a.dirty = true
		default:
		}
	}
}
func (a *App) closeTraffic() {
	if a.traffic != nil {
		a.traffic.cancel()
		a.traffic.client.Close()
		a.traffic = nil
		a.dirty = true
	}
}
func (a *App) stopTraffic() {
	m := a.traffic
	if m == nil {
		return
	}
	if !m.state.Active || m.err != "" {
		a.closeTraffic()
		return
	}
	a.backgroundTask("Stopping traffic and removing PodToast rules", func(o *fsops.Operation) (func(), func(), error) {
		var st podtoast.TrafficStatus
		e := m.client.JSON(o.Context(), "POST", "/v1/traffic/stop", nil, &st)
		return func() { a.closeTraffic() }, func() {
			if a.traffic == m {
				a.closeTraffic()
			} else {
				m.cancel()
				m.client.Close()
			}
		}, e
	})
}
func (a *App) trafficKey(m Message) bool {
	if a.traffic == nil {
		return false
	}
	switch m.Key {
	case "Escape", "F10":
		a.stopTraffic()
	case "ArrowDown":
		a.traffic.offset++
	case "ArrowUp":
		a.traffic.offset = max(0, a.traffic.offset-1)
	case "Home":
		a.traffic.offset = 0
	}
	a.dirty = true
	return true
}
func (a *App) drawTraffic(g *Grid) {
	m := a.traffic
	if m == nil {
		return
	}
	st := m.state
	c := st.Config
	g.Fill(0, 0, g.W, g.H, LightCyan, Blue)
	g.Box(0, 0, g.W, g.H, LightCyan, Blue)
	g.Text(3, 0, " Traffic · "+m.title+" ", Yellow, Blue, g.W-6)
	lines := []string{fmt.Sprintf("%s TCP/%d · peer %s · expires %s", c.Direction, c.Port, c.PeerCIDR, st.Expires.Local().Format("15:04:05")), fmt.Sprintf("Delay %d ms · %d KiB/s/connection · loss %.2f%% · reset %d ms", c.DelayMS, c.RateKiB, c.LossPercent, c.ResetAfterMS), fmt.Sprintf("Connections %d · rejected %d · resets %d · sent %s · received %s", st.Accepted, st.Rejected, st.Resets, humanSize(int64(st.Sent)), humanSize(int64(st.Received))), "First HTTP/1 exchange; TLS opaque. Proxy: new connections. Loss: all matching packets."}
	if !st.Active && st.CleanupError == "" {
		lines = append(lines, "Experiment finished; traffic rules removed")
	}
	if st.CleanupError != "" {
		lines = append(lines, "Cleanup failed: "+st.CleanupError)
	}
	if m.err != "" {
		lines = append(lines, "Connection lost: "+m.err+" (watchdog cleanup / deadline applies)")
	}
	for i, line := range lines {
		g.Text(2, 2+i, line, LightCyan, Blue, g.W-4)
	}
	y := len(lines) + 3
	g.Text(2, y, "Source → destination · bytes out/in · HTTP", Yellow, Blue, g.W-4)
	y++
	m.offset = min(m.offset, max(0, len(st.Connections)-1))
	for i := m.offset; i < len(st.Connections) && y < g.H-2; i++ {
		e := st.Connections[len(st.Connections)-1-i]
		state := "open"
		if e.Closed {
			state = "closed"
		}
		line := fmt.Sprintf("%s → %s  %s/%s  %s %s %s [%s]", e.Client, e.Destination, humanSize(int64(e.Sent)), humanSize(int64(e.Received)), e.HTTPMethod, e.HTTPPath, e.HTTPStatus, state)
		if e.Error != "" {
			line += " · " + e.Error
		}
		g.Text(2, y, line, LightCyan, Blue, g.W-4)
		y++
	}
	g.Text(2, g.H-2, "[ Esc Stop / close ]   ↑↓ Scroll    TLS decryption disabled", Black, Cyan, g.W-4)
}

// Traffic windows own the transport after detaching. Neither their polling nor
// their controls depend on the file-manager window remaining open.
type trafficPacket struct {
	Title       string                 `json:"title"`
	State       podtoast.TrafficStatus `json:"state"`
	Error       string                 `json:"error,omitempty"`
	ActionError string                 `json:"actionError,omitempty"`
	Pending     bool                   `json:"pending"`
	Time        int64                  `json:"time"`
}

func (a *App) pollTrafficWindow() Update {
	a.pollTraffic()
	for {
		select {
		case fn := <-a.jobs:
			fn()
		default:
			goto drained
		}
	}
drained:
	u := Update{Mode: "traffic", Title: "Traffic monitor", Cols: a.Cols, Rows: a.Rows, FontSize: a.Settings.FontSize, Quit: a.Quit, Zoom: a.Zoom}
	a.Zoom = false
	if a.traffic != nil {
		u.Title = "Traffic · " + a.traffic.title
	}
	if m := a.traffic; m != nil && m.changed {
		m.changed = false
		u.Title = "Traffic · " + m.title
		u.Traffic = &trafficPacket{Title: m.title, State: m.state, Error: m.err, ActionError: m.actionError, Pending: m.pending, Time: time.Now().UnixMilli()}
	}
	return u
}
func (a *App) handleTrafficWindow(m Message) bool {
	if !a.TrafficOnly {
		return false
	}
	if m.Type == "resize" {
		a.Cols, a.Rows = max(60, min(300, m.Cols)), max(15, min(120, m.Rows))
	}
	if m.Type == "key" {
		if m.Key == "Escape" || m.Meta && strings.EqualFold(m.Key, "w") {
			a.RequestClose()
		}
		if m.Alt && m.Key == "F9" {
			a.Zoom = true
		}
	}
	if m.Type != "traffic" {
		return true
	}
	if m.Key == "close" {
		a.RequestClose()
		return true
	}
	t := a.traffic
	if t == nil || t.pending || t.err != "" {
		return true
	}
	var payload any
	endpoint := "/v1/traffic/action"
	switch m.Key {
	case "start":
		var cfg podtoast.TrafficConfig
		if json.Unmarshal([]byte(m.Text), &cfg) != nil {
			return true
		}
		cfg.Interactive = true
		cfg.DurationSeconds = 30
		if e := cfg.Validate(); e != nil {
			t.actionError = e.Error()
			t.changed = true
			return true
		}
		payload = cfg
		endpoint = "/v1/traffic/start"
	case "stop":
		endpoint = "/v1/traffic/stop"
	case "action":
		var action podtoast.TrafficAction
		if json.Unmarshal([]byte(m.Text), &action) != nil {
			return true
		}
		if e := action.Validate(); e != nil {
			t.actionError = e.Error()
			t.changed = true
			return true
		}
		payload = action
	default:
		return true
	}

	t.pending = true
	t.actionError = ""
	t.changed = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var st podtoast.TrafficStatus
		e := t.client.JSON(ctx, "POST", endpoint, payload, &st)
		apply := func() {
			if a.traffic != t {
				return
			}
			t.pending = false
			t.changed = true
			if e != nil {
				t.actionError = e.Error()
			}
		}
		select {
		case a.jobs <- apply:
		case <-time.After(time.Second):
		}
	}()
	return true
}

func (a *App) closeTrafficWindow() {
	m := a.traffic
	if m == nil || m.err != "" || !m.state.Active {
		a.Quit = true
		return
	}
	if a.Busy {
		return
	}
	a.Busy = true
	m.pending = true
	m.changed = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var st podtoast.TrafficStatus
		e := m.client.JSON(ctx, "POST", "/v1/traffic/stop", nil, &st)
		apply := func() {
			a.Busy = false
			if a.traffic != m {
				return
			}
			m.pending = false
			m.changed = true
			if e != nil {
				m.err = "Could not confirm cleanup: " + e.Error() + ". The watchdog will clean up after disconnect; close this window to disconnect."
				return
			}
			m.state = st
			a.Quit = true
		}
		select {
		case a.jobs <- apply:
		case <-time.After(time.Second):
		}
	}()
}

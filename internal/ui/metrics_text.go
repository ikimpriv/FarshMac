package ui

import (
	"fmt"
	"strings"

	"farshmac/internal/metrics"
)

type compactMetricsPanel struct {
	session    *metrics.Session
	title      string
	index      int
	previous   panelViewReturn
	enabled    [3]bool
	legendPage int
	paused     bool
	packet     *metrics.Packet
	selected   string
	memoryMode int
}

func basicMetric(name string) int {
	switch name {
	case "docker_container_cpu_usage_cores", "podtoast_cpu_usage_seconds_total", "container_cpu_usage_seconds_total", "kubernetes_container_cpu_usage_cores":
		return 0
	case "docker_container_memory_usage_bytes", "podtoast_memory_usage_bytes", "podtoast_memory_working_set_bytes", "podtoast_memory_anonymous_bytes", "podtoast_memory_file_cache_bytes", "container_memory_working_set_bytes", "container_memory_usage_bytes", "container_memory_cache", "kubernetes_container_memory_usage_bytes":
		return 1
	case "kubernetes_container_ephemeral_storage_used_bytes", "container_ephemeral_storage_used_bytes", "ephemeral_storage_used_bytes":
		return 2
	}
	return -1
}
func (a *App) startCompactMetrics(session *metrics.Session, title string) {
	a.stopCompactMetrics(true)
	index := 1 - a.Active
	p := &a.Panels[index]
	old := panelViewReturn{p.View, p.Hidden}
	p.cancelPanelLoad()
	p.cancelTree()
	p.rememberFileView()
	p.View = "Quick view"
	p.Hidden = false
	a.compactMetrics = &compactMetricsPanel{session: session, title: title, index: index, previous: old, enabled: [3]bool{true, true, true}}
	if a.previewOperation != nil {
		a.previewOperation.Cancel()
	}
	a.previewKey = ""
	a.previewImage = ""
	a.previewText = ""
	a.TermVisible = false
	a.dirty = true
}
func (a *App) stopCompactMetrics(restore bool) {
	c := a.compactMetrics
	if c == nil {
		return
	}
	c.session.Close()
	a.compactMetrics = nil
	if restore {
		a.Panels[c.index].View = c.previous.View
		a.Panels[c.index].Hidden = c.previous.Hidden
	}
	a.previewKey = ""
	a.dirty = true
}
func (a *App) pollCompactMetrics() {
	c := a.compactMetrics
	if c == nil {
		return
	}
	if a.Panels[c.index].View != "Quick view" {
		a.stopCompactMetrics(false)
		return
	}
	packet := c.session.Packet(false)
	if packet == nil {
		return
	}
	if packet.Catalog != nil {
		keys := quickSeriesIDsForMemory(packet.Catalog, c.memoryMode)
		signature := strings.Join(keys, "\x00")
		if c.selected != signature {
			c.selected = signature
			c.session.Configure(keys, "")
			packet = c.session.Packet(true)
		}
	}
	c.packet = packet
	a.dirty = true
}
func (a *App) compactMetricsKey(m Message) bool {
	c := a.compactMetrics
	if c == nil || a.Active != c.index || m.Ctrl || m.Meta || m.Alt {
		return false
	}
	switch m.Key {
	case "PageDown":
		c.legendPage++
	case "PageUp":
		c.legendPage = max(0, c.legendPage-1)
	case "1", "2", "3":
		i := int(m.Key[0] - '1')
		c.enabled[i] = !c.enabled[i]
	case "m", "M":
		a.chooseQuickMemory()
	case "p", "P":
		c.paused = !c.paused
		c.session.Pause(c.paused)
	case "Escape":
		a.stopCompactMetrics(true)
	default:
		return false
	}
	a.dirty = true
	return true
}
func (a *App) compactMetricsClick(m Message) bool {
	c := a.compactMetrics
	if c == nil || a.panelAt(m.X, m.Y) != c.index {
		return false
	}
	a.Active = c.index
	row := m.Y - 1
	if row == 3 {
		a.chooseQuickMemory()
		return true
	}
	if row == 2 {
		x, w := a.panelRect(c.index)
		group := min(2, max(0, (m.X-x-2)*3/max(1, w-4)))
		a.compactMetricsKey(Message{Key: string(rune('1' + group))})
		return true
	}
	if row == a.Rows-6 {
		x, w := a.panelRect(c.index)
		if m.X < x+w/2 {
			a.compactMetricsKey(Message{Key: "p"})
		} else {
			a.compactMetricsKey(Message{Key: "Escape"})
		}
	}
	a.dirty = true
	return true
}
func compactSeriesValue(s metrics.Series, p metrics.Point) *float64 {
	if s.Kind == "counter" {
		return p.Rate
	}
	return p.Value
}
func (a *App) drawCompactMetrics(g *Grid, index, x, y, w, h int) bool {
	c := a.compactMetrics
	if c == nil || index != c.index {
		return false
	}
	g.Box(x, y, w, h, LightCyan, Blue)
	g.Text(x+2, y, " Quick Live Metrics ", Yellow, Blue, w-4)
	g.Text(x+2, y+1, c.title, LightCyan, Blue, w-4)
	for i, name := range []string{"CPU", "Memory", "Storage"} {
		mark := "x"
		if !c.enabled[i] {
			mark = " "
		}
		g.Text(x+2+i*(w-4)/3, y+2, fmt.Sprintf("%d [%s] %s", i+1, mark, name), Yellow, Blue, (w-4)/3)
	}
	g.Text(x+2, y+3, "[ M Memory: "+quickMemoryNames[c.memoryMode]+" ]", Yellow, Blue, w-4)
	pause := "Pause"
	if c.paused {
		pause = "Resume"
	}
	g.Text(x+2, y+h-2, "[ P "+pause+" ]", Black, Cyan, max(1, w/2-3))
	g.Text(x+w/2, y+h-2, "[ Esc Close ]", Black, Cyan, max(1, w/2-2))
	return true
}

var quickMemoryNames = []string{"Working set", "Applications (anonymous)", "File cache", "Total"}

func (a *App) chooseQuickMemory() {
	c := a.compactMetrics
	if c == nil {
		return
	}
	a.choose("Memory metric", quickMemoryNames, func(i int) {
		if a.compactMetrics != c {
			return
		}
		c.memoryMode = i
		packet := c.session.Packet(true)
		if packet != nil {
			keys := quickSeriesIDsForMemory(packet.Catalog, i)
			c.selected = strings.Join(keys, "\x00")
			c.session.Configure(keys, "")
			c.packet = c.session.Packet(true)
		}
		a.dirty = true
	})
	a.Form.Focus = c.memoryMode
}

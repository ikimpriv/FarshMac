package ui

import (
	"farshmac/internal/sysinfo"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type infoRow struct {
	Label, Value string
	Heading      bool
}
type infoResult struct {
	rows        []infoRow
	description string
}
type infoJob struct {
	key  string
	done chan infoResult
}

func (a *App) updateInfoPanels() {
	if a.TermVisible {
		return
	}
	for i := range a.Panels {
		p := &a.Panels[i]
		if p.View != "Info panel" || p.Hidden {
			continue
		}
		source := &a.Panels[1-i]
		if source.View == "Info panel" {
			source = p
		}
		path := source.filesystemPath()
		plugin := source.backend().InfoPlugin(source)
		settings := a.Settings
		key := fmt.Sprintf("%s|%s|%t%t%t%t%t%t|%s", path, plugin, settings.InfoHost, settings.InfoVolume, settings.InfoMemory, settings.InfoPower, settings.InfoDescription, settings.HumanReadableSizes, settings.FolderDescriptions)
		if job := a.infoJobs[i]; job != nil {
			select {
			case result := <-job.done:
				a.infoJobs[i] = nil
				if job.key == key {
					p.infoRows = result.rows
					a.infoDescriptionPaths[i] = result.description
					a.infoKeys[i] = key
					a.infoNext[i] = time.Now().Add(2 * time.Second)
					a.dirty = true
				}
			default:
			}
		}
		if a.infoJobs[i] != nil {
			continue
		}
		if key == a.infoKeys[i] && time.Now().Before(a.infoNext[i]) {
			continue
		}
		job := &infoJob{key: key, done: make(chan infoResult, 1)}
		a.infoJobs[i] = job
		// One bounded worker per info panel; no statfs, IOKit, or file IO in Draw.
		go func() { job.done <- readInfoPanel(path, plugin, settings) }()
	}
}
func readInfoPanel(path, plugin string, s Settings) infoResult {
	v := sysinfo.Read(path)
	var r infoResult
	row := func(k, val string) { r.rows = append(r.rows, infoRow{Label: k, Value: val}) }
	section := func(title string) { r.rows = append(r.rows, infoRow{Label: title, Heading: true}) }
	size := func(n uint64) string {
		if s.HumanReadableSizes {
			return panelSize(int64(n), true)
		}
		v := fmt.Sprint(n)
		for i := len(v) - 3; i > 0; i -= 3 {
			v = v[:i] + " " + v[i:]
		}
		return v
	}
	if s.InfoHost {
		row("Computer name", v.Host)
		row("User name", v.User)
	}
	if s.InfoVolume {
		section("Disk " + v.Mount + " (" + v.Filesystem + ")")
		row("Total bytes", size(v.DiskTotal))
		row("Free bytes", size(v.DiskFree))
		row("Volume label", v.Volume)
		row("Volume UUID", v.UUID)
		row("Device", v.Device)
	}
	if s.InfoMemory {
		section("Memory")
		if v.Total > 0 {
			row("Memory used", fmt.Sprintf("%.0f%%", 100*float64(v.Total-min(v.Total, v.Available))/float64(v.Total)))
		}
		row("Installed memory", size(v.Total))
		row("Free physical", size(v.Free))
		row("Available (estimate)", size(v.Available))
		row("Compressed", size(v.Compressed))
		row("Swap total", size(v.SwapTotal))
		row("Swap free", size(v.SwapFree))
	}
	if plugin != "" {
		section("Filesystem panel")
		row(plugin, "")
	}
	if s.InfoPower {
		section("Power status")
		row("Power source", v.Power)
		row("Charge status", v.Charge)
		if v.Percent >= 0 {
			row("Battery", fmt.Sprintf("%d%%", v.Percent))
		}
		if v.Minutes > 0 {
			row("Time remaining", fmt.Sprintf("%dh %dm", v.Minutes/60, v.Minutes%60))
		}
	}
	if s.InfoDescription && plugin == "" {
		section("Folder description")
		for _, name := range strings.Split(s.FolderDescriptions, ",") {
			name = strings.TrimSpace(name)
			if name == "" || filepath.Base(name) != name {
				continue
			}
			candidate := filepath.Join(path, name)
			if r.description == "" {
				r.description = candidate
			}
			info, err := os.Stat(candidate)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			f, err := os.Open(candidate)
			if err != nil {
				continue
			}
			data, err := io.ReadAll(io.LimitReader(f, 4096))
			f.Close()
			if err != nil {
				continue
			}
			r.description = candidate
			row(name, "")
			for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
				row(line, "")
			}
			break
		}
	}
	return r
}
func (a *App) drawInfoPanel(g *Grid, index, x, y, w, h int) {
	p := &a.Panels[index]
	if len(p.infoRows) == 0 {
		g.Text(x+2, y+2, "Reading information…", LightCyan, Blue, w-4)
		return
	}
	p.InfoOffset = max(0, min(p.InfoOffset, max(0, len(p.infoRows)-(h-2))))
	for n := 0; n < h-2 && n+p.InfoOffset < len(p.infoRows); n++ {
		r := p.infoRows[n+p.InfoOffset]
		yy := y + 1 + n
		if r.Heading {
			for xx := x + 1; xx < x+w-1; xx++ {
				g.Set(xx, yy, "─", LightCyan, Blue)
			}
			title := " " + r.Label + " "
			g.Text(x+max(2, (w-len([]rune(title)))/2), yy, title, LightCyan, Blue, w-4)
			continue
		}
		if r.Value == "" {
			g.Text(x+2, yy, r.Label, LightCyan, Blue, w-4)
			continue
		}
		valueWidth := min(len([]rune(r.Value)), max(8, w-17))
		g.Text(x+2, yy, r.Label, LightCyan, Blue, max(0, w-valueWidth-5))
		g.Text(x+w-2-valueWidth, yy, tail(r.Value, valueWidth), Yellow, Blue, valueWidth)
	}
	if p.InfoOffset > 0 {
		g.Set(x+w-1, y+1, "▲", Yellow, Blue)
	}
	if p.InfoOffset+h-2 < len(p.infoRows) {
		g.Set(x+w-1, y+h-2, "▼", Yellow, Blue)
	}
}
func (a *App) infoPanelKey(m Message) bool {
	p := &a.Panels[a.Active]
	if p.View != "Info panel" {
		return false
	}
	if (m.Ctrl || m.Meta) && m.Key == "F12" {
		a.settingsMenu("Info panel settings")
		return true
	}
	if m.Ctrl || m.Meta || m.Alt {
		return false
	}
	switch m.Key {
	case "ArrowDown":
		p.InfoOffset++
	case "ArrowUp":
		p.InfoOffset = max(0, p.InfoOffset-1)
	case "PageDown":
		p.InfoOffset += max(1, a.Rows-7)
	case "PageUp":
		p.InfoOffset = max(0, p.InfoOffset-max(1, a.Rows-7))
	case "Home":
		p.InfoOffset = 0
	case "End":
		p.InfoOffset = len(p.infoRows)
	case "F3", "F4":
		path := a.infoDescriptionPaths[a.Active]
		if path != "" {
			a.launch(path, a.viewPlan(path, m.Key == "F4", false, false))
		}
	default:
		return false
	}
	a.dirty = true
	return true
}

func (a *App) infoPanelClick(m Message) bool {
	i := a.panelAt(m.X, m.Y)
	if i < 0 || a.Panels[i].View != "Info panel" {
		return false
	}
	a.Active = i
	p := &a.Panels[i]
	row := m.Y - 2 + p.InfoOffset
	description := false
	for j, r := range p.infoRows {
		if j > row {
			break
		}
		if r.Heading {
			description = r.Label == "Folder description"
		}
	}
	if description && a.infoDescriptionPaths[i] != "" && (m.Button == 0 || m.Button == 2) {
		path := a.infoDescriptionPaths[i]
		a.launch(path, a.viewPlan(path, m.Button == 2, false, false))
	}
	return true
}

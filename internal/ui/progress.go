package ui

import (
	"farshmac/internal/fsops"
	"fmt"
	"strings"
	"time"
)

func (a *App) workProgress(label string, fn func(*fsops.Operation) error) {
	a.backgroundTask(label, func(o *fsops.Operation) (func(), func(), error) {
		err := fn(o)
		return func() { a.Status = "Done" }, func() {
			if !a.closed {
				a.refresh()
			}
		}, err
	})
	if a.task != nil {
		a.task.reportError = a.Panels[0].Remote != nil || a.Panels[1].Remote != nil
	}
}
func (a *App) cancelOperation() {
	if a.Operation == nil {
		return
	}
	a.Operation.Cancel()
	// Release an outstanding overwrite question too. Its worker also observes
	// the cancellation channel, including when the window is closed.
	if a.Form != nil && (a.Form.Title == "Overwrite" || a.Form.Title == "Cannot preserve metadata") {
		a.Form = nil
	}
	a.Status = "Cancelling…"
	a.dirty = true
}
func progressBar(done, total int64, width int) string {
	width = max(1, width)
	n := 0
	if total > 0 {
		n = int(min(float64(width), max(0, float64(done)/float64(total)*float64(width))))
	}
	return strings.Repeat("█", n) + strings.Repeat("░", width-n)
}
func (a *App) progressBounds() modalRect { return modalBounds(a.Cols, a.Rows, 88, 15) }
func (a *App) drawProgress(g *Grid) {
	if a.Operation == nil {
		return
	}
	p := a.Operation.Snapshot()
	r := a.progressBounds()
	x, y, w := r.X, r.Y, r.W
	drawModal(g, r, p.Label)
	totalY, fileY, statsY := y+4, y+7, y+9
	if r.H < 15 {
		totalY, fileY, statsY = y+3, y+5, y+7
	}
	g.Text(x+4, y+2, tail(p.Path, w-8), Black, Gray, w-8)
	if p.Scanning || p.Streaming {
		frames := []string{"▰▱▱▱", "▱▰▱▱", "▱▱▰▱", "▱▱▱▰"}
		frame := frames[int(time.Since(p.Started).Milliseconds()/150)%len(frames)]
		g.Text(x+4, totalY, fmt.Sprintf("Working / calculating… %s  %d items", frame, p.TotalFiles), Black, Gray, w-8)
		if p.Streaming {
			g.Text(x+4, fileY, "File  "+progressBar(p.FileBytes, p.FileSize, w-15), Blue, Gray, w-8)
		}
	} else {
		done, total := p.Bytes, p.TotalBytes
		if total == 0 {
			done, total = p.Files, p.TotalFiles
		}
		pct := float64(0)
		if total > 0 {
			pct = min(100, float64(done)*100/float64(total))
		}
		g.Text(x+4, totalY, fmt.Sprintf("Total %5.1f%%  %d / %d items", pct, p.Files, p.TotalFiles), Black, Gray, w-8)
		g.Text(x+4, totalY+1, progressBar(done, total, w-8), Blue, Gray, w-8)
		g.Text(x+4, fileY, "File  "+progressBar(p.FileBytes, p.FileSize, w-15), Blue, Gray, w-8)
	}
	elapsed := time.Since(p.Started).Seconds()
	if p.Workers > 1 {
		g.Text(x+4, fileY, fmt.Sprintf("Parallel transfers: %d active / %d workers", p.Active, p.Workers), Blue, Gray, w-8)
	}
	rate := float64(p.Bytes) / max(0.001, elapsed)
	info := fmt.Sprintf("%s / %s  ·  %s/s  ·  %.0fs", humanSize(p.Bytes), humanSize(p.TotalBytes), humanSize(int64(rate)), elapsed)
	if p.Streaming {
		info = fmt.Sprintf("%s processed · %s/s · %.0fs", humanSize(p.Bytes), humanSize(int64(rate)), elapsed)
	}
	if rate > 0 && p.TotalBytes > p.Bytes && !p.Scanning && !p.Streaming {
		info += fmt.Sprintf("  ETA %.0fs", float64(p.TotalBytes-p.Bytes)/rate)
	}
	g.Text(x+4, statsY, info, Black, Gray, w-8)
	modalRule(g, r, y+r.H-5)
	drawModalButtons(g, r, y+r.H-4, []string{"Cancel"}, 0, []rune{'c'})
}

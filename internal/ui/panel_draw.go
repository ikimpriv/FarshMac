package ui

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
)

func panelColumns(p *Panel) int {
	switch p.View {
	case "Brief":
		return 3
	case "Medium":
		return 2
	}
	return 1
}

// Column geometry belongs to the view, not to an individual row's values.
func panelExtraWidth(view string, width int) int {
	preferred := 24 // size (9), space, modified timestamp (14)
	switch view {
	case "Wide":
		preferred = 9
	case "File links":
		preferred = 15 // size, space, uint16 link count
	case "File owners":
		preferred = 20 // size, space, uint32 UID
	case "Detailed":
		preferred = 72
	case "Descriptions", "Long descriptions":
		preferred = width / 2
	}
	return min(preferred, max(0, width/2))
}
func (a *App) panelRect(index int) (x, w int) {
	full := a.Panels[a.Active].View == "Detailed" || a.Panels[a.Active].View == "Long descriptions"
	other := a.Panels[1-a.Active]
	if !other.Hidden && (other.View == "Quick view" || other.View == "Info panel" || other.View == "Tree panel") {
		full = false
	}
	if full && !a.Panels[a.Active].Hidden {
		if index == a.Active {
			return 0, a.Cols
		}
		return 0, 0
	}
	split := a.Cols / 2
	if a.panelSplit != 0 {
		split = a.Cols * a.panelSplit / 100
	}
	if index == 0 {
		return 0, split
	}
	return split, a.Cols - split
}
func (a *App) drawPanel(g *Grid, index, x, y, w, h int) {
	p := &a.Panels[index]
	if p.Hidden || w < 1 {
		g.Fill(x, y, w, h, Gray, Black)
		return
	}
	g.Box(x, y, w, h, LightCyan, Blue)
	label := p.label() + p.Path
	if p.View == "Info panel" {
		label = "Information"
	}
	if p.load != nil {
		label += " …"
	}
	title := " " + tail(label, w-8) + " "
	fg, bg := uint32(LightCyan), uint32(Blue)
	if index == a.Active {
		fg, bg = Black, Cyan
	}
	g.Text(x+(w-len([]rune(title)))/2, y, title, fg, bg, w-4)
	if p.View == "Info panel" {
		a.drawInfoPanel(g, index, x, y, w, h)
		return
	}
	if p.View == "Quick view" {
		lines := strings.Split(a.previewText, "\n")
		for row, line := range lines {
			if row >= h-3 {
				break
			}
			g.Text(x+2, y+2+row, line, LightCyan, Blue, w-4)
		}
		return
	}
	rows := max(1, h-5)
	columns := panelColumns(p)
	capacity := rows * columns
	cw := (w - 2) / columns
	if p.Cursor < p.Offset {
		p.Offset = p.Cursor
	}
	if p.Cursor >= p.Offset+capacity {
		p.Offset = p.Cursor - capacity + 1
	}
	p.Offset = max(0, p.Offset)
	for c := 0; c < columns; c++ {
		xx := x + 1 + c*cw
		width := cw
		if c == columns-1 {
			width = x + w - 1 - xx
		}
		heading := "Name"
		switch p.View {
		case "Descriptions", "Long descriptions":
			heading = "Name / Description"
		case "File owners":
			heading = "Name / Size / UID"
		case "File links":
			heading = "Name / Size / Hard links"
		case "Detailed":
			heading = "Name / Size / Modified / Accessed / Created / Mode"
		case "Tree panel":
			heading = "Folder tree"
		default:
			if columns == 1 {
				heading = "Name / Size / Modified"
			}
		}
		if p.Kube != nil && columns == 1 && p.View != "Tree panel" {
			heading = "Name / Kind / Status / Age"
			if p.View == "Detailed" {
				heading = "Name / Kind / Status / Created"
			}
			if p.kubeLevel().Level == kubeContainerFS {
				heading = "Name / Size / Modified"
			}
		}
		g.Text(xx+1, y+1, heading, Yellow, Blue, width-2)
		sizeWidth := max(9, len(panelSize(p.TotalBytes, a.Settings.HumanReadableSizes)))
		for _, r := range p.folderSizes {
			n := len(panelSize(r.Bytes, a.Settings.HumanReadableSizes))
			if r.Unreadable > 0 {
				n += 2
			}
			if r.Stale {
				n++
			}
			sizeWidth = max(sizeWidth, n)
		}
		preferred := panelExtraWidth(p.View, width) + sizeWidth - 9
		if p.Kube != nil && p.kubeLevel().Level != kubeContainerFS {
			preferred = 30 // kind (11), status (12), age (5)
			switch p.View {
			case "Wide":
				preferred = 12
			case "Detailed":
				preferred = 46
			}
			preferred = min(preferred, max(0, width*2/3))
		}
		extraWidth := min(width-4, max(sizeWidth, preferred))
		nameWidth := max(0, width-extraWidth-3)
		metadata := columns == 1 && p.View != "Tree panel" && width >= 12
		for row := 0; row < rows; row++ {
			i := p.Offset + c*rows + row
			yy := y + 2 + row
			if c > 0 {
				g.Set(xx-1, yy, "│", LightCyan, Blue)
			}
			if metadata {
				g.Set(xx+nameWidth+1, yy, "│", LightCyan, Blue)
			}
			if i >= len(p.Entries) {
				continue
			}
			e := p.Entries[i]
			fg, bg := uint32(LightCyan), uint32(Blue)
			if e.Dir {
				fg = White
			} else if a.Settings.Highlight && e.Info != nil {
				if e.Info.Mode()&os.ModeSymlink != 0 {
					fg = Cyan
				} else if e.Info.Mode()&0111 != 0 {
					fg = 0x55ff55
				} else if matchesMask(e.Name, "<arc>", a.Settings.MaskGroups) {
					fg = 0xff55ff
				}
			}
			if p.Marked[e.Name] {
				fg = Yellow
			}
			if i == p.Cursor && index == a.Active {
				fg, bg = Black, Cyan
				if p.Marked[e.Name] {
					fg = Yellow
				}
				g.Fill(xx, yy, width, 1, fg, bg)
			}
			marker := " "
			if p.Marked[e.Name] {
				marker = "√"
			}
			g.Text(xx, yy, marker, fg, bg, 1)
			name := e.Name
			if p.View == "Tree panel" && name != ".." {
				name = p.treeLabels[e.Name]
			}
			if !metadata {
				g.Text(xx+1, yy, name, fg, bg, width-2)
				continue
			}
			size, date := "<DIR>", ""
			if e.Info != nil && !e.Info.ModTime().IsZero() {
				date = e.Info.ModTime().Format("02.01.06 15:04")
			}
			if k, ok := e.Info.(*kubeEntry); ok {
				g.Text(xx+1, yy, name, fg, bg, nameWidth)
				g.Set(xx+nameWidth+1, yy, "│", LightCyan, bg)
				g.Text(xx+nameWidth+2, yy, k.panelColumns(p.View), fg, bg, width-nameWidth-2)
				continue
			} else if e.Info != nil {
				if !e.Dir {
					size = panelSize(e.Info.Size(), a.Settings.HumanReadableSizes)
				}
				if e.Info.Mode()&os.ModeSymlink != 0 {
					size = "<LINK>"
				}
			}
			if result, ok := p.folderSizes[e.Name]; e.Dir && ok {
				size = panelSize(result.Bytes, a.Settings.HumanReadableSizes)
				if result.Unreadable > 0 {
					size = ">=" + size
				}
				if result.Stale {
					size = "~" + size
				}
			}
			extra := fmt.Sprintf("%*s %s", sizeWidth, size, date)
			switch p.View {
			case "Wide":
				extra = fmt.Sprintf("%*s", sizeWidth, size)
			case "Descriptions":
				extra = p.Descriptions[e.Name]
			case "Long descriptions":
				extra = fmt.Sprintf("%*s %s", sizeWidth, size, p.Descriptions[e.Name])
			case "File owners", "File links":
				// Legacy views retained for old sessions, with their fixed columns.
				if len(size) > sizeWidth && e.Info != nil {
					size = humanSize(e.Info.Size())
				}
				value := "-"
				if e.Info != nil {
					if st, ok := e.Info.Sys().(*syscall.Stat_t); ok {
						if p.View == "File owners" {
							value = fmt.Sprint(st.Uid)
						} else {
							value = fmt.Sprint(st.Nlink)
						}
					}
				}
				digits := 5
				if p.View == "File owners" {
					digits = 10
				}
				extra = fmt.Sprintf("%*s %*s", sizeWidth, size, digits, value)
			case "Detailed":
				if e.Info != nil {
					st, _ := e.Info.Sys().(*syscall.Stat_t)
					extra = fmt.Sprintf("%*s %s %s", sizeWidth, size, date, e.Info.Mode())
					if st != nil {
						extra += fmt.Sprintf(" A:%d C:%d", st.Atimespec.Sec, st.Birthtimespec.Sec)
					}
				}
			}
			g.Text(xx+1, yy, name, fg, bg, nameWidth)
			g.Set(xx+nameWidth+1, yy, "│", LightCyan, bg)
			g.Text(xx+nameWidth+2, yy, extra, fg, bg, width-nameWidth-2)
		}
	}
	if p.load != nil {
		a.drawReading(g, p, x, y, w, h)
	}
	divider := y + h - 3
	for col := x + 1; col < x+w-1; col++ {
		g.Set(col, divider, "─", LightCyan, Blue)
	}
	g.Set(x, divider, "╟", LightCyan, Blue)
	g.Set(x+w-1, divider, "╢", LightCyan, Blue)
	marked := 0
	for _, v := range p.Marked {
		if v {
			marked++
		}
	}
	status := fmt.Sprintf("%s in %d items", panelSizeLabel(p.TotalBytes, a.Settings.HumanReadableSizes), p.ItemCount)
	if marked > 0 {
		status = fmt.Sprintf("%d selected · %s", marked, status)
	}
	status = p.filterFooter(status)
	if p.Err != "" {
		status = p.Err
	}
	// The tree keeps its scan summary in Err; a real error or a running
	// read still takes precedence over the calculated folder size.
	if size, ok := p.folderSizeFooter(a.Settings.HumanReadableSizes); ok && p.load == nil && p.tree == nil && (p.Err == "" || p.View == "Tree panel") {
		status = size
	}
	g.Text(x+2, divider+1, status, LightCyan, Blue, w-4)
}
func panelSize(n int64, human bool) string {
	if human {
		return humanSize(n)
	}
	return fmt.Sprint(n)
}
func panelSizeLabel(n int64, human bool) string {
	if human {
		return humanSize(n)
	}
	return fmt.Sprintf("%d bytes", n)
}
func humanSize(n int64) string {
	units := []string{"B", "K", "M", "G", "T"}
	f := float64(n)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprint(n)
	}
	return fmt.Sprintf("%.1f%s", f, units[i])
}
func (p *Panel) loadInfo() {
	p.TotalBytes = 0
	p.ItemCount = 0
	for _, e := range p.Entries {
		if e.Name == ".." {
			continue
		}
		p.ItemCount++
		if !e.Dir && e.Info != nil {
			p.TotalBytes += e.Info.Size()
		}
	}
	p.InfoText = fmt.Sprintf("%s\n\n%d entries\n%s in files\n", p.Path, p.ItemCount, panelSizeLabel(p.TotalBytes, p.HumanReadableSizes))
}

// drawReading overlays a progress line while the folder is being read, so a
// slow source (a cluster, a server) is visibly busy and Esc visibly stops it.
func (a *App) drawReading(g *Grid, p *Panel, x, y, w, h int) {
	spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	elapsed := time.Since(p.load.started)
	frame := spinner[int(elapsed/(100*time.Millisecond))%len(spinner)]
	what := "folder"
	if p.Kube != nil {
		what = "cluster"
	} else if p.Remote != nil {
		what = "server"
	}
	text := fmt.Sprintf(" %s Reading %s… %ds · Esc stops ", frame, what, int(elapsed.Seconds()))
	if elapsed < 300*time.Millisecond && p.Kube == nil {
		return // Local folders usually finish before the overlay is worth drawing.
	}
	width := min(w-4, len([]rune(text)))
	row := y + 2 + max(1, h-5)/2
	g.Fill(x+(w-width)/2, row, width, 1, Black, Cyan)
	g.Text(x+(w-width)/2, row, text, Black, Cyan, width)
}

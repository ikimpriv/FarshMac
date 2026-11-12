package ui

import (
	"fmt"
)

func (a *App) Draw() *Grid {
	g := NewGrid(a.Cols, a.Rows)
	a.recordStatus()
	g.Fill(0, 0, g.W, 1, Black, Cyan)
	for _, section := range a.menuSections() {
		g.Text(section.X, 0, section.Name, Black, Cyan, len(section.Name))
	}
	brand := "FarshMac 0.20.0"
	if g.W >= 70 {
		g.Text(g.W-len(brand)-2, 0, brand, Black, Cyan, len(brand))
	}
	if a.Doc == nil {
		for i := range a.Panels {
			x, w := a.panelRect(i)
			if w > 0 {
				a.drawPanel(g, i, x, 1, w, g.H-4)
			}
		}
	}
	g.Fill(0, g.H-3, g.W, 2, Gray, Black)
	status := a.Status
	if status == "" {
		status = "Tab Switch panel   Space Select   ⌘O Terminal   ⌘1…9 Function keys"
	}
	g.Text(0, g.H-3, fitStatus(status, g.W), Gray, Black, g.W)
	promptPath := a.Panels[a.Active].Path
	if a.Panels[a.Active].Remote != nil {
		promptPath = "local:" + a.Panels[a.Active].filesystemPath()
	}
	prompt := promptPath + ">" + a.Command
	g.Text(0, g.H-2, tail(prompt, g.W-1), Gray, Black, g.W-1)
	cursor := min(len([]rune(prompt)), g.W-1)
	g.Set(cursor, g.H-2, "▌", Gray, Black)
	labels := []string{"Help", "UserMenu", "View", "Edit", "Copy", "RenMov", "MkFold", "Trash", "Menu", ""}
	if a.Panels[a.Active].Remote != nil {
		labels[7] = "Delete"
	}
	if a.Panels[a.Active].Archive != nil {
		// Archives are read-only: F5 extracts, F4/F6/F7/F8 only explain that.
		labels[3], labels[4], labels[5], labels[6], labels[7] = "", "Extract", "", "", ""
	}
	if a.Settings.ShowKeys {
		for i, label := range labels {
			x := i * g.W / 10
			end := (i + 1) * g.W / 10
			key := fmt.Sprint(i + 1)
			g.Fill(x, g.H-1, end-x, 1, Black, Cyan)
			g.Text(x, g.H-1, key, Gray, Black, len(key))
			g.Text(x+len(key), g.H-1, label, Black, Cyan, end-x-len(key))
		}
	}
	if a.Doc != nil {
		a.drawDoc(g)
	}
	a.drawFastFind(g)
	if a.Menu != nil {
		a.drawMenu(g)
	}
	a.drawProgress(g)
	if a.Dialog != nil {
		a.drawDialog(g)
	}
	if a.Form != nil {
		a.drawForm(g)
	}
	if a.Settings.ColorTheme == "Dark" || a.Settings.ColorTheme == "Light" {
		palette := map[uint32]uint32{Blue: 0x151b24, LightCyan: 0xc7d7e9, Cyan: 0x486987}
		if a.Settings.ColorTheme == "Light" {
			palette = map[uint32]uint32{Blue: 0xf1f3f5, LightCyan: 0x182b49, White: 0x000000, Yellow: 0x885000, Cyan: 0x80b8dd,
				// Syntax and marker colors chosen for dark backgrounds.
				0x55ff55: 0x1d6f1d, 0xaaaaaa: 0x6a6a6a, 0xff88ff: 0x8a2a8a, 0xff8888: 0xa02020}
		}
		for i := range g.Cells {
			c := &g.Cells[i]
			if v, ok := palette[c.FG]; ok {
				c.FG = v
			}
			if v, ok := palette[c.BG]; ok {
				c.BG = v
			}
		}
	}
	return g
}
func (a *App) promptBounds() modalRect {
	width := min(88, a.Cols-4)
	lines := max(1, min(3, (len([]rune(a.Dialog.Message))+width-9)/(width-8)))
	return modalBounds(a.Cols, a.Rows, width, lines+8)
}
func (a *App) drawDialog(g *Grid) {
	d := a.Dialog
	r := a.promptBounds()
	drawModal(g, r, d.Title)
	msg := []rune(d.Message)
	lineWidth := r.W - 8
	for line := 0; line < 3 && line*lineWidth < len(msg); line++ {
		start := line * lineWidth
		g.Text(r.X+4, r.Y+2+line, string(msg[start:min(len(msg), start+lineWidth)]), Black, Gray, lineWidth)
	}
	modalInput(g, r.X+4, r.Y+r.H-6, r.W-8, d.Input, d.caret(), d.Focus == 0, d.Selected)
	modalRule(g, r, r.Y+r.H-4)
	drawModalButtons(g, r, r.Y+r.H-3, []string{"OK", "Cancel"}, d.Focus-1, mnemonicKeys([]string{"OK", "Cancel"}))
}
func (a *App) promptClick(m Message) {
	r := a.promptBounds()
	if m.Y == r.Y+r.H-6 && m.X >= r.X+4 && m.X < r.X+r.W-4 {
		a.Dialog.Focus = 0
		return
	}
	for i, pos := range buttonLayout([]string{"OK", "Cancel"}, r.W) {
		if m.Y == r.Y+r.H-3+pos.Y && m.X >= r.X+pos.X && m.X < r.X+pos.X+pos.W {
			a.Dialog.Focus = i + 1
			a.key(Message{Key: "Enter"})
			return
		}
	}
}

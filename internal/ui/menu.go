package ui

import "strings"

type MenuState struct{ Column, Row, Top int }
type menuItem struct {
	Label, Shortcut string
	Run             func()
}
type menuSection struct {
	Name  string
	X     int
	Items []menuItem
}

func (a *App) menuSections() []menuSection {
	function := func(n int) func() { return func() { a.function(n) } }
	panel := func(index int) []menuItem {
		items := []menuItem{}
		for _, view := range []string{"Brief", "Medium", "Full", "Wide", "Detailed", "", "Info panel", "Tree panel", "Quick view"} {
			if view == "" {
				items = append(items, menuItem{})
				continue
			}
			view := view
			label := panelViewLabel(view)
			if a.Panels[index].View == view || (a.Panels[index].View == "" && view == "Full") {
				label = "✓ " + label
			}
			items = append(items, menuItem{label, "", func() { a.setView(index, view) }})
		}
		return append(items, []menuItem{
			{},
			{"Sort modes…", "⌃F12", func() { a.sortMenu(index) }},
			{"Panel on/off", []string{"⌘F1", "⌘F2"}[index], func() { a.togglePanel(index) }},
			{"Re-read", "⌘R", func() { a.rereadPanel(index) }},
			{"Change volume…", []string{"⌥F1", "⌥F2"}[index], func() { a.volumes(index) }},
		}...)
	}
	settings := func(kind string) func() { return func() { a.settingsMenu(kind) } }
	return []menuSection{
		{"Left", 2, panel(0)},
		{"Files", 11, []menuItem{
			{"Open with macOS", "⇧Enter", a.openSystemSelected},
			{"View / folder size", "F3", function(3)}, {"Calculate all folder sizes", "⌘⌥F3", func() { a.calculateFolderSizes(true) }}, {"Edit", "F4", function(4)},
			{"Alternate viewer", "⌥F3", func() { a.openConfigured(false, true, false) }}, {"Alternate editor", "⌥F4", func() { a.openConfigured(true, true, false) }},
			{"Internal viewer", "⌘⇧F3", func() { a.openConfigured(false, false, true) }}, {"Internal editor", "⌘⇧F4", func() { a.openConfigured(true, false, true) }},
			{"New file…", "⇧F4", a.newFile},
			{"Copy…", "F5", function(5)}, {"Rename or move…", "F6", function(6)}, {"Link…", "⌥F6", a.createLink},
			{"Make folder…", "F7", function(7)}, {"Move to Trash…", "F8", function(8)},
			{"Delete permanently…", "⇧F8", func() { a.deleteSelected(true) }},
			{"Create archive…", "⇧F1", a.archiveCreate}, {"Extract archive…", "⇧F2", a.archiveExtract}, {"Archive commands…", "⇧F3", a.archiveCommands},
			{"File attributes…", "", a.fileAttributes}, {"Apply command…", "", a.applyCommand}, {"Describe files…", "", a.describeFiles},
			{"Select group…", "Num +", func() { a.selectGroup("Select") }}, {"Deselect group…", "Num −", func() { a.selectGroup("Deselect") }},
			{"Invert selection", "Num *", func() { a.selectGroup("Invert") }}, {"Restore selection", "⌘M", func() { a.selectGroup("Restore") }},
		}},
		{"Commands", 21, []menuItem{
			{"Network connections…", "", a.networkMenu},
			{"Quick view on/off", "⌃Q", func() { a.toggleOtherView("Quick view") }},
			{"New window", "⌘N", func() { a.NewWindow = true }},
			{"Find file…", "⌥F7", func() { a.findFiles(false) }}, {"Command history", "⌥F8", func() { a.historyMenu("Commands") }},
			{"Find folder…", "", func() { a.findFiles(true) }}, {"Folder tree", "⌥F10", a.treeMenu},
			{"File view/edit history", "⌥F11", func() { a.historyMenu("Files") }}, {"Folder history", "⌥F12", func() { a.historyMenu("Folders") }},
			{"Same folder in other panel", "⌘=", a.matchOtherPanel},
			{"Swap panels", "⌘U", func() { a.Panels[0], a.Panels[1] = a.Panels[1], a.Panels[0]; a.previewKey = "" }},
			{"Panels on/off / terminal", "⌘O", a.showTerminal},
			{"Compare folders", "", a.compareFolders}, {"User menu", "F2", a.userMenu}, {"Edit user menu…", "", a.editUserMenu},
			{"File associations…", "", a.fileAssociations}, {"Folder shortcuts…", "⌃1…9", a.folderShortcuts}, {"Edit sort groups…", "", settings("Sort groups")},
			{"File panel filter…", "", func() { a.filterPanel(a.Active) }}, {"Screens list", "F12", a.screenMenu}, {"Last messages…", "", a.showMessages},
			{"Full screen", "⌃⌘F", func() { a.Fullscreen = true }}, {"Task list", "", a.taskList}, {"Mounted volumes", "", func() { a.volumes(a.Active) }},
			{"Insert filename", "⌘Enter", a.insertFilename}, {"Quit", "⌘Q", a.RequestQuit},
		}},
		{"Options", 34, []menuItem{
			{"General settings…", "", settings("General settings")},
			{"Panel settings…", "", settings("Panel settings")},
			{"Terminal settings…", "", settings("Terminal settings")},
			{},
			{"Viewer settings…", "", settings("Viewer settings")},
			{"Editor settings…", "", settings("Editor settings")},
			{"Advanced…", "", settings("Advanced settings")},
			{},
			{"Full Disk Access…", "", func() {
				a.DiskAccessSettings = true
				a.Status = "Enable this copy of FarshMac in Full Disk Access (+ or drag from Finder), then quit and reopen."
			}},
			{},
			{"Save setup", "⇧F9", func() { a.saveSetup() }}, {"Keyboard reference", "F1", function(1)},
		}},
		{"Right", 46, panel(1)},
	}
}

func (a *App) menuKey(m Message) {
	menu := a.Menu
	sections := a.menuSections()
	items := sections[menu.Column].Items
	switch m.Key {
	case "Escape", "F9":
		a.Menu = nil
	case "9":
		if m.Meta {
			a.Menu = nil
		}
	case "ArrowLeft":
		menu.Column = (menu.Column + len(sections) - 1) % len(sections)
		menu.Row = 0
	case "ArrowRight":
		menu.Column = (menu.Column + 1) % len(sections)
		menu.Row = 0
	case "Tab":
		switch menu.Column {
		case 0:
			menu.Column = 4
		case 4:
			menu.Column = 0
		default:
			menu.Column = 4
			if a.Active == 1 {
				menu.Column = 0
			}
		}
		menu.Row = 0
	case "ArrowDown":
		if a.Settings.MenuWrap {
			menu.Row = (menu.Row + 1) % len(items)
		} else {
			menu.Row = min(menu.Row+1, len(items)-1)
		}
	case "ArrowUp":
		if a.Settings.MenuWrap {
			menu.Row = (menu.Row + len(items) - 1) % len(items)
		} else {
			menu.Row = max(menu.Row-1, 0)
		}
	case "Home":
		menu.Row = 0
	case "End":
		menu.Row = len(items) - 1
	case "PageDown":
		menu.Row = min(len(items)-1, menu.Row+a.Rows-5)
	case "PageUp":
		menu.Row = max(0, menu.Row-a.Rows+5)
	case "Enter":
		a.lastMenu = *menu
		run := items[menu.Row].Run
		a.Menu = nil
		if run != nil {
			run()
		}
	}
	if a.Menu != nil {
		items = a.menuSections()[menu.Column].Items
		direction := 1
		if m.Key == "ArrowUp" || m.Key == "PageUp" || m.Key == "End" {
			direction = -1
		}
		for items[menu.Row].Run == nil {
			menu.Row = (menu.Row + direction + len(items)) % len(items)
		}
	}
}

func (a *App) menuBounds() (x, y, w, h int) {
	section := a.menuSections()[a.Menu.Column]
	w = min(44, a.Cols-2)
	h = min(len(section.Items)+2, a.Rows-3)
	a.Menu.Top = max(0, min(a.Menu.Top, len(section.Items)-(h-2)))
	if a.Menu.Row < a.Menu.Top {
		a.Menu.Top = a.Menu.Row
	}
	if a.Menu.Row >= a.Menu.Top+h-2 {
		a.Menu.Top = a.Menu.Row - h + 3
	}
	return min(section.X, a.Cols-w), 1, w, h
}
func (a *App) menuClick(m Message) {
	x, y, w, h := a.menuBounds()
	if m.X > x && m.X < x+w-1 && m.Y > y && m.Y < y+h-1 {
		items := a.menuSections()[a.Menu.Column].Items
		index := a.Menu.Top + m.Y - y - 1
		if index < 0 || index >= len(items) {
			return
		}
		item := items[index]
		if item.Run == nil {
			return
		}
		a.Menu = nil
		item.Run()
		return
	}
	a.Menu = nil
}
func (a *App) drawMenu(g *Grid) {
	sections := a.menuSections()
	section := sections[a.Menu.Column]
	g.Text(section.X, 0, section.Name, White, Black, len(section.Name))
	x, y, w, h := a.menuBounds()
	g.Fill(x+1, y+1, w, h, Black, Black)
	g.Box(x, y, w, h, White, Cyan)
	for row := 0; row < h-2; row++ {
		i := a.Menu.Top + row
		if i >= len(section.Items) {
			break
		}
		item := section.Items[i]
		if item.Run == nil {
			g.Text(x+1, y+1+row, strings.Repeat("─", w-2), White, Cyan, w-2)
			g.Set(x, y+1+row, "╟", White, Cyan)
			g.Set(x+w-1, y+1+row, "╢", White, Cyan)
			continue
		}
		fg, bg := uint32(White), uint32(Cyan)
		if a.menuItemDisabled(section, item) {
			fg = Gray
		}
		if i == a.Menu.Row {
			bg = Black
		}
		g.Fill(x+1, y+1+row, w-2, 1, fg, bg)
		label := strings.TrimPrefix(item.Label, "✓ ")
		if label != item.Label {
			g.Set(x+1, y+1+row, "✓", fg, bg)
		}
		g.Text(x+2, y+1+row, label, fg, bg, w-4-len([]rune(item.Shortcut)))
		g.Text(x+w-11, y+1+row, item.Shortcut, Yellow, bg, 9)
	}
}

// menuItemDisabled marks commands that the active (or, for Left/Right, the
// selected) panel cannot run because it is remote or an archive. They stay
// selectable and still explain why when run (P5-14).
func (a *App) menuItemDisabled(section menuSection, item menuItem) bool {
	label := strings.TrimPrefix(item.Label, "✓ ")
	switch section.Name {
	case "Left", "Right":
		p := a.Panels[0]
		if section.Name == "Right" {
			p = a.Panels[1]
		}
		switch label {
		case "Info panel", "Quick view":
			return p.Remote != nil
		case "Tree panel":
			return p.Remote != nil || p.Archive != nil
		}
		return false
	}
	p := a.Panels[a.Active]
	other := a.Panels[1-a.Active]
	remote, archive := p.Remote != nil, p.Archive != nil
	switch label {
	case "Open with macOS", "Archive commands…", "Find file…", "Find folder…", "Folder tree", "User menu":
		return remote
	case "Edit", "Rename or move…", "Make folder…", "Move to Trash…", "Delete permanently…":
		return archive
	case "New file…", "Link…", "File attributes…", "Apply command…", "Describe files…":
		return remote || archive
	case "Create archive…":
		return remote || archive || other.Remote != nil
	case "Extract archive…":
		return remote || other.Remote != nil
	}
	return false
}

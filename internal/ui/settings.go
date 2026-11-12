package ui

import (
	"farshmac/internal/remotefs"
	"farshmac/internal/terminal"
	"farshmac/internal/textfile"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Settings struct {
	InfoMemory, InfoPower, InfoDescription                                                           bool
	ShellCustomPrompt                                                                                bool
	SelectFolders                                                                                    bool
	CopyWorkers                                                                                      int
	RemoteConnections                                                                                []remotefs.Config
	CopyPermissions, CopyAttributes                                                                  bool
	CopySymlinks                                                                                     string // fsops.SymlinkMode name; empty is Smart
	Shell                                                                                            string
	HumanReadableSizes                                                                               bool
	ExternalViewer, ExternalEditor                                                                   string
	ExternalViewerDefault, ExternalEditorDefault                                                     bool
	CompleteFiles, CompleteIgnoreCase, SelectDialogInput, RememberDialogValues, InfoHost, InfoVolume bool
	FolderDescriptions                                                                               string
	AutoSave, MenuWrap, ShowKeys                                                                     bool
	FontSize                                                                                         int
	ColorTheme                                                                                       string
	ConfirmDelete                                                                                    bool
	ConfirmOverwrite                                                                                 bool
	ShowHidden                                                                                       bool
	IgnoreDSStore                                                                                    bool
	Highlight                                                                                        bool
	ViewerWrap                                                                                       bool
	EditorAutoIndent                                                                                 bool
	EditorSyntax                                                                                     bool
	EditorExpandTabs                                                                                 bool
	EditorTabSize                                                                                    int
	DefaultCodepage                                                                                  string
	HistoryLimit                                                                                     int
	TreeDepth                                                                                        int
	UserMenu                                                                                         []UserCommand
	Associations                                                                                     []Association
	Shortcuts                                                                                        [10]string
	ShortcutRemotes                                                                                  [10]*remotefs.Config
	ShortcutArchives                                                                                 [10]string
	SortGroups                                                                                       string
	MaskGroups                                                                                       map[string]string
	DescriptionFile                                                                                  string
}
type UserCommand struct{ Name, Command string }
type Association struct{ Mask, Command, Viewer, Editor string }
type savedSetup struct {
	Settings          Settings
	Panels            [2]panelOptions
	Paths             [2]string
	Commands, Folders []string
	Viewed            []viewRecord
}
type viewRecord struct {
	Path string
	Edit bool
}

func defaultSettings() Settings {
	return Settings{InfoMemory: true, InfoPower: true, InfoDescription: true, CopyWorkers: 4, CopyPermissions: true, CopyAttributes: true, Shell: "bash", CompleteFiles: true, SelectDialogInput: true, InfoHost: true, InfoVolume: true, FolderDescriptions: "file_id.diz,README.md,README,readme.txt", MenuWrap: true, ShowKeys: true, FontSize: 14, ConfirmDelete: true, ConfirmOverwrite: true, ShowHidden: true, Highlight: true, EditorAutoIndent: true, EditorSyntax: true, EditorTabSize: 4, DefaultCodepage: "UTF-8", HistoryLimit: 200, TreeDepth: 4, DescriptionFile: "descript.ion", MaskGroups: map[string]string{"arc": "*.zip,*.tar,*.gz,*.bz2,*.xz,*.tgz,*.tbz,*.tbz2,*.txz,*.7z,*.rar,*.jar,*.war,*.cpio", "source": "*.go,*.c,*.h,*.js,*.ts,*.py", "exec": "*.sh,*.command"}}
}
func setupPath() string {
	root, _ := os.UserConfigDir()
	return filepath.Join(root, "FarshMac", "setup.json")
}
func (a *App) LoadSetup() {
	defer a.loadConnectionProfiles()
	s, ok := a.readSetupFile()
	if !ok {
		return
	}
	a.Settings = s.Settings
	a.normalizeSettings()
	for i := range a.Panels {
		a.Panels[i].panelOptions = s.Panels[i]
		a.configurePanel(i)
		path := a.Panels[i].Path
		// Folder restoration is automatic through session.json, independently of Save setup.
		if a.Panels[i].Load(path) != nil {
			_ = a.Panels[i].Load(a.Panels[i].Path)
		}
	}
	a.history = s.Commands
	a.historyAt = len(a.history)
	a.folderHistory = s.Folders
	a.viewHistory = s.Viewed
}
func (a *App) normalizeSettings() {
	a.Settings.CopyWorkers = max(1, min(8, a.Settings.CopyWorkers))
	if !terminal.ValidShell(a.Settings.Shell) {
		a.Settings.Shell = "bash"
	}
	if a.Settings.EditorTabSize < 1 || a.Settings.EditorTabSize > 16 {
		a.Settings.EditorTabSize = 4
	}
	a.Settings.FontSize = max(10, min(24, a.Settings.FontSize))
	a.Settings.HistoryLimit = max(1, min(10000, a.Settings.HistoryLimit))
	a.Settings.TreeDepth = max(1, min(12, a.Settings.TreeDepth))
	if a.Settings.MaskGroups == nil {
		a.Settings.MaskGroups = map[string]string{}
	}
	if a.Settings.DescriptionFile == "" || filepath.Base(a.Settings.DescriptionFile) != a.Settings.DescriptionFile {
		a.Settings.DescriptionFile = "descript.ion"
	}
}
func (a *App) configurePanel(i int) {
	p := &a.Panels[i]
	switch p.View {
	case "Descriptions", "Long descriptions", "File owners", "File links", "Alternative full":
		p.View = "Full"
	}
	p.HumanReadableSizes = a.Settings.HumanReadableSizes
	p.InfoHost = a.Settings.InfoHost
	p.InfoVolume = a.Settings.InfoVolume
	p.FolderDescriptionFiles = a.Settings.FolderDescriptions
	p.TreeDepth = a.Settings.TreeDepth
	p.HideHidden = !a.Settings.ShowHidden
	p.IgnoreDSStore = a.Settings.IgnoreDSStore
	p.SortGroups = a.Settings.SortGroups
	p.MaskGroups = a.Settings.MaskGroups
	p.DescriptionFile = a.Settings.DescriptionFile
	a.warnInvalidFilter(i)
}

// setupSnapshot collects what Save setup writes from this window: shared
// settings plus this window's panel options and histories.
func (a *App) setupSnapshot() savedSetup {
	a.normalizeSettings()
	for _, p := range a.Panels {
		if p.Remote != nil {
			continue
		}
		// History entries carry their source (see historyEntry); remote
		// entries hold only the password-free connection label.
		for _, path := range p.History {
			a.folderHistory = appendUnique(a.folderHistory, path, a.Settings.HistoryLimit)
		}
	}
	s := savedSetup{Settings: a.Settings, Commands: a.history, Folders: a.folderHistory, Viewed: a.viewHistory}
	for i := range a.Panels {
		s.Panels[i] = a.Panels[i].panelOptions
		s.Paths[i] = a.Panels[i].filesystemPath()
	}
	return s
}
func (a *App) settingsMenu(kind string) {
	toggle := func(label string, b bool) Field { return Field{Label: label, Toggle: true, Checked: b} }
	switch kind {
	case "General settings":
		a.form(kind, []Field{{Label: "Font size (10–24)", Value: strconv.Itoa(a.Settings.FontSize)}, toggle("Key bar", a.Settings.ShowKeys), toggle("Wrap menus", a.Settings.MenuWrap), toggle("Select initial dialog text", a.Settings.SelectDialogInput), toggle("Remember dialog values", a.Settings.RememberDialogValues), toggle("Confirm Move to Trash (permanent deletion always asks)", a.Settings.ConfirmDelete), toggle("Confirm overwrite", a.Settings.ConfirmOverwrite), toggle("Save setup on exit", a.Settings.AutoSave)}, func(f []Field) {
			n, err := strconv.Atoi(f[0].Value)
			if err != nil || n < 10 || n > 24 {
				a.reopenForm(f, 0, "Font size must be 10–24")
				return
			}
			a.Settings.FontSize = n
			a.Settings.ShowKeys = f[1].Checked
			a.Settings.MenuWrap = f[2].Checked
			a.Settings.SelectDialogInput = f[3].Checked
			a.Settings.RememberDialogValues = f[4].Checked
			a.Settings.ConfirmDelete = f[5].Checked
			a.Settings.ConfirmOverwrite = f[6].Checked
			a.Settings.AutoSave = f[7].Checked
		})
	case "Terminal settings":
		a.form(kind, []Field{{Label: "Shell: bash / sh / zsh (new windows)", Value: a.Settings.Shell}, {Label: "Command history entries (1–10000)", Value: strconv.Itoa(a.Settings.HistoryLimit)}, toggle("Complete filenames", a.Settings.CompleteFiles), toggle("Completion ignores case", a.Settings.CompleteIgnoreCase), toggle("Use custom shell prompt / hooks (new windows, may be slow)", a.Settings.ShellCustomPrompt)}, func(f []Field) {
			shell := strings.TrimSpace(f[0].Value)
			if !terminal.ValidShell(shell) {
				a.reopenForm(f, 0, "Choose bash, sh or zsh")
				return
			}
			n, err := strconv.Atoi(f[1].Value)
			if err != nil || n < 1 || n > 10000 {
				a.reopenForm(f, 1, "History entries must be 1–10000")
				return
			}
			if shell != a.Settings.Shell || f[4].Checked != a.Settings.ShellCustomPrompt {
				a.shellFailures = 0 // New configuration: restart backoff starts over.
			}
			a.Settings.Shell = shell
			a.Settings.HistoryLimit = n
			a.Settings.CompleteFiles = f[2].Checked
			a.Settings.CompleteIgnoreCase = f[3].Checked
			a.Settings.ShellCustomPrompt = f[4].Checked
			a.Status = "Shell applies to new windows; Save setup to keep settings after restart"
		})
	case "Advanced settings":
		items := []string{"Colors", "Info panel settings", "Tree settings", "Folder description files", "File descriptions", "Groups of file masks", "Sort groups", "Code pages"}
		a.choose(kind, items, func(i int) { a.settingsMenu(items[i]) })
	case "Info panel settings":
		a.form(kind, []Field{toggle("Computer and user", a.Settings.InfoHost), toggle("Drive information", a.Settings.InfoVolume), toggle("Memory information", a.Settings.InfoMemory), toggle("Folder description", a.Settings.InfoDescription), toggle("Power status", a.Settings.InfoPower)}, func(f []Field) {
			a.Settings.InfoHost = f[0].Checked
			a.Settings.InfoVolume = f[1].Checked
			a.Settings.InfoMemory = f[2].Checked
			a.Settings.InfoDescription = f[3].Checked
			a.Settings.InfoPower = f[4].Checked
			a.infoKeys = [2]string{}
			a.dirty = true
		})
	case "Folder description files":
		a.prompt(kind, "Names separated by commas", a.Settings.FolderDescriptions, func(s string) {
			var names []string
			for _, name := range strings.Split(s, ",") {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				if !plainFileName(name) {
					a.rejectPrompt(s, "Enter a file name without a path")
					return
				}
				names = append(names, name)
			}
			s = strings.Join(names, ",")
			a.Settings.FolderDescriptions = s
			a.refresh()
		})

	case "Colors":
		names := []string{"Classic Far", "Dark", "Light"}
		a.choose(kind, names, func(i int) { a.Settings.ColorTheme = names[i] })

	case "Panel settings":
		a.form(kind, []Field{toggle("Show hidden files (Cmd+Shift+.)", a.Settings.ShowHidden), toggle("Highlight files", a.Settings.Highlight), toggle("Human-readable sizes (off = bytes)", a.Settings.HumanReadableSizes), toggle("Hide and ignore .DS_Store", a.Settings.IgnoreDSStore), toggle("Select folders with file masks", a.Settings.SelectFolders)}, func(f []Field) {
			a.Settings.ShowHidden = f[0].Checked
			a.Settings.Highlight = f[1].Checked
			a.Settings.HumanReadableSizes = f[2].Checked
			a.Settings.IgnoreDSStore = f[3].Checked
			a.Settings.SelectFolders = f[4].Checked
			for i := range a.Panels {
				a.Panels[i].ForcedVisiblePath = ""
				a.configurePanel(i)
			}
			a.refresh()
		})
	case "Viewer settings":
		a.form(kind, []Field{toggle("Wrap lines", a.Settings.ViewerWrap), toggle("External by default", a.Settings.ExternalViewerDefault), {Label: "Command (blank=system)", Value: a.Settings.ExternalViewer}}, func(f []Field) {
			a.Settings.ViewerWrap = f[0].Checked
			a.Settings.ExternalViewerDefault = f[1].Checked
			a.Settings.ExternalViewer = f[2].Value
		})
	case "Editor settings":
		a.form(kind, []Field{toggle("Auto indent", a.Settings.EditorAutoIndent), toggle("Syntax highlighting (Chroma)", a.Settings.EditorSyntax), {Label: "Tab width (1–16)", Value: strconv.Itoa(a.Settings.EditorTabSize)}, toggle("Expand tabs to spaces", a.Settings.EditorExpandTabs), toggle("External by default", a.Settings.ExternalEditorDefault), {Label: "Command (blank=system)", Value: a.Settings.ExternalEditor}}, func(f []Field) {
			n, err := strconv.Atoi(f[2].Value)
			if err != nil || n < 1 || n > 16 {
				a.reopenForm(f, 2, "Tab width must be 1–16")
				return
			}
			a.Settings.EditorAutoIndent = f[0].Checked
			a.Settings.EditorSyntax = f[1].Checked
			a.Settings.EditorTabSize = n
			a.Settings.EditorExpandTabs = f[3].Checked
			a.Settings.ExternalEditorDefault = f[4].Checked
			a.Settings.ExternalEditor = f[5].Value
		})
	case "Code pages":
		a.choose("Default encoding for new files", textfile.Codepages, func(i int) { a.Settings.DefaultCodepage = textfile.Codepages[i] })
	case "Tree settings":
		a.prompt(kind, "Maximum levels for folder tree (1–12)", strconv.Itoa(a.Settings.TreeDepth), func(s string) {
			n, e := strconv.Atoi(s)
			if e != nil || n < 1 || n > 12 {
				a.rejectPrompt(s, "Enter a number from 1 to 12")
				return
			}
			a.Settings.TreeDepth = n
			for i := range a.Panels {
				a.configurePanel(i)
			}
			a.refresh()
		})
	case "File descriptions":
		a.prompt(kind, "Description file name", a.Settings.DescriptionFile, func(s string) {
			if name := strings.TrimSpace(s); !plainFileName(name) {
				a.rejectPrompt(s, "Enter a file name without a path")
				return
			} else {
				s = name
			}
			a.Settings.DescriptionFile = s
			for i := range a.Panels {
				a.configurePanel(i)
			}
			a.refresh()
		})
	case "Sort groups":
		a.prompt(kind, "Masks in priority order, separated by semicolons", a.Settings.SortGroups, func(s string) {
			if err := validateMasks(strings.ReplaceAll(s, ";", ","), a.Settings.MaskGroups); err != nil {
				a.rejectPrompt(s, err.Error())
				return
			}
			a.Settings.SortGroups = s
			for i := range a.Panels {
				a.configurePanel(i)
			}
			a.refresh()
		})
	case "Groups of file masks":
		keys := sortedKeys(a.Settings.MaskGroups)
		names := []string{"Add group…"}
		for _, k := range keys {
			names = append(names, k+" = "+a.Settings.MaskGroups[k])
		}
		a.choose(kind, names, func(i int) {
			name, value := "", ""
			if i > 0 {
				// Use the key itself: a label cannot be split back reliably.
				name = keys[i-1]
				value = a.Settings.MaskGroups[name]
			}
			old := name
			a.form("Mask group", []Field{{Label: "Name", Value: name}, {Label: "Masks (empty deletes)", Value: value}}, func(f []Field) {
				k, v := strings.TrimSpace(f[0].Value), f[1].Value
				// Existing names with "=" (allowed before) stay editable.
				if k == "" || strings.ContainsAny(k, "<>,;|") || k != old && strings.Contains(k, "=") {
					a.reopenForm(f, 0, "Invalid group name")
					return
				}
				if strings.ContainsAny(v, "<>") {
					a.reopenForm(f, 1, "Use literal masks in groups")
					return
				}
				if err := validateMasks(v, nil); err != nil {
					a.reopenForm(f, 1, err.Error())
					return
				}
				apply := func() {
					delete(a.Settings.MaskGroups, old)
					if v != "" {
						a.Settings.MaskGroups[k] = v
					}
					for i := range a.Panels {
						a.configurePanel(i)
					}
					a.refresh()
				}
				// Deleting or renaming a referenced group would silently
				// invalidate filters, sort groups or associations.
				if old != "" && (v == "" || k != old) {
					if use := a.maskGroupUse(old); use != "" {
						a.confirm("Mask group", "Group "+old+" is used by "+use+". Delete anyway?", []string{"Delete", "Cancel"}, func(i int) {
							if i == 0 {
								apply()
							}
						})
						return
					}
				}
				apply()
			})
		})
	default:
		a.Status = fmt.Sprintf("Settings: %s", kind)
	}
}

// fontSizeKey handles Cmd+Plus / Cmd+Minus / Cmd+0. Cmd+= also zooms in the
// terminal; in panels it keeps "Same folder in other panel".
func (a *App) fontSizeKey(m Message) bool {
	if !m.Meta || m.Ctrl || m.Alt || strings.HasPrefix(m.Code, "Numpad") {
		return false
	}
	size := a.Settings.FontSize
	switch {
	case m.Key == "+" || m.Key == "=" && a.TermVisible:
		size++
	case m.Key == "-" && !m.Shift:
		size--
	case m.Key == "0" && !m.Shift:
		size = defaultSettings().FontSize
	default:
		return false
	}
	a.Settings.FontSize = max(10, min(24, size))
	a.Status = fmt.Sprintf("Font size %d", a.Settings.FontSize)
	a.dirty = true
	return true
}

// plainFileName accepts a single file name: no path, not "." or "..".
func plainFileName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\x00") && filepath.Base(name) == name
}

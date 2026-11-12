package ui

import (
	"encoding/json"
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
	Shell                                                                                            string
	HumanReadableSizes                                                                               bool
	ExternalViewer, ExternalEditor                                                                   string
	ExternalViewerDefault, ExternalEditorDefault                                                     bool
	CompleteFiles, CompleteIgnoreCase, SelectDialogInput, RememberDialogValues, InfoHost, InfoVolume bool
	FolderDescriptions                                                                               string
	AutoSave, RestoreFolders, MenuWrap, ShowKeys                                                     bool
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
	b, err := readConfigFile(setupPath())
	if err != nil {
		return
	}
	s := savedSetup{Settings: defaultSettings()}
	if json.Unmarshal(b, &s) != nil {
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
}
func (a *App) saveSetup() {
	a.normalizeSettings()
	for _, p := range a.Panels {
		if p.Remote != nil {
			for i := range a.Settings.RemoteConnections {
				if a.Settings.RemoteConnections[i].Label() == p.Remote.Label() {
					a.Settings.RemoteConnections[i].Directory = p.Path
				}
			}
			continue
		}
		for _, path := range p.History {
			a.folderHistory = appendUnique(a.folderHistory, path, a.Settings.HistoryLimit)
		}
	}
	s := savedSetup{Settings: a.Settings, Commands: a.history, Folders: a.folderHistory, Viewed: a.viewHistory}
	for i := range a.Panels {
		s.Panels[i] = a.Panels[i].panelOptions
		s.Paths[i] = a.Panels[i].filesystemPath()
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err == nil {
		err = os.MkdirAll(filepath.Dir(setupPath()), 0700)
	}
	if err == nil {
		var f *os.File
		f, err = os.CreateTemp(filepath.Dir(setupPath()), ".setup-*")
		if err == nil {
			defer os.Remove(f.Name())
			_, err = f.Write(b)
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
			if err == nil {
				err = os.Rename(f.Name(), setupPath())
			}
		}
	}
	a.Status = "Setup saved"
	a.error(err)
}
func (a *App) settingsMenu(kind string) {
	toggle := func(label string, b bool) Field { return Field{Label: label, Toggle: true, Checked: b} }
	switch kind {
	case "General settings":
		a.form(kind, []Field{{Label: "Font size (10–24)", Value: strconv.Itoa(a.Settings.FontSize)}, toggle("Key bar", a.Settings.ShowKeys), toggle("Wrap menus", a.Settings.MenuWrap), toggle("Select initial dialog text", a.Settings.SelectDialogInput), toggle("Remember dialog values", a.Settings.RememberDialogValues), toggle("Confirm deletion", a.Settings.ConfirmDelete), toggle("Confirm overwrite", a.Settings.ConfirmOverwrite), toggle("Save setup on exit", a.Settings.AutoSave)}, func(f []Field) {
			n, err := strconv.Atoi(f[0].Value)
			if err != nil || n < 10 || n > 24 {
				a.Status = "Font size must be 10–24"
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
				a.Status = "Choose bash, sh or zsh"
				return
			}
			n, err := strconv.Atoi(f[1].Value)
			if err != nil || n < 1 || n > 10000 {
				a.Status = "History entries must be 1–10000"
				return
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
	case "Dialog settings":
		a.form(kind, []Field{toggle("Select initial input", a.Settings.SelectDialogInput), toggle("Remember values", a.Settings.RememberDialogValues)}, func(f []Field) {
			a.Settings.SelectDialogInput = f[0].Checked
			a.Settings.RememberDialogValues = f[1].Checked
		})
	case "AutoComplete settings":
		a.form(kind, []Field{toggle("Files (⌘Space)", a.Settings.CompleteFiles), toggle("Ignore case", a.Settings.CompleteIgnoreCase)}, func(f []Field) { a.Settings.CompleteFiles = f[0].Checked; a.Settings.CompleteIgnoreCase = f[1].Checked })
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
			for _, name := range strings.Split(s, ",") {
				name = strings.TrimSpace(name)
				if name != "" && filepath.Base(name) != name {
					a.Status = "Use filenames without paths"
					return
				}
			}
			a.Settings.FolderDescriptions = s
			a.refresh()
		})

	case "System settings":
		a.form(kind, []Field{toggle("Save setup on exit", a.Settings.AutoSave)}, func(f []Field) { a.Settings.AutoSave = f[0].Checked })
	case "Interface settings":
		a.form(kind, []Field{{Label: "Font size (10–24)", Value: strconv.Itoa(a.Settings.FontSize)}, toggle("Key bar", a.Settings.ShowKeys)}, func(f []Field) {
			n, e := strconv.Atoi(f[0].Value)
			if e != nil || n < 10 || n > 24 {
				a.Status = "Font size must be 10–24"
				return
			}
			a.Settings.FontSize = n
			a.Settings.ShowKeys = f[1].Checked
		})
	case "Menu settings":
		a.form(kind, []Field{toggle("Wrap at list ends", a.Settings.MenuWrap)}, func(f []Field) { a.Settings.MenuWrap = f[0].Checked })
	case "Colors":
		names := []string{"Classic Far", "Dark", "Light"}
		a.choose(kind, names, func(i int) { a.Settings.ColorTheme = names[i] })

	case "Confirmations":
		a.form(kind, []Field{toggle("Move to Trash (permanent deletion always asks)", a.Settings.ConfirmDelete), toggle("Overwrite files", a.Settings.ConfirmOverwrite)}, func(f []Field) { a.Settings.ConfirmDelete = f[0].Checked; a.Settings.ConfirmOverwrite = f[1].Checked })
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
				a.Status = "Tab width must be 1–16"
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
				a.Status = "Enter a number from 1 to 12"
				return
			}
			a.Settings.TreeDepth = n
			for i := range a.Panels {
				a.configurePanel(i)
			}
			a.refresh()
		})
	case "Command line settings":
		a.prompt(kind, "Maximum history entries (1–10000)", strconv.Itoa(a.Settings.HistoryLimit), func(s string) {
			n, e := strconv.Atoi(s)
			if e != nil || n < 1 || n > 10000 {
				a.Status = "Enter a number from 1 to 10000"
				return
			}
			a.Settings.HistoryLimit = n
		})
	case "File descriptions":
		a.prompt(kind, "Description file name", a.Settings.DescriptionFile, func(s string) {
			if s == "" || filepath.Base(s) != s {
				a.Status = "Enter a file name without a path"
				return
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
				a.error(err)
				return
			}
			a.Settings.SortGroups = s
			for i := range a.Panels {
				a.configurePanel(i)
			}
			a.refresh()
		})
	case "Groups of file masks":
		names := []string{"Add group…"}
		for _, k := range sortedKeys(a.Settings.MaskGroups) {
			names = append(names, k+" = "+a.Settings.MaskGroups[k])
		}
		a.choose(kind, names, func(i int) {
			name, value := "", ""
			if i > 0 {
				parts := strings.SplitN(names[i], " = ", 2)
				name, value = parts[0], parts[1]
			}
			old := name
			a.form("Mask group", []Field{{Label: "Name", Value: name}, {Label: "Masks (empty deletes)", Value: value}}, func(f []Field) {
				k, v := strings.TrimSpace(f[0].Value), f[1].Value
				if k == "" || strings.ContainsAny(k, "<>,;|") {
					a.Status = "Invalid group name"
					return
				}
				if strings.ContainsAny(v, "<>") {
					a.Status = "Use literal masks in groups"
					return
				}
				if err := validateMasks(v, nil); err != nil {
					a.error(err)
					return
				}
				delete(a.Settings.MaskGroups, old)
				if v != "" {
					a.Settings.MaskGroups[k] = v
				}
				for i := range a.Panels {
					a.configurePanel(i)
				}
				a.refresh()
			})
		})
	default:
		a.Status = fmt.Sprintf("Settings: %s", kind)
	}
}

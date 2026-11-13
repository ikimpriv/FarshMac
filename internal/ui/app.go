package ui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"farshmac/internal/highlight"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
	"farshmac/internal/terminal"
)

type Entry struct {
	Name string
	Info os.FileInfo
	Dir  bool
}
type Panel struct {
	sizeArchive            *fsops.ArchiveIndex
	sizeArchiveGeneration  uint64
	folderSizes            map[string]folderSizeResult
	folderSizeKey          folderSizeKey
	folderSizeHistory      []folderSizeSnapshot
	infoRows               []infoRow
	InfoOffset             int
	viewReturns            map[string]panelViewReturn
	Remote                 *remotefs.Config
	RemoteLocalPath        string // Local folder behind a remote or Kubernetes panel.
	Archive                *fsops.ArchiveIndex
	Kube                   *kubePanel
	Async                  bool
	load                   *panelLoad
	rollback               *panelRollback
	loadStatus             string
	readOperation          *fsops.Operation
	tree                   *treeLoad
	treeLabels             map[string]string
	ForcedVisiblePath      string
	InfoHost, InfoVolume   bool
	HumanReadableSizes     bool
	FolderDescriptionFiles string
	TotalBytes             int64
	ItemCount, TreeDepth   int
	InfoText               string
	panelOptions
	HideHidden                    bool
	IgnoreDSStore                 bool
	SortGroups, DescriptionFile   string
	MaskGroups, Descriptions      map[string]string
	PreviousMarked, RestoreMarked map[string]bool
	History                       []string
	Path                          string
	Entries                       []Entry
	Cursor, Offset                int
	Marked                        map[string]bool
	Err                           string
	states                        map[string]panelState
}

type panelState struct {
	CursorName, TopName string
	Cursor, Offset      int
	Marked              map[string]bool
}

func (p *Panel) snapshot() panelState {
	if p.load != nil {
		return p.load.state
	}
	if p.tree != nil {
		return p.tree.state
	}
	s := panelState{Cursor: p.Cursor, Offset: p.Offset, Marked: map[string]bool{}}
	if p.Cursor >= 0 && p.Cursor < len(p.Entries) {
		s.CursorName = p.Entries[p.Cursor].Name
	}
	if p.Offset >= 0 && p.Offset < len(p.Entries) {
		s.TopName = p.Entries[p.Offset].Name
	}
	for name, marked := range p.Marked {
		if marked {
			s.Marked[name] = true
		}
	}
	return s
}

func (p *Panel) Load(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if p.Kube != nil {
		if p.Async && !p.Kube.cachedFor(abs) {
			return p.loadPanel(abs)
		}
		return p.loadKube(abs)
	}
	if p.Remote != nil {
		if p.Async {
			return p.loadPanel(abs)
		}
		return p.loadRemote(abs)
	}
	if p.Archive != nil {
		if _, inside := p.Archive.Relative(abs); inside {
			if p.Async {
				return p.loadPanel(abs)
			}
			return p.loadArchiveDirectory(abs)
		}
	}
	if p.View == "Tree panel" {
		return p.loadTree(abs)
	}
	if p.Async {
		return p.loadPanel(abs)
	}
	var files []Entry
	err = fsops.ReadDirectory(abs, p.readOperation, func(e os.DirEntry) error {
		if p.IgnoreDSStore && e.Name() == ".DS_Store" {
			return nil
		}
		if p.HideHidden && strings.HasPrefix(e.Name(), ".") && filepath.Join(abs, e.Name()) != p.ForcedVisiblePath {
			return nil
		}
		info, err := e.Info()
		if err != nil {
			return nil
		}
		if p.HideHidden && hiddenEntry(e.Name(), info) && filepath.Join(abs, e.Name()) != p.ForcedVisiblePath {
			return nil
		}
		isDir := info.IsDir()
		if info.Mode()&os.ModeSymlink != 0 {
			if target, err := os.Stat(filepath.Join(abs, e.Name())); err == nil {
				isDir = target.IsDir()
			}
		}
		if !isDir && !matchesMask(e.Name(), p.activeFilter(), p.MaskGroups) {
			return nil
		}
		files = append(files, Entry{e.Name(), info, isDir})
		return nil
	})
	if err != nil {
		return err
	}
	p.Archive = nil
	state := p.beginLoad(abs)
	p.cancelTree()
	p.cancelPanelLoad()
	p.Descriptions = readDescriptions(filepath.Join(abs, p.DescriptionFile), p.readOperation)
	p.sortEntries(files)
	p.installEntries(files, state)
	p.loadInfo()
	return p.readOperation.Check()
}

func (p *Panel) beginLoad(abs string) panelState {
	previous := p.Path
	p.loadFolderSizes(abs)
	state := p.snapshot()
	changed := p.Path != abs
	if p.states == nil {
		p.states = map[string]panelState{}
	}
	if changed {
		if previous != "" {
			p.states[previous] = state
		}
		state = p.states[abs]
		// Going up always focuses the directory just left, even when the
		// parent has an older saved cursor (for example after a bookmark jump).
		if previous != "" && filepath.Dir(previous) == abs {
			state.CursorName = filepath.Base(previous)
		}
	}
	if changed {
		p.History = appendUnique(p.History, p.historyEntry(abs), 200)
	}
	p.Path = abs
	p.Entries = nil
	p.Err = ""
	if filepath.Dir(abs) != abs {
		p.Entries = append(p.Entries, Entry{Name: "..", Dir: true})
	}
	p.Cursor, p.Offset = 0, 0
	p.Marked = cloneMarks(state.Marked)
	p.treeLabels = nil
	return state
}

func (p *Panel) installEntries(files []Entry, state panelState) {
	p.Entries = append(p.Entries, files...)
	p.pruneFolderSizes()
	p.Cursor, p.Offset = state.Cursor, state.Offset
	p.Marked = map[string]bool{}
	for i, e := range p.Entries {
		if e.Name == state.CursorName {
			p.Cursor = i
		}
		if e.Name == state.TopName {
			p.Offset = i
		}
		if e.Name != ".." && state.Marked[e.Name] {
			p.Marked[e.Name] = true
		}
	}
	p.Cursor = max(0, min(p.Cursor, len(p.Entries)-1))
	p.Offset = max(0, min(p.Offset, p.Cursor))
}
func (p *Panel) Current() string {
	if len(p.Entries) == 0 {
		return ""
	}
	return filepath.Join(p.Path, p.Entries[p.Cursor].Name)
}
func (p *Panel) Selected() []string {
	var paths []string
	for _, e := range p.Entries {
		if e.Name != ".." && p.Marked[e.Name] {
			paths = append(paths, filepath.Join(p.Path, e.Name))
		}
	}
	if len(paths) == 0 && len(p.Entries) > 0 && p.Entries[p.Cursor].Name != ".." {
		paths = append(paths, p.Current())
	}
	return paths
}

type Dialog struct {
	Focus                 int
	Selected              bool
	Title, Message, Input string
	Confirm               func(string)
	Error                 string
	Back                  int // caret position counted from the end of Input
}
type Message struct {
	History                  []json.RawMessage
	Start, Request           int64
	Count, Delta             int
	Button, Buttons          int
	Type, Key, Text          string
	Code                     string
	Ctrl, Alt, Meta, Shift   bool
	X, Y, Clicks, Cols, Rows int
}
type Update struct {
	ShellReady         bool                   `json:"shellReady"`
	Commands           *[]string              `json:"commands,omitempty"`
	CommandsError      string                 `json:"commandsError,omitempty"`
	Zoom               bool                   `json:"zoom,omitempty"`
	Preview            *ImagePreview          `json:"preview,omitempty"`
	History            terminal.HistoryStatus `json:"history"`
	HistoryPage        *terminal.HistoryPage  `json:"historyPage,omitempty"`
	Title              string                 `json:"title"`
	QuitApplication    bool                   `json:"quitApplication,omitempty"`
	AttentionWindow    int                    `json:"attentionWindow,omitempty"`
	NewWindow          bool                   `json:"newWindow,omitempty"`
	PanelKeys          bool                   `json:"panelKeys"`
	FontSize           int                    `json:"fontSize"`
	Fullscreen         bool                   `json:"fullscreen,omitempty"`
	DiskAccessSettings bool                   `json:"diskAccessSettings,omitempty"`
	Cols               int                    `json:"cols"`
	Rows               int                    `json:"rows"`
	Grid               string                 `json:"grid,omitempty"`
	Terminal           string                 `json:"terminal,omitempty"`
	Mode               string                 `json:"mode"`
	Footer             string                 `json:"footer"`
	Document           bool                   `json:"document"`
	Clipboard          *string                `json:"clipboard,omitempty"`
	Quit               bool                   `json:"quit,omitempty"`
}
type App struct {
	shellState
	shellCommands         *[]string
	infoJobs              [2]*infoJob
	infoKeys              [2]string
	infoNext              [2]time.Time
	infoDescriptionPaths  [2]string
	Zoom                  bool
	panelSplit            int
	syntax                syntaxState
	syntaxRun             func(context.Context, string, string) (highlight.Result, error)
	runExternal           func(context.Context, []string) ([]byte, error)
	trashItem             func(string, *fsops.Operation) error
	historyCleanup        chan struct{}
	mouse                 mouseSelection
	mouseAnchors          [2]int
	mouseAnchorPath       [2]string
	documentScrollbarDrag bool
	terminalHistory       *terminal.History
	historyStatus         terminal.HistoryStatus
	historyWrite          chan terminal.HistoryStatus
	historyRead           chan terminal.HistoryPage
	historyPage           *terminal.HistoryPage
	historyQueue          [][]json.RawMessage
	historyQueuedBytes    int
	task                  *uiTask
	Async                 bool
	previewOperation      *fsops.Operation
	nextScreenID          int
	Operation             *fsops.Operation

	LastProgress                                              fsops.Progress
	FastFind                                                  *FastFind
	NewWindow                                                 bool
	closed                                                    bool
	dialogValues                                              map[string]string
	Fullscreen                                                bool
	DiskAccessSettings                                        bool
	Settings                                                  Settings
	folderHistory                                             []string
	viewHistory                                               []viewRecord
	Bookmarks                                                 map[int]bookmark // Folder and document bookmarks 0…9, per window.
	Image                                                     *imageView       // F3 graphical view of an image or PDF.
	mods                                                      modifiers        // Modifier keys held, for the key bar.
	Foreign                                                   foreignStream    // Stream shown in the terminal instead of the shell (exec, logs).
	foreignLabel                                              string
	foreignDone                                               func()
	foreignEnded                                              bool // The next key returns to the panels.
	modsAt                                                    time.Time
	previewKey, previewText                                   string
	previewPending                                            bool
	previewImage, previewPath, previewSent                    string
	lastMenu                                                  MenuState
	Panels                                                    [2]Panel
	Active                                                    int
	Cols, Rows                                                int
	Command, Status                                           string
	Dialog                                                    *Dialog
	Form                                                      *Form
	Documents                                                 []*Document
	Clipboard                                                 *string
	Menu                                                      *MenuState
	Doc                                                       *Document
	Term                                                      *terminal.Session
	TermVisible, ShellReady, Busy, Quit, dirty, editorChanged bool
	ShellCwd                                                  string
	shellPanelReturn                                          *shellPanelReturn
	pendingShellCwd                                           string
	shellStarted                                              bool
	shellRestartAt                                            time.Time
	shellRestartCwd                                           string
	pending                                                   []byte
	jobs                                                      chan func()
	history                                                   []string
	historyAt                                                 int
	formState
}

func New(left, right string) (*App, error)      { return newApp(left, right, false) }
func NewAsync(left, right string) (*App, error) { return newApp(left, right, true) }
func newApp(left, right string, async bool) (*App, error) {
	a := &App{Async: async, Settings: defaultSettings(), Cols: 120, Rows: 36, dirty: true, jobs: make(chan func(), 8)}
	for i := range a.Panels {
		a.Panels[i].Async = async
		a.configurePanel(i)
	}
	if err := a.Panels[0].Load(left); err != nil {
		return nil, err
	}
	if err := a.Panels[1].Load(right); err != nil {
		return nil, err
	}
	return a, nil
}
func (a *App) StartTerminal() error {
	return a.startTerminalAt(a.Panels[a.Active].filesystemPath())
}
func (a *App) startTerminalAt(cwd string) error {
	t, err := terminal.StartConfigured(cwd, a.Cols, a.Rows-1, a.Settings.Shell, a.Settings.ShellCustomPrompt)
	if err != nil {
		return err
	}
	a.Term = t
	a.shellStartedAt = time.Now()
	a.shellStopped = ""
	a.integrationSentAt = time.Time{}
	a.shellRestartAt = time.Time{}
	a.shellRestartCwd = ""
	a.shellStarted = false
	a.ShellReady = false
	a.ShellCwd = ""
	a.pendingShellCwd = ""
	return nil
}
func (a *App) Close() {
	if a.closed {
		return
	}
	a.closed = true
	a.stopSyntax()
	a.stopForeign()
	for i := range a.Panels {
		if a.Panels[i].Kube != nil {
			a.Panels[i].Kube.close()
		}
	}
	if a.terminalHistory != nil {
		h := a.terminalHistory
		a.historyCleanup = make(chan struct{})
		done := a.historyCleanup
		go func() { h.Close(); close(done) }()
	}
	for i := range a.Panels {
		a.Panels[i].cancelTree()
		a.Panels[i].cancelPanelLoad()
	}
	if a.previewOperation != nil {
		a.previewOperation.Cancel()
	}
	if a.Operation != nil {
		a.Operation.Cancel()
	}
	if a.task != nil {
		task := a.task
		a.task = nil
		go func() {
			result := <-task.done
			if result.cleanup != nil {
				result.cleanup()
			}
		}()
	}
	_ = a.autoSaveSetup()
	for _, d := range append(a.Documents, a.Doc) {
		if d != nil {
			if d.File != nil {
				d.File.Close()
			}
			if d.TempDir != "" {
				os.RemoveAll(d.TempDir)
			}
		}
	}
	if a.Term != nil {
		a.Term.Close()
	}
}
func (a *App) refresh() {
	a.previewKey = ""
	for i := range a.Panels {
		a.configurePanel(i)
		p := &a.Panels[i]
		if err := p.Load(p.Path); err != nil {
			p.Err = err.Error()
		}
	}
	a.dirty = true
}
func (a *App) error(err error) {
	if err != nil {
		a.Status = err.Error()
		a.dirty = true
	}
}
func (a *App) prompt(title, msg, value string, fn func(string)) {
	if value == "" && a.Settings.RememberDialogValues {
		value = a.dialogValues[title]
	}
	a.Dialog = &Dialog{Selected: a.Settings.SelectDialogInput, Title: title, Message: msg, Input: value, Confirm: func(s string) {
		a.activePrompt = &promptSpec{title, msg, fn}
		a.promptRejected = false
		fn(s)
		// A value rejected by validation is not remembered (P5-15).
		if a.Settings.RememberDialogValues && !a.promptRejected {
			if a.dialogValues == nil {
				a.dialogValues = map[string]string{}
			}
			a.dialogValues[title] = s
		}
	}}
	a.dirty = true
}

// RequestQuit asks to quit; RequestClose asks to close only this window.
func (a *App) RequestQuit()  { a.requestQuit(false) }
func (a *App) RequestClose() { a.requestQuit(true) }
func (a *App) requestQuit(closeWindow bool) {
	if a.Busy {
		a.Status = "Wait for the file operation to finish"
		a.dirty = true
		return
	}
	// An open form is cancelled like Esc before the quit confirmation replaces it.
	if f := a.Form; f != nil {
		a.Form = nil
		if f.Cancel != nil {
			f.Cancel()
		}
		if a.Form != nil {
			a.Status = "Close the open dialog first"
			a.dirty = true
			return
		}
	}
	confirmShell := func() { a.confirmQuitShell(closeWindow) }
	dirty := false
	for _, d := range append(append([]*Document{}, a.Documents...), a.Doc) {
		if d != nil && d.Dirty {
			dirty = true
		}
	}
	if dirty {
		a.TermVisible = false
		message := "Save changes in open documents before quitting?"
		if closeWindow {
			message = "Save changes in open documents before closing this window?"
		}
		a.confirm("Save changes", message, []string{"Save all", "Discard", "Cancel"}, func(i int) {
			if i == 2 {
				return
			}
			if i == 0 {
				a.saveAllThen(append(append([]*Document{}, a.Documents...), a.Doc), confirmShell)
				return
			}
			confirmShell()
		})
		return
	}
	confirmShell()
}
func (a *App) confirmQuitShell(closeWindow bool) {
	if a.Term != nil && !a.ShellReady {
		a.TermVisible = false
		title, message := "Quit", "A command is running. Close the application?"
		if closeWindow {
			title, message = "Close window", "A command is running. Close this window?"
		}
		a.confirm(title, message, []string{"Yes", "No"}, func(i int) {
			if i == 0 {
				a.Quit = true
			}
		})
		return
	}
	a.Quit = true
}
func (a *App) Handle(raw string) {
	var m Message
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	if m.Type == "resize" && max(60, min(300, m.Cols)) == a.Cols && max(15, min(120, m.Rows)) == a.Rows {
		return
	}
	// Terminal protocol traffic must continue while a file operation is running.
	switch m.Type {
	case "mouseUp":
		a.mouse.active = false
		a.documentScrollbarDrag = false
		return
	case "terminalError":
		a.Status = "Terminal renderer: " + m.Text
		return
	case "commandHistory":
		// Always answer, so the overlay never waits for a reply that cannot come.
		if a.Term == nil {
			a.shellCommands = &[]string{}
			a.commandsError = "Cannot read shell history: shell unavailable"
		} else if a.ShellReady {
			if err := a.Term.RequestCommands(a.ShellCwd); err != nil {
				a.error(err)
				a.shellCommands = &[]string{}
				a.commandsError = "Cannot read shell history: " + err.Error()
			} else {
				a.ShellReady = false
				a.integrationSent("", true)
			}
		}
		return
	case "commandHistoryRun":
		if a.Term != nil && a.ShellReady && strings.TrimSpace(m.Text) != "" {
			if err := a.Term.Run(a.ShellCwd, m.Text); err != nil {
				a.error(err)
			} else {
				a.ShellReady = false
				a.integrationSent("", false)
			}
		}
		return
	case "historyAppend":
		a.appendTerminalHistory(m.History)
		return
	case "historyRead":
		a.readTerminalHistory(m)
		return
	case "modifiers":
		if mods := modifiersOf(m); mods != a.mods {
			a.mods = mods
			a.dirty = true
		}
		a.modsAt = time.Now()
		return
	case "inputBinary":
		if data, err := base64.StdEncoding.DecodeString(m.Text); err == nil {
			if a.Foreign != nil {
				a.error(a.Foreign.Write(data))
			} else if a.TermVisible && a.foreignReturn() {
				return
			} else if a.Term != nil {
				a.error(a.Term.Write(string(data)))
			}
		}
		return
	}
	a.dirty = true
	if a.Busy && m.Type != "key" && m.Type != "click" && m.Type != "resize" && m.Type != "quit" && m.Type != "input" {
		return
	}
	switch m.Type {
	case "resize":
		cols, rows := max(60, min(300, m.Cols)), max(15, min(120, m.Rows))
		if cols == a.Cols && rows == a.Rows {
			return
		}
		a.Cols, a.Rows = cols, rows
		if a.Term != nil {
			a.error(a.Term.Resize(a.Cols, a.Rows-1))
		}
		if a.Foreign != nil {
			a.error(a.Foreign.Resize(a.Cols, a.Rows-1))
		}
	case "quit":
		a.RequestQuit()
	case "input":
		if a.Foreign != nil {
			a.error(a.Foreign.Write([]byte(m.Text)))
			return
		}
		if a.TermVisible && a.foreignReturn() {
			return
		}
		if a.Term == nil && strings.ContainsAny(m.Text, "\r\n") {
			a.retryShell()
		}
		if a.Term != nil {
			// Reserve the shell as soon as a command line is submitted, before
			// its preexec marker can arrive from the PTY reader.
			if strings.ContainsAny(m.Text, "\r\n") {
				a.ShellReady = false
			}
			a.error(a.Term.Write(m.Text))
		}
	case "copy":
		a.copyDocument(false)
	case "cut":
		a.copyDocument(true)
	case "selectAll":
		if a.Doc != nil && a.Dialog == nil && a.Form == nil {
			a.Doc.Anchor = 0
			a.Doc.Cursor = len(a.Doc.Text)
		} else {
			a.key(Message{Key: "a", Meta: true})
		}
	case "drag":
		a.dragMouse(m)
	case "wheel":
		a.scrollMouse(m)
	case "scroll":
		m.Delta = m.Y * 3
		a.scrollMouse(m)
	case "save":
		a.save()
	case "click":
		a.click(m)
	case "key":
		// A modifier report can be lost (focus changes); a key without the
		// held modifier after a quiet spell resets the key bar row.
		if a.mods != 0 && modifiersOf(m)&a.mods != a.mods && time.Since(a.modsAt) > 2*time.Second {
			a.mods = modifiersOf(m)
		}
		a.key(m)
	case "paste":
		if a.FastFind != nil && a.Form == nil && a.Dialog == nil && a.Doc == nil && a.Menu == nil {
			a.fastFindText(m.Text)
			return
		}
		if a.Form != nil {
			a.formPaste(m.Text)
			return
		}
		if a.Doc != nil && a.Doc.Edit && a.Dialog == nil {
			if a.Doc.ReadOnly {
				a.Status = "Read-only mode (⌘L to unlock)"
				a.dirty = true
				return
			}
			a.Doc.Insert(m.Text)
			a.Doc.modified()
			return
		}
		m.Text = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, m.Text)
		if a.Dialog != nil {
			if a.Dialog.Focus > 0 {
				return
			}
			if a.Dialog.Selected {
				a.Dialog.Input = ""
				a.Dialog.Selected = false
			}
			a.Dialog.insert(strings.ReplaceAll(m.Text, "\n", ""))
		} else if a.Doc == nil {
			a.Command += strings.ReplaceAll(m.Text, "\n", "")
		}
	}
}

// latinShortcut maps a Cmd/Ctrl letter typed in a non-Latin layout to the
// Latin letter of its physical key. Latin layouts (Dvorak, Colemak) keep Key.
func latinShortcut(m Message) Message {
	if (m.Meta || m.Ctrl) && len(m.Code) == 4 && strings.HasPrefix(m.Code, "Key") && utf8.RuneCountInString(m.Key) == 1 && m.Key[0] >= utf8.RuneSelf {
		m.Key = strings.ToLower(m.Code[3:])
		if m.Shift {
			m.Key = strings.ToUpper(m.Key)
		}
	}
	return m
}
func (a *App) key(m Message) {
	m = latinShortcut(m)
	if m.Alt && !m.Meta && !m.Ctrl && (a.Form != nil || a.Dialog != nil || a.Busy) && len(m.Code) == 4 && strings.HasPrefix(m.Code, "Key") {
		m.Key = strings.ToLower(m.Code[3:])
	}
	if a.Busy {
		if m.Key == "Escape" || m.Alt && strings.EqualFold(m.Key, "c") {
			a.cancelOperation()
		} else if a.Form != nil {
			a.formKey(m)
		}
		return
	}
	if m.Meta && strings.EqualFold(m.Key, "n") {
		a.NewWindow = true
		return
	}
	if m.Meta && strings.EqualFold(m.Key, "q") {
		a.RequestQuit()
		return
	}
	if a.fontSizeKey(m) {
		return
	}
	if a.Image != nil && a.Form == nil && a.Dialog == nil && !a.TermVisible {
		a.imageKey(m)
		return
	}
	if (m.Ctrl && m.Key == "Tab") || (m.Meta && m.Shift && (m.Key == "[" || m.Key == "]" || m.Key == "{" || m.Key == "}")) {
		a.cycleScreen(m.Shift && m.Key == "Tab" || m.Key == "[" || m.Key == "{")
		return
	}
	if (m.Meta || m.Ctrl) && !m.Alt && !m.Shift && strings.EqualFold(m.Key, "o") && a.Dialog == nil && a.Form == nil {
		a.Menu = nil
		a.FastFind = nil
		a.foreignEnded = false
		if !a.TermVisible {
			a.showTerminal()
		} else {
			a.TermVisible = false
		}
		if !a.TermVisible && a.Doc == nil {
			a.returnFromTerminal()
			a.refresh()
		}
		return
	}
	if a.TermVisible {
		return
	}
	if a.Form != nil {
		a.formKey(m)
		return
	}
	if a.Dialog != nil {
		d := a.Dialog
		if m.Alt {
			switch strings.ToLower(m.Key) {
			case "o":
				d.Focus = 1
				m.Key = "Enter"
			case "c":
				d.Focus = 2
				m.Key = "Enter"
			}
		}
		switch m.Key {
		case "Escape":
			a.Dialog = nil
		case "Enter":
			a.Dialog = nil
			if d.Focus != 2 {
				d.Confirm(d.Input)
			}
		case "Tab":
			delta := 1
			if m.Shift {
				delta = -1
			}
			d.Focus = (d.Focus + delta + 3) % 3
		case "ArrowLeft", "ArrowRight", "Home", "End":
			if d.Focus > 0 {
				d.Focus = 3 - d.Focus
			} else {
				d.Selected = false
				d.moveCaret(m.Key)
			}
		case "Backspace", "Delete":
			if d.Focus > 0 {
				return
			}
			if d.Selected {
				d.Input = ""
				d.Selected = false
				d.Back = 0
				return
			}
			d.deleteAtCaret(m.Key == "Backspace")
		default:
			if d.Focus > 0 {
				if m.Key == " " {
					a.key(Message{Key: "Enter"})
				}
				return
			}
			if m.Meta && strings.EqualFold(m.Key, "a") {
				d.Selected = true
			} else if len([]rune(m.Key)) == 1 && !m.Meta && !m.Ctrl {
				if d.Selected {
					d.Input = ""
					d.Selected = false
					d.Back = 0
				}
				d.insert(m.Key)
			}
		}
		return
	}
	if a.Doc != nil {
		a.documentKey(m)
		return
	}
	if a.Busy {
		if m.Key == "Escape" {
			a.cancelOperation()
		}
		return
	}
	if a.Menu == nil && a.infoPanelKey(m) {
		return
	}
	// Printable Option shortcuts and an existing query take precedence over
	// selection punctuation. Explicit numeric-keypad shortcuts remain intact.
	if a.Menu == nil && !m.Ctrl && !m.Meta && len([]rune(m.Key)) == 1 && !strings.HasPrefix(m.Code, "Numpad") && a.fastFindKey(m) {
		return
	}
	if a.Menu == nil && a.selectionKey(m) {
		return
	}
	if p := &a.Panels[a.Active]; a.Menu == nil && p.kind() != backendLocal && p.backend().Key(a, p, m) {
		return
	}
	if a.Menu == nil && a.panelBookmarkKey(m) {
		return
	}
	if a.Menu == nil && !m.Meta && !m.Ctrl && !m.Alt && (m.Key == "Delete" || m.Key == "F8" && m.Shift) {
		a.FastFind = nil
		a.deleteSelected(m.Shift)
		return
	}
	if a.Menu == nil && m.Key == "Enter" && m.Shift && !m.Meta && !m.Ctrl && !m.Alt {
		a.FastFind = nil
		a.openSystemSelected()
		return
	}
	if a.Foreign != nil && m.Meta && !m.Shift && !m.Ctrl && !m.Alt && (m.Code == "Period" || m.Key == ".") {
		a.stopForeign()
		return
	}
	if a.Menu == nil && m.Meta && m.Shift && !m.Ctrl && !m.Alt && (m.Code == "Period" || m.Key == "." || m.Key == ">") {
		a.FastFind = nil
		a.Settings.ShowHidden = !a.Settings.ShowHidden
		for i := range a.Panels {
			a.Panels[i].ForcedVisiblePath = ""
			a.configurePanel(i)
		}
		a.refresh()
		return
	}
	if a.Menu == nil && a.fastFindKey(m) {
		return
	}
	if (m.Key == "F3" || m.Key == "F4") && m.Shift && (m.Meta || m.Ctrl) {
		a.openConfigured(m.Key == "F4", false, true)
		return
	}
	if a.Menu == nil && a.farPanelKey(m) {
		return
	}
	if (m.Meta || m.Ctrl) && !m.Alt && a.Menu == nil {
		if m.Shift && (m.Key == "\\" || m.Key == "|") {
			a.matchOtherPanel()
			return
		}
		switch m.Key {
		case "F1":
			a.togglePanel(0)
			return
		case "F2":
			a.togglePanel(1)
			return
		case "=":
			a.matchOtherPanel()
			return
		}
	}
	if m.Alt {
		switch m.Key {
		case "F1":
			a.volumes(0)
			return
		case "F2":
			a.volumes(1)
			return
		case "F3", "F4":
			a.openConfigured(m.Key == "F4", true, false)
			return
		case "F7":
			a.findFiles(false)
			return
		case "F8":
			a.historyMenu("Commands")
			return
		case "F10":
			a.treeMenu()
			return
		}
	}
	if m.Key == "F10" && m.Shift {
		last := a.lastMenu
		a.Menu = &last
		return
	}
	if m.Key == "F4" && m.Shift {
		a.newFile()
		return
	}
	if m.Key == "F12" {
		a.screenMenu()
		return
	}
	if a.Menu != nil {
		a.menuKey(m)
		return
	}
	if m.Meta {
		switch strings.ToLower(m.Key) {
		case " ":
			a.completeCommand()
		case "enter":
			a.insertFilename()
		case "u":
			a.Panels[0], a.Panels[1] = a.Panels[1], a.Panels[0]
			a.previewKey = ""
		case "arrowup":
			a.historyAt = max(0, a.historyAt-1)
			if a.historyAt < len(a.history) {
				a.Command = a.history[a.historyAt]
			}
		case "arrowdown":
			a.historyAt = min(len(a.history), a.historyAt+1)
			if a.historyAt < len(a.history) {
				a.Command = a.history[a.historyAt]
			} else {
				a.Command = ""
			}
		case "r":
			a.rereadPanel(a.Active)
		case "l":
			a.gotoPrompt()
		case "q":
			a.RequestQuit()
		}
		return
	}
	if strings.HasPrefix(m.Key, "F") {
		if n, err := strconv.Atoi(strings.TrimPrefix(m.Key, "F")); err == nil {
			a.function(n)
			return
		}
	}
	p := &a.Panels[a.Active]
	switch m.Key {
	case "Tab":
		a.switchPanels()
	case "ArrowUp":
		if m.Ctrl {
			a.historyAt = max(0, a.historyAt-1)
			if a.historyAt < len(a.history) {
				a.Command = a.history[a.historyAt]
			}
		} else {
			p.Cursor = max(0, p.Cursor-1)
		}
	case "ArrowDown":
		if m.Ctrl {
			a.historyAt = min(len(a.history), a.historyAt+1)
			if a.historyAt < len(a.history) {
				a.Command = a.history[a.historyAt]
			} else {
				a.Command = ""
			}
		} else {
			p.Cursor = max(0, min(len(p.Entries)-1, p.Cursor+1))
		}
	case "PageUp":
		p.Cursor = max(0, p.Cursor-(a.Rows-9)*panelColumns(p))
	case "PageDown":
		p.Cursor = max(0, min(len(p.Entries)-1, p.Cursor+(a.Rows-9)*panelColumns(p)))
	case "ArrowLeft":
		if panelColumns(p) > 1 {
			p.Cursor = max(0, p.Cursor-(a.Rows-9))
		}
	case "ArrowRight":
		if panelColumns(p) > 1 {
			p.Cursor = min(len(p.Entries)-1, p.Cursor+a.Rows-9)
		}
	case "Home":
		p.Cursor = 0
	case "End":
		p.Cursor = max(0, len(p.Entries)-1)
	case "Backspace":
		if a.Command != "" {
			r := []rune(a.Command)
			a.Command = string(r[:len(r)-1])
		}
	case "Insert":
		a.mark()
	case " ":
		if a.Command == "" {
			a.mark()
		} else {
			a.Command += " "
		}
	case "Escape":
		if a.cancelReads() {
			return
		}
		a.Command = ""
		a.Status = ""
	case "Enter":
		if strings.TrimSpace(a.Command) != "" {
			a.execute()
		} else {
			a.Command = ""
			a.enterFile()
		}
	default:
		if m.Ctrl && strings.EqualFold(m.Key, "u") {
			a.Panels[0], a.Panels[1] = a.Panels[1], a.Panels[0]
		} else if len([]rune(m.Key)) == 1 && !m.Ctrl && !m.Alt {
			a.Command += m.Key
		}
	}
}
func (a *App) mark() {
	if !a.requireFilePanel() {
		return
	}
	p := &a.Panels[a.Active]
	if len(p.Entries) > 0 {
		e := p.Entries[p.Cursor]
		if e.Name != ".." {
			p.Marked[e.Name] = !p.Marked[e.Name]
		}
		p.Cursor = min(len(p.Entries)-1, p.Cursor+1)
	}
}
func (a *App) gotoPrompt() {
	p := &a.Panels[a.Active]
	a.prompt("Change directory", "Enter a directory", p.Path, func(s string) { a.error(p.Load(expand(s, p.Path))) })
}
func expand(s, base string) string {
	if s == "~" || strings.HasPrefix(s, "~/") {
		home, _ := os.UserHomeDir()
		s = filepath.Join(home, strings.TrimPrefix(s, "~"))
	}
	if !filepath.IsAbs(s) {
		s = filepath.Join(base, s)
	}
	return filepath.Clean(s)
}
func (a *App) function(n int) {
	p := &a.Panels[a.Active]
	switch n {
	case 1:
		d := newDocument("FarshMac — Help", help)
		d.Help = true
		d.markSaved()
		a.showDocument(d)
	case 2:
		a.userMenu()
	case 3, 4:
		if n == 4 && !p.can(capEdit) && p.backend().Open(a, p, true) {
			return
		}
		a.openConfigured(n == 4, false, false)
	case 5, 6:
		a.transferDialog(n == 6)
	case 7:
		if !a.requireFilePanel() {
			return
		}
		if p.backend().Mkdir(a, p) {
			return
		}
		base, index := p.Path, a.Active
		a.prompt("Make folder", "Folder name", "", func(s string) {
			if strings.TrimSpace(s) == "" {
				return
			}
			path := expand(s, base)
			var create func()
			create = func() {
				if err := os.MkdirAll(path, 0755); err != nil {
					a.confirm("Error", "Cannot create folder\n"+path+"\n\n"+err.Error(), []string{"Retry", "Cancel"}, func(i int) {
						if i == 0 {
							create()
						}
					})
					return
				}
				a.refresh()
				a.focusCreatedDirectory(index, path)
			}
			create()
		})

	case 8:
		a.deleteSelected(false)

	case 9:
		column := 0
		if a.Active == 1 {
			column = 4
		}
		a.Menu = &MenuState{Column: column}
	case 10:
		// Application exit is exclusively Cmd+Q / the native window control.
	}
}
func (a *App) execute() {
	if a.Foreign != nil {
		a.Status = "A command is running in the container; ⌘O to see it, ⌘. to stop it"
		a.TermVisible = true
		return
	}
	if p := &a.Panels[a.Active]; p.can(capExecute) {
		if command := a.Command; strings.TrimSpace(command) != "" && p.backend().Execute(a, p, command) {
			a.history = appendUnique(a.history, command, a.Settings.HistoryLimit)
			a.historyAt = len(a.history)
			a.Command = ""
			return
		}
	}
	if a.Term == nil {
		if a.retryShell() || !a.shellRestartAt.IsZero() {
			a.Status = "Shell unavailable; retrying…"
		} else {
			a.Status = "Shell unavailable"
		}
		return
	}
	if !a.ShellReady {
		a.Status = "Shell is busy; ⌘O to return to it"
		a.TermVisible = true
		return
	}
	command := a.Command
	path := a.Panels[a.Active].filesystemPath()
	if err := a.Term.Run(path, command); err != nil {
		a.error(err)
		return
	}
	a.history = appendUnique(a.history, command, a.Settings.HistoryLimit)
	a.historyAt = len(a.history)
	a.Command = ""
	a.TermVisible = true
	a.ShellReady = false
	a.integrationSent(command, false)
}

// A directory request waits for the prompt; never send integration keys to a
// running program (ssh, editors, or commands consuming stdin).
func (a *App) showTerminal() {
	a.TermVisible = true
	a.retryShell()
	if r := a.shellPanelReturn; r != nil {
		a.shellPanelReturn = nil
		if r.index == a.Active && a.Panels[r.index].Path == r.path {
			return
		}
	}
	if a.Panels[a.Active].can(capShellSync) {
		a.pendingShellCwd = a.Panels[a.Active].filesystemPath()
		a.syncShellCwd()
	}
}
func (a *App) syncShellCwd() {
	if a.Term == nil || !a.ShellReady || a.pendingShellCwd == "" {
		return
	}
	target := a.pendingShellCwd
	a.pendingShellCwd = ""
	if target == a.ShellCwd {
		return
	}
	if err := a.Term.ChangeDir(target); err != nil {
		a.error(err)
		return
	}
	a.ShellReady = false
	a.integrationSent("", false)
}

type shellPanelReturn struct {
	index int
	path  string
}

func (a *App) returnFromTerminal() {
	if a.ShellReady {
		a.syncCwd()
		return
	}
	if a.Panels[a.Active].can(capShellSync) && a.pendingShellCwd == "" {
		a.shellPanelReturn = &shellPanelReturn{a.Active, a.Panels[a.Active].Path}
	}
}
func (a *App) finishPanelReturn() {
	r := a.shellPanelReturn
	if r == nil {
		return
	}
	a.shellPanelReturn = nil
	if !a.TermVisible && a.Active == r.index && a.Panels[r.index].Path == r.path && a.Panels[r.index].can(capShellSync) && a.ShellCwd != "" && !a.shellAtArchive(r.index) {
		a.error(a.Panels[r.index].Load(a.ShellCwd))
	}
}

// shellAtArchive reports whether panel i browses an archive and the shell is
// still in the archive's folder, so returning from the terminal keeps the
// position inside the archive. A shell cd elsewhere leaves the archive.
func (a *App) shellAtArchive(i int) bool {
	p := &a.Panels[i]
	if p.Archive == nil || p.Remote != nil {
		return false
	}
	if a.ShellCwd == "" {
		return true
	}
	shell, err := filepath.EvalSymlinks(a.ShellCwd)
	if err != nil {
		shell = a.ShellCwd
	}
	folder, err := filepath.EvalSymlinks(p.filesystemPath())
	if err != nil {
		folder = p.filesystemPath()
	}
	return shell == folder
}
func (a *App) syncCwd() {
	if !a.Panels[a.Active].can(capShellSync) || a.shellAtArchive(a.Active) {
		return
	}
	if a.ShellCwd != "" && a.ShellReady && a.pendingShellCwd == "" {
		a.error(a.Panels[a.Active].Load(a.ShellCwd))
	}
}
func (a *App) click(m Message) {
	if m.Button != 0 && (a.Form != nil || a.Dialog != nil || a.Doc != nil || a.Menu != nil || a.Busy) {
		return
	}
	if a.Image != nil && a.Form == nil && a.Dialog == nil && !a.TermVisible {
		return
	}
	a.FastFind = nil
	if a.Form != nil {
		a.formClick(m)
		return
	}
	if a.Busy {
		if a.Operation != nil {
			r := a.progressBounds()
			for _, pos := range buttonLayout([]string{"Cancel"}, r.W) {
				if m.Y == r.Y+r.H-4+pos.Y && m.X >= r.X+pos.X && m.X < r.X+pos.X+pos.W {
					a.cancelOperation()
				}
			}
		}
		return
	}
	if a.Doc != nil && !a.TermVisible {
		a.documentClick(m, false)
		return
	}
	if a.Dialog != nil {
		a.promptClick(m)
		return
	}

	if a.TermVisible {
		return
	}
	if m.Y == 0 && m.Button == 0 {
		for i, section := range a.menuSections() {
			if m.X >= section.X && m.X < section.X+len(section.Name) {
				a.Menu = &MenuState{Column: i}
				return
			}
		}
	}
	if a.Menu != nil {
		a.menuClick(m)
		return
	}
	if m.Y == a.Rows-3 && m.Button == 0 && a.Status != "" {
		a.showMessages()
		return
	}
	if m.Y == a.Rows-1 && m.Button == 0 {
		if !a.Settings.ShowKeys {
			return
		}
		a.function(min(10, m.X*10/a.Cols+1))
		return
	}
	if a.infoPanelClick(m) || a.quickViewClick(m) {
		return
	}
	a.panelMouse(m)
}
func (a *App) Poll() Update {
	a.pollTerminalHistory()
	a.pollTask()
	a.pollSyntax()
	a.pollManifestCheck()
	for {
		select {
		case fn := <-a.jobs:
			fn()
		default:
			goto jobsDone
		}
	}
jobsDone:
	for i := range a.Panels {
		if a.Panels[i].pollTree() || a.Panels[i].pollPanelLoad() {
			a.dirty = true
			a.noteInvalidRemoteNames(i)
		}
		if a.Panels[i].load != nil && !a.TermVisible {
			a.dirty = true // Animate the reading indicator.
		}
		if s := a.Panels[i].loadStatus; s != "" {
			a.Panels[i].loadStatus = ""
			a.Status = s
		}
	}
	if a.Operation != nil {
		a.dirty = true
	}
	a.updateInfoPanels()
	a.updatePreview()
	a.pollKubeWatches()
	if a.terminalCanDrain() {
		a.pollForeign()
	}
	if a.Term != nil && a.terminalCanDrain() {
		for i := 0; i < 8 && len(a.pending) < 256*1024; i++ {
			select {
			case e := <-a.Term.Events:
				a.pending = append(a.pending, e.Data...)
				if e.Running {
					a.ShellReady = false
					a.integrationSentAt = time.Time{}
					a.dirty = true
				}
				if e.Ready {
					a.integrationSentAt = time.Time{}
					a.shellFailures = 0
					if e.Commands != nil {
						a.shellCommands = &e.Commands
						a.commandsError = ""
					}
					a.ShellReady = true
					a.shellStarted = true
					a.ShellCwd = e.Cwd
					a.finishPanelReturn()
					a.setShellStatus(fmt.Sprintf("Exit %d · ⌘O panels / terminal", e.Status))
					a.dirty = true
				}
				if e.Exit {
					a.scheduleShellRestart(e.Err)
					goto termDone
				}
			default:
				goto termDone
			}
		}
	}
termDone:
	a.checkIntegrationWatchdog()
	a.restartShellIfDue()
	a.syncShellCwd()
	u := Update{Zoom: a.Zoom, History: a.historyStatus, HistoryPage: a.historyPage, NewWindow: a.NewWindow, PanelKeys: a.Doc == nil && a.Image == nil && a.Form == nil && a.Dialog == nil && a.Menu == nil && !a.Busy, FontSize: a.Settings.FontSize, Fullscreen: a.Fullscreen, Cols: a.Cols, Rows: a.Rows, Mode: "panels", Footer: a.Status, Quit: a.Quit, Document: a.Doc != nil || a.Image != nil, Clipboard: a.Clipboard}
	u.ShellReady = a.ShellReady
	u.Commands = a.shellCommands
	if u.Commands != nil {
		u.CommandsError = a.commandsError
		a.commandsError = ""
	}
	a.shellCommands = nil
	u.Title = a.Panels[a.Active].Path
	u.DiskAccessSettings = a.DiskAccessSettings
	if a.Doc != nil {
		u.Title = filepath.Base(a.Doc.displayPath())
	}
	if a.Image != nil {
		u.Title = filepath.Base(a.Image.Path)
	}
	a.historyPage = nil
	a.NewWindow = false
	a.Fullscreen = false
	a.Zoom = false
	a.DiskAccessSettings = false
	a.Clipboard = nil
	if a.TermVisible {
		u.Mode = "terminal"
		if a.Term == nil && a.shellStopped != "" {
			u.Footer = "⌘O  Panels    ·    Shell stopped: " + a.shellStopped + "    ·    Enter: retry"
		} else if !a.shellRestartAt.IsZero() {
			u.Footer = "⌘O  Panels    ·    " + a.Status
		} else if a.ShellReady {
			u.Footer = "⌘O  Panels    ·    Shell ready    ·    " + a.ShellCwd
		} else if !a.shellStarted {
			u.Footer = "⌘O  Panels    ·    Starting " + a.Settings.Shell + " (loading shell configuration)…    ·    ⌘. Interrupt"
		} else {
			u.Footer = "⌘O  Panels    ·    ⌘.  Interrupt"
		}
	}
	if len(a.pending) > 0 && a.terminalCanDrain() {
		n := min(len(a.pending), 16*1024)
		u.Terminal = base64.StdEncoding.EncodeToString(a.pending[:n])
		a.pending = a.pending[n:]
		if len(a.pending) == 0 {
			a.pending = nil
		}
	}
	if a.dirty && !a.TermVisible {
		u.Grid = a.Draw().ANSI()
		u.Preview = a.imagePreview()
		a.dirty = false
	}
	return u
}

const help = `FarshMac — native macOS window, Go file manager, persistent shell
(bash by default; sh or zsh in F9 > Options > Terminal settings)

Ctrl/⌘: shortcuts written Cmd/⌘ also work with Ctrl. Ctrl only: Ctrl+Q,
Ctrl+L, Ctrl+H, Ctrl+E/X. Cmd only: ⌘0..9 and ⌘⌥0..9 bookmarks, ⌘+/−.
Letter shortcuts follow the physical key on non-Latin keyboard layouts.

Tab                 Switch active panel (skips a hidden panel)
Arrows / PgUp/PgDn   Navigate files
Shift+arrows/PgUp/PgDn/Home/End  Mark a range of files
Quick view / Info / Tree / hidden panel: file keys need a file panel (Tab)
Ctrl+Q             Toggle inactive Quick view (restore previous view)
Ctrl+L / Ctrl+T    Toggle inactive Info / Tree
Ctrl+P / Ctrl+B    Toggle inactive panel / key bar
Ctrl+F3..F11       Sort (name, ext, modified, size, unsorted, created, accessed, description, owner)
Ctrl+F12           Sort menu; Shift+F12 selected first; Shift+F11 sort groups
Left/Right menu    Brief, Medium, Full, Wide, Detailed, Info, Tree, Quick view
Shift+F5/F6        Copy/move only the current item
Shift+F1/F2/F3     Archive create/extract/commands
Shift+F9           Save setup
Alt+F6             Link (symbolic, relative by default / hard); an existing
                   folder as the name creates the link inside it
Alt+F7 / Alt+F8    Find file / command history (inserts, does not run)
Alt+F10            Folder tree
Alt+F11/F12        View/edit history / folder history
Ctrl+PgUp/PgDown   Parent / enter folder or archive
Ctrl+Left/Right    Panel width (empty command); Ctrl+Num5 reset
Ctrl+E/X/Y         Previous / next command / clear
Ctrl+↑/↓, ⌘↑/↓     Command history
Cmd+F / Cmd+;      Insert full path from active / inactive panel
Ctrl+[ / Ctrl+]    Insert left / right panel path (+Shift: active / inactive)
Cmd+Enter          Insert current filename; Cmd+Shift+Enter other panel's
Ctrl+Insert / ⌘C   Copy command or selected names (on .. the panel path)
Alt+Shift+Insert   Copy full paths; Ctrl+Alt+Insert real paths (links resolved)
Shift+Insert       Paste text
Num + / Num -      Select / deselect by masks
Num *              Invert files; Cmd/Ctrl+Num * includes folders
Alt+Num *          Invert files and unmark folders
Cmd/Ctrl+Num +/-   Select / deselect same extension
Alt+Num +/-        Select / deselect same base name
Shift+Num +/-      Select / deselect all files
Cmd/Ctrl+M         Restore previous selection
Cmd+Option+0..9     Save bookmark: current folder and the file under the cursor
Cmd+0..9            Go to bookmark (keeps remembered cursor positions and marks).
                    The same keys work in the viewer/editor. Bookmarks belong
                    to the window: a document bookmark opens that file at the
                    saved position (nothing if the file is gone)
Network connections: F9 > Commands > Network connections
Kubernetes panel:   New Kubernetes cluster… (kubeconfig + context). Folders:
                    namespaces > resource types > objects > owned objects > pod
                    > containers > files. F3/F4 manifest (F4 saves with Update),
                    F5 export manifests, F7 new namespace, F8 delete (Shift+F8
                    keeps children), Shift+F3 logs, Alt+F3 follow logs, Shift+F4
                    describe, Cmd+R refresh, F9 > Kubernetes > Open shell.
                    In a container: files as on a server; a typed command runs
                    in the container (Cmd+. stops it)
FTP / FTPS / SFTP    Remote panel; terminal stays local
F5 Parallel transfers: 1-8 workers, Esc cancels

Enter               Open directory / view file / execute command
Shift+Enter         Open selected item with macOS (folders in Finder, .app launches;
                    Enter on .app enters the bundle)
Cmd+Shift+. / Ctrl+H Show / hide hidden files (also in Panel settings)
Cmd+Shift+S         Human-readable / exact sizes
Backspace           Delete command-line character; never leave directory
Space / Insert      Mark file; operations apply to marked files
F3 on a folder      Calculate marked/current folder sizes; participates in Size sort
F4 on a folder      File attributes (permissions, modified)
⌘Option+F3         Calculate all folder sizes (~ = cached, possibly outdated)
Info: Ctrl+F12      Configure sections; PgUp/PgDn/wheel scroll; F3/F4 description
F3 / F4             Viewer / editor (F1 inside for full key reference)
F3 on image / PDF   Graphical view: black background, picture centred, Esc closes
Alt+F3 / Alt+F4     Alternate viewer / editor (external vs internal)
Cmd+Shift+F3/F4     Internal viewer / editor
Shift+F4            New file
F5 / F6             Copy / move or rename (asks before overwriting)
                    Destination ending in / that does not exist: new folder
F7                  Make folder
F8 / Delete         Move selected items to Trash
Shift+F8 / Shift+Del Delete permanently (always asks)
F9 / Cmd+9          Main menu (arrows, Enter, Esc)
Cmd+L              Change directory
F2 / Cmd+2          User menu
Cmd+R              Re-read active panel (F9 > Left/Right > Re-read)
Cmd+Space          Complete filename in command line
⌘U              Swap panels
⌘F1 / ⌘F2     Hide/show left / right panel
Alt+F1 / Alt+F2    Choose left / right volume
⌘Shift+\ or ⌘=  Open active folder in the other panel
⌘+ / ⌘−         Font size larger / smaller (10–24; ⌘= and ⌘0 default in terminal)
⌃⌘F             Full screen
Enter on archive  Browse archive; .. returns to parent
F5 in archive     Copy selected files/folders out; F3 previews
⌘O              Toggle panels and persistent terminal
Cmd+1 … Cmd+9       Alternatives to F1 … F9 on Mac keyboards
Cmd+N / Cmd+W      New / close native window (new windows copy histories)
Cmd+Q              Quit ALL windows
F10                Closes the viewer/editor; in panels it does nothing
Alt+characters     Fast find (Cmd+Enter next; Cmd+Shift+Enter previous)
F12                Panels and document screens
Esc during operation  Cancel the whole operation
Status line click  Last 20 messages (also F9 > Commands > Last messages)

Type a shell command directly while panels are visible, then press Enter.
The panels disappear and the SAME window becomes an interactive terminal.
Commands run in the active panel's directory. The shell keeps its environment,
history, aliases and jobs. Your shell's startup files are loaded.
The output stays visible after completion; ⌘O restores the panels.
⌘O also works during a command; returning does not stop the job.
Use Cmd+. (or Ctrl+C) to interrupt, Cmd+C / Cmd+V to copy / paste.
Scroll the terminal to review output. vim/less use the alternate screen.
If the shell exits it restarts (1/2/4/8 s backoff; stops after 5 failed
starts): press Enter or ⌘O to retry.

This is an initial independent implementation, not full Far compatibility.
Read-only archive panels are supported. No Windows DLL plugins or macros.
Cross-volume moves copy each file with metadata, ask before any loss, then
delete the source.
Copy options: permissions/ACL, xattrs/flags/timestamps (F5 dialog).
Symbolic links (F5/F6, local): Smart keeps links pointing at the same object
(relative links to outside the selection are rewritten), As is copies the
link text, Copy contents copies what links point to. Hard-linked files
copied together stay hard links.
The text editor saves atomically and keeps xattrs, ACLs and hidden/nodump
flags; it warns "Saved; some attributes were not preserved" otherwise.
Unwritable folders: "Save in place" (not atomic, keeps hard links).
Text editing/decoding: 64 MiB limit. Larger files: paged hex/dump viewer.
Esc returns to the file panels.`

func (a *App) insertFilename() {
	if !a.requireFilePanel() {
		return
	}
	p := &a.Panels[a.Active]

	if p.Cursor < 0 || p.Cursor >= len(p.Entries) {
		return
	}
	name := p.Entries[p.Cursor].Name
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		a.Status = "Cannot insert a filename containing control characters"
		return
	}
	if strings.HasPrefix(name, "-") {
		name = "./" + name
	}
	if strings.IndexFunc(name, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_./-", r))
	}) >= 0 {
		name = terminal.Quote(name)
	}
	if a.Command != "" && !strings.HasSuffix(a.Command, " ") && !strings.HasSuffix(a.Command, "\t") {
		a.Command += " "
	}
	a.Command += name + " "
}

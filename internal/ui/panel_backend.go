package ui

import (
	"os"
	"path/filepath"
	"strconv"

	"farshmac/internal/remotefs"
)

// backendKind identifies what a panel shows. Use it only where the identity
// of the source matters (messages, tests); behaviour belongs to panelCaps and
// the panelBackend methods.
type backendKind uint8

const (
	backendLocal backendKind = iota
	backendArchive
	backendRemote
	backendKube
)

// panelCaps says what the panel at its current path can do, so actions ask
// for a capability instead of testing the kind of source.
type panelCaps uint32

const (
	capEdit         panelCaps = 1 << iota // F4 edits the current entry
	capMkdir                              // F7
	capDelete                             // F8 / Shift+F8
	capTrash                              // F8 moves to Trash (otherwise deletes)
	capRename                             // F6 renames or moves within the source
	capCopyIn                             // the panel can be the destination of F5
	capFolderSize                         // F3 on a folder, Cmd+Option+F3
	capQuickView                          // the other panel's Quick view can preview the current entry
	capArchiveEnter                       // Enter / Ctrl+PgDn opens archives as folders
	capShellSync                          // Cmd+O syncs the shell folder; commands run here
	capSystemOpen                         // Shift+Enter opens with macOS
	capMetadataSort                       // Created / Accessed / Owner / Description sorts
	capTree                               // Tree panel view
	capInfo                               // Info panel view
	capLinks                              // Alt+F6 creates links
	capArchiveOps                         // Shift+F1..F3 archive commands
	capFind                               // Alt+F7 find files / folders
	capUserMenu                           // F2 user menu, file associations, Apply command
	capAttributes                         // File attributes, Describe files
	capNewFile                            // Shift+F4 creates a file
	capLocalOutput                        // FilesystemPath is a sensible place for local output (archives)
)

// modifiers is the set of modifier keys currently held, reported by the
// renderer so the key bar can show Shift/Ctrl/Alt rows like Far Manager.
type modifiers uint8

const (
	modShift modifiers = 1 << iota
	modCtrl
	modAlt
	modMeta
)

func modifiersOf(m Message) modifiers {
	var mods modifiers
	if m.Shift {
		mods |= modShift
	}
	if m.Ctrl {
		mods |= modCtrl
	}
	if m.Alt {
		mods |= modAlt
	}
	if m.Meta {
		mods |= modMeta
	}
	return mods
}

// panelBackend is the strategy behind a panel: local folders, an archive, a
// remote server or a Kubernetes cluster. Panel keeps the source-specific
// state (Remote, Archive, Kube); the backend interprets it.
type panelBackend interface {
	Kind() backendKind
	// Label prefixes the panel title and history entries ("", "[Archive] ", "sftp://user@host").
	Label(p *Panel) string
	Caps(p *Panel) panelCaps
	KeyBar(p *Panel, mods modifiers) [10]string
	// FilesystemPath is the local folder behind the panel: the shell's folder
	// and the base for relative paths typed into dialogs.
	FilesystemPath(p *Panel) string
	// Virtual reports whether path (an entry of this panel) is not a local
	// filesystem path, so macOS and shell commands cannot use it directly.
	Virtual(p *Panel, path string) bool
	Endpoint(p *Panel) remotefs.Endpoint
	// HistoryEntry names a visited folder in the folder history with its source.
	HistoryEntry(p *Panel, abs string) string
	// SizeSource keys the folder size cache; sizes never cross sources.
	SizeSource(p *Panel) string
	// InfoPlugin describes the source in the Info panel ("" for local).
	InfoPlugin(p *Panel) string
	// DocumentSource gives the viewer/editor access to a file of this panel
	// that is not a local file; nil means path is a local file.
	DocumentSource(p *Panel, path string) documentSource
	// Open views or edits the current entry; false leaves it to the local code.
	Open(a *App, p *Panel, edit bool) bool
	// Enter handles Enter on the current entry; false leaves it to the local code.
	Enter(a *App, p *Panel) bool
	// Mkdir, Delete, Move handle F7, F8 and F6; false leaves them to the local code.
	Mkdir(a *App, p *Panel) bool
	Delete(a *App, p *Panel, permanent bool) bool
	Move(a *App, p *Panel) bool
	// MenuSection is an extra F9 menu for this source, or nil.
	MenuSection(a *App, p *Panel) *menuSection
}

func (p *Panel) backend() panelBackend {
	switch {
	case p.Kube != nil:
		return kubeBackend{}
	case p.Remote != nil:
		return remoteBackend{}
	case p.Archive != nil:
		return archiveBackend{}
	}
	return localBackend{}
}
func (p *Panel) kind() backendKind { return p.backend().Kind() }
func (p *Panel) caps() panelCaps   { return p.backend().Caps(p) }
func (p *Panel) can(c panelCaps) bool {
	return p.caps()&c == c
}
func (p *Panel) label() string { return p.backend().Label(p) }
func (p *Panel) virtual(path string) bool {
	return p.backend().Virtual(p, path)
}
func (p *Panel) endpoint() remotefs.Endpoint { return p.backend().Endpoint(p) }

// served reports a panel whose entries live on a server (remote or
// Kubernetes), not in the local file system or an archive on it.
func (p *Panel) served() bool {
	k := p.kind()
	return k == backendRemote || k == backendKube
}

// Key bar rows shared by the sources: Far Manager's local table.
var (
	keyBarLocal = [10]string{"Help", "UserMn", "View", "Edit", "Copy", "RenMov", "MkFold", "Trash", "Menu", ""}
	keyBarShift = [10]string{"AddArc", "ExtrAr", "ArcCmd", "", "Copy", "Rename", "", "Delete", "Save", ""}
	keyBarCtrl  = [10]string{"", "", "Name", "Ext", "Modif", "Size", "Unsort", "Creat", "Access", "Descr"}
	keyBarAlt   = [10]string{"", "", "", "", "", "", "Find", "Histry", "", ""}
)

// keyBarFor picks the row for the held modifiers; Cmd shows the Ctrl row
// because Cmd+F-keys sort like Ctrl+F-keys here.
func keyBarFor(mods modifiers, plain, shift, ctrl, alt [10]string) [10]string {
	switch {
	case mods&modShift != 0 && mods&(modCtrl|modMeta|modAlt) == 0:
		return shift
	case mods&(modCtrl|modMeta) != 0 && mods&(modShift|modAlt) == 0:
		return ctrl
	case mods&modAlt != 0 && mods&(modShift|modCtrl|modMeta) == 0:
		return alt
	case mods == 0:
		return plain
	}
	return [10]string{}
}

// localBackend: ordinary folders.
type localBackend struct{}

func (localBackend) Kind() backendKind              { return backendLocal }
func (localBackend) Label(*Panel) string            { return "" }
func (localBackend) Caps(*Panel) panelCaps          { return ^panelCaps(0) }
func (localBackend) FilesystemPath(p *Panel) string { return p.Path }
func (localBackend) Virtual(*Panel, string) bool    { return false }
func (localBackend) Endpoint(*Panel) remotefs.Endpoint {
	return remotefs.Endpoint{}
}
func (localBackend) HistoryEntry(_ *Panel, abs string) string { return abs }
func (localBackend) SizeSource(*Panel) string                 { return "local" }
func (localBackend) InfoPlugin(*Panel) string                 { return "" }
func (localBackend) KeyBar(_ *Panel, mods modifiers) [10]string {
	return keyBarFor(mods, keyBarLocal, keyBarShift, keyBarCtrl, keyBarAlt)
}
func (localBackend) DocumentSource(*Panel, string) documentSource { return nil }
func (localBackend) Open(*App, *Panel, bool) bool                 { return false }
func (localBackend) Enter(*App, *Panel) bool                      { return false }
func (localBackend) Mkdir(*App, *Panel) bool                      { return false }
func (localBackend) Delete(*App, *Panel, bool) bool               { return false }
func (localBackend) Move(*App, *Panel) bool                       { return false }
func (localBackend) MenuSection(*App, *Panel) *menuSection        { return nil }

// archiveBackend: a read-only archive opened as a folder tree.
type archiveBackend struct{}

func (archiveBackend) Kind() backendKind { return backendArchive }
func (archiveBackend) Label(*Panel) string {
	return "[Archive] "
}
func (archiveBackend) Caps(*Panel) panelCaps {
	return capFolderSize | capQuickView | capShellSync | capSystemOpen | capMetadataSort | capInfo | capArchiveOps | capFind | capUserMenu | capAttributes | capLocalOutput
}
func (archiveBackend) FilesystemPath(p *Panel) string { return filepath.Dir(p.Archive.Path) }
func (archiveBackend) Virtual(p *Panel, path string) bool {
	_, inside := p.Archive.Relative(path)
	return inside
}
func (archiveBackend) Endpoint(*Panel) remotefs.Endpoint { return remotefs.Endpoint{} }
func (archiveBackend) HistoryEntry(p *Panel, abs string) string {
	if _, inside := p.Archive.Relative(abs); inside {
		return archiveHistoryPrefix + abs
	}
	return abs
}
func (archiveBackend) SizeSource(p *Panel) string {
	// A reopened archive gets a new index; never reuse sizes from old contents.
	return "archive:" + itoa64(p.sizeArchiveGeneration) + ":" + p.Archive.Path
}
func (archiveBackend) InfoPlugin(p *Panel) string { return "Archive: " + p.Archive.Path }
func (archiveBackend) KeyBar(_ *Panel, mods modifiers) [10]string {
	// Archives are read-only: F5 extracts, F4/F6/F7/F8 only explain that.
	plain := keyBarLocal
	plain[3], plain[4], plain[5], plain[6], plain[7] = "", "Extract", "", "", ""
	shift := keyBarShift
	shift[1], shift[4], shift[5], shift[7] = "Extract", "", "", ""
	return keyBarFor(mods, plain, shift, keyBarCtrl, keyBarAlt)
}
func (archiveBackend) DocumentSource(*Panel, string) documentSource { return nil }
func (archiveBackend) Open(a *App, p *Panel, edit bool) bool {
	if edit {
		a.archiveReadOnly()
	} else {
		a.viewArchiveMember()
	}
	return true
}
func (archiveBackend) Enter(a *App, p *Panel) bool {
	a.enterArchiveMember()
	return true
}
func (archiveBackend) Mkdir(a *App, _ *Panel) bool          { return a.archiveReadOnly() }
func (archiveBackend) Delete(a *App, _ *Panel, _ bool) bool { return a.archiveReadOnly() }
func (archiveBackend) Move(a *App, _ *Panel) bool           { return a.archiveReadOnly() }
func (archiveBackend) MenuSection(*App, *Panel) *menuSection {
	return nil
}

// remoteBackend: an FTP/FTPS/SFTP server.
type remoteBackend struct{}

func (remoteBackend) Kind() backendKind     { return backendRemote }
func (remoteBackend) Label(p *Panel) string { return p.Remote.Label() }
func (remoteBackend) Caps(*Panel) panelCaps {
	return capEdit | capMkdir | capDelete | capRename | capCopyIn | capFolderSize | capQuickView
}
func (remoteBackend) FilesystemPath(p *Panel) string { return p.RemoteLocalPath }
func (remoteBackend) Virtual(*Panel, string) bool    { return true }
func (remoteBackend) Endpoint(p *Panel) remotefs.Endpoint {
	return remotefs.RemoteEndpoint(p.Remote)
}
func (remoteBackend) HistoryEntry(p *Panel, abs string) string { return p.Remote.Label() + abs }
func (remoteBackend) SizeSource(p *Panel) string               { return p.Remote.Label() }
func (remoteBackend) InfoPlugin(p *Panel) string               { return p.Remote.Label() + p.Path }
func (remoteBackend) KeyBar(p *Panel, mods modifiers) [10]string {
	plain := keyBarLocal
	plain[7] = "Delete"
	shift := keyBarShift
	shift[0], shift[1], shift[2] = "", "", ""
	ctrl := keyBarCtrl
	ctrl[7], ctrl[8], ctrl[9] = "", "", "" // metadata sorts need local files
	alt := keyBarAlt
	alt[6] = ""
	return keyBarFor(mods, plain, shift, ctrl, alt)
}
func (remoteBackend) DocumentSource(p *Panel, path string) documentSource {
	return &remoteSource{Config: *p.Remote, Path: path}
}
func (remoteBackend) Open(a *App, p *Panel, edit bool) bool {
	a.openRemoteDocument(edit)
	return true
}
func (remoteBackend) Enter(a *App, p *Panel) bool {
	if len(p.Entries) == 0 {
		return true
	}
	if e := p.Entries[p.Cursor]; e.Dir {
		a.error(p.Load(p.Current()))
	} else if e.Info != nil && e.Info.Mode()&os.ModeSymlink != 0 {
		a.enterRemoteLink()
	} else {
		a.openRemoteDocument(false)
	}
	return true
}
func (remoteBackend) Mkdir(a *App, _ *Panel) bool {
	a.remoteMkdir()
	return true
}
func (remoteBackend) Delete(a *App, _ *Panel, _ bool) bool {
	a.remoteDelete()
	return true
}
func (remoteBackend) Move(a *App, _ *Panel) bool {
	a.remoteMoveDialog()
	return true
}
func (remoteBackend) MenuSection(*App, *Panel) *menuSection { return nil }

func itoa64(n uint64) string { return strconv.FormatUint(n, 10) }

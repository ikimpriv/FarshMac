package ui

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"farshmac/internal/fsops"
	"farshmac/internal/kube"
	"farshmac/internal/remotefs"
	"golang.org/x/crypto/ssh"
)

// remoteLinkResolveLimit bounds the extra round trips per remote listing.
var remoteLinkResolveLimit = 500

// enterRemoteLink resolves a remote symbolic link that the listing did not
// follow: a folder target is entered, anything else is opened as a file.
func (a *App) enterRemoteLink() {
	index := a.Active
	p := &a.Panels[index]
	endpoint, base, target := p.endpoint(), p.Path, p.Current()
	a.backgroundTask("Checking remote link", func(o *fsops.Operation) (func(), func(), error) {
		f, err := endpoint.Open(o.Context())
		if err != nil {
			return nil, nil, err
		}
		defer f.Close()
		dir := false
		if stater, ok := f.(remotefs.Stater); ok {
			info, err := stater.Stat(target)
			dir = err == nil && info.IsDir()
		} else {
			_, err = f.ReadDir(target)
			dir = err == nil
		}
		if err := o.Check(); err != nil {
			return nil, nil, err
		}
		return func() {
			p := &a.Panels[index]
			if !p.served() || p.Path != base || a.Active != index || p.Current() != target {
				return
			}
			if dir {
				a.error(p.Load(target))
			} else {
				a.openRemoteDocument(false)
			}
		}, nil, nil
	})
}

func (p *Panel) loadRemote(path string) error {
	o := p.readOperation
	if o == nil {
		o = fsops.NewOperation("Reading remote folder")
		defer o.Cancel()
	}
	f, err := p.Remote.Dial(o.Context())
	if err != nil {
		return err
	}
	defer f.Close()
	files, size, err := p.listFS(f, path, o)
	if err != nil {
		return err
	}
	state := p.beginLoad(path)
	p.sortEntries(files)
	p.installEntries(files, state)
	p.TotalBytes = size
	p.ItemCount = len(files)
	p.InfoText = p.Remote.Label() + "\nRemote filesystem; terminal remains local"
	return nil
}

// listFS reads one folder of a server or container file system into panel
// entries, following a bounded number of links so links to folders open as
// folders (the rest resolve on Enter). It returns the total size of files.
func (p *Panel) listFS(f remotefs.FS, path string, o *fsops.Operation) ([]Entry, int64, error) {
	entries, err := f.ReadDir(path)
	if err != nil {
		return nil, 0, err
	}
	if len(entries) > 100000 {
		return nil, 0, fmt.Errorf("remote folder exceeds 100000 entries")
	}
	var files []Entry
	var size int64
	stater, _ := f.(remotefs.Stater)
	links := 0
	for _, info := range entries {
		if err := o.Check(); err != nil {
			return nil, 0, err
		}
		name := info.Name()
		if name == "." || name == ".." {
			continue
		}
		if !remotefs.ValidName(name) {
			return nil, 0, fmt.Errorf("server returned an unsafe filename")
		}
		if p.IgnoreDSStore && name == ".DS_Store" || p.HideHidden && strings.HasPrefix(name, ".") {
			continue
		}
		isDir := info.IsDir()
		if info.Mode()&os.ModeSymlink != 0 && stater != nil && links < remoteLinkResolveLimit {
			links++
			if target, err := stater.Stat(filepath.Join(path, name)); err == nil {
				isDir = target.IsDir()
			} else if err := o.Check(); err != nil {
				return nil, 0, err
			}
		}
		if !isDir && !matchesMask(name, p.activeFilter(), p.MaskGroups) {
			continue
		}
		if !isDir {
			size += info.Size()
		}
		files = append(files, Entry{Name: name, Info: info, Dir: isDir})
	}
	return files, size, nil
}

func (a *App) networkMenu() {
	a.loadConnectionProfiles()
	a.loadKubeProfiles()
	choices := []string{"New FTP / SFTP connection…", "New Kubernetes cluster…"}
	profiles := append([]remotefs.Config(nil), a.Settings.RemoteConnections...)
	for _, c := range profiles {
		choices = append(choices, c.Label()+c.Directory)
	}
	clusters := append([]kube.Profile(nil), a.Settings.KubeProfiles...)
	for _, c := range clusters {
		label := c.Label()
		if c.Name != "" {
			label = c.Name + " (" + label + ")"
		}
		choices = append(choices, label+"  "+c.Kubeconfig)
	}
	choices = append(choices, "Disconnect active panel")
	first := 2
	a.choose("Network connections", choices, func(i int) {
		switch {
		case i == len(choices)-1:
			a.disconnectRemote(a.Active)
		case i == 0:
			a.connectionDialog(remotefs.Config{Protocol: "sftp", Port: 22, Directory: "/", User: os.Getenv("USER")})
		case i == 1:
			a.kubeConnectionForm(a.Active, kube.Profile{})
		case i < first+len(profiles):
			a.connectionDialog(profiles[i-first])
		default:
			a.connectKube(a.Active, clusters[i-first-len(profiles)])
		}
	})
	menu := a.Form
	menu.OnKey = func(m Message) bool {
		i := menu.choiceIndex(menu.Focus)
		if m.Key != "F8" && m.Key != "Delete" || i < first || i >= first+len(profiles)+len(clusters) {
			return false
		}
		if i >= first+len(profiles) {
			cluster := clusters[i-first-len(profiles)]
			a.confirm("Delete cluster profile", "Remove "+cluster.Label()+" ("+cluster.Kubeconfig+") from kubernetes.json?", []string{"Delete", "Cancel"}, func(i int) {
				if i != 0 {
					a.networkMenu()
					return
				}
				if err := a.deleteKubeProfile(cluster); err != nil {
					a.Status = "Profile not deleted: " + err.Error()
					return
				}
				a.networkMenu()
				a.Status = "Deleted " + cluster.Label()
			})
			return true
		}
		profile := profiles[i-first]
		a.confirm("Delete connection profile", "Remove "+profile.Label()+profile.Directory+" from connections.json?", []string{"Delete", "Cancel"}, func(i int) {
			if i != 0 {
				a.networkMenu()
				return
			}
			if err := a.deleteConnectionProfile(profile); err != nil {
				a.Status = "Profile not deleted: " + err.Error()
				return
			}
			a.networkMenu()
			a.Status = "Deleted " + profile.Label() + profile.Directory
		})
		return true
	}
}

func (a *App) connectionDialog(c remotefs.Config) {
	a.connectionForm(a.Active, c, 0)
}

// connectionForm keeps passwords only in this form's memory, so Edit after a
// failed attempt does not make the user type them again.
func (a *App) connectionForm(index int, c remotefs.Config, focus int) {
	address := "sftp://"
	if c.Host != "" {
		address = c.ServerURL()
	}
	previous := c
	a.form("FTP / SFTP connection", []Field{
		{Label: "Server URL (sftp://, ftp://, ftps://)", Value: address},
		{Label: "User", Value: c.User},
		{Label: "Password (memory only)", Value: c.Password, Secret: true},
		{Label: "SSH private key (blank = default keys + agent)", Value: c.KeyFile},
		{Label: "Key passphrase (memory only)", Value: c.KeyPassphrase, Secret: true},
	}, func(fields []Field) {
		config, err := remotefs.Parse(fields[0].Value, fields[1].Value)
		if err != nil {
			a.reopenForm(fields, 0, err.Error())
			return
		}
		config.Password = fields[2].Value
		config.KeyPassphrase = fields[4].Value
		if fields[3].Value != "" {
			config.KeyFile = expand(fields[3].Value, a.Panels[a.Active].filesystemPath())
		}
		if config.Protocol == previous.Protocol && config.Address() == previous.Address() {
			// Trust given in this session (Trust once) survives editing other fields.
			config.HostKey, config.CertFingerprint = previous.HostKey, previous.CertFingerprint
			config.PlainFTPAccepted = previous.PlainFTPAccepted
		}
		a.connectRemote(index, config)
	})
	a.Form.Primary = "Connect"
	if focus > 0 && focus < len(a.Form.Fields) {
		a.Form.Focus = focus
		a.Form.Cursor = utf8.RuneCountInString(a.Form.Fields[focus].Value)
	}
}
func (a *App) plainFTPAccepted(c remotefs.Config) bool {
	if c.PlainFTPAccepted {
		return true
	}
	for _, p := range a.Settings.RemoteConnections {
		if p.PlainFTPAccepted && p.Label() == c.Label() {
			return true
		}
	}
	return false
}
func (a *App) connectRemote(index int, c remotefs.Config) {
	if c.Protocol == "ftp" && !a.plainFTPAccepted(c) {
		var f *Form
		a.confirm("Plain FTP", "Passwords and files are sent unencrypted. Continue?", []string{"Continue", "Cancel"}, func(i int) {
			if i != 0 {
				a.Status = "Connection cancelled"
				return
			}
			// Without Remember choice the warning is accepted for this attempt only.
			c.PlainFTPAccepted = f.RememberChecked
			a.connectRemoteChecked(index, c)
		})
		f = a.Form
		f.Remember = true
		return
	}
	a.connectRemoteChecked(index, c)
}
func (a *App) connectRemoteChecked(index int, c remotefs.Config) {
	note := a.connectNote
	a.connectNote = ""
	cursor := a.connectCursor
	a.connectCursor = ""
	a.backgroundTask("Connecting to "+c.Label(), func(o *fsops.Operation) (func(), func(), error) {
		f, err := c.Dial(o.Context())
		if err == nil {
			_, err = f.ReadDir(c.Directory)
			f.Close()
		}
		if err != nil {
			if o.Check() != nil || errors.Is(err, context.Canceled) {
				return nil, nil, err
			}
			return func() { a.connectFailed(index, c, err) }, nil, nil
		}
		return func() {
			p := &a.Panels[index]
			a.configurePanel(index)
			p.cancelPanelLoad()
			p.cancelTree()
			local := p.filesystemPath()
			if local == "" {
				local, _ = os.UserHomeDir()
			}
			p.Remote = &c
			p.RemoteLocalPath = local
			p.Archive = nil
			p.Async = true
			p.View = "Full"
			p.states = nil
			p.Marked = map[string]bool{}
			p.Path = c.Directory
			p.Cursor = 0
			p.Offset = 0
			saveErr := a.saveConnectionProfile(c)
			a.error(p.Load(c.Directory))
			p.placeCursor(cursor)
			a.Status = "Connected: " + c.Label() + " (terminal is local)" + note
			if saveErr != nil {
				a.Status += "; profile not saved: " + saveErr.Error()
			}
		}, nil, nil
	})
	if a.task != nil {
		a.task.cancelStatus = "Connection cancelled"
	}
}

// connectFailed turns a failed connection into a decision the user can act on
// instead of a truncated status line. Changed or revoked host keys can only
// be acknowledged; they are never trusted from here.
func (a *App) connectFailed(index int, c remotefs.Config, err error) {
	var unknown *remotefs.UnknownHost
	var changed *remotefs.HostKeyChanged
	var cert *remotefs.UnknownCertificate
	var keyFile *remotefs.KeyFileError
	var missing *ssh.PassphraseMissingError
	switch {
	case errors.As(err, &changed):
		title, status := "SSH host key changed", "Connection refused: host key mismatch"
		if changed.Revoked {
			title, status = "SSH host key revoked", "Connection refused: host key revoked"
		}
		a.confirm(title, hostKeyWarning(changed), []string{"OK"}, func(int) { a.Status = status })
	case errors.As(err, &unknown):
		a.confirm("Unknown SSH host key", unknown.Host+" "+unknown.Type+" "+unknown.Fingerprint+". Trust for this connection?", []string{"Trust once", "Trust and save", "Cancel"}, func(i int) {
			if i > 1 {
				return
			}
			c.HostKey = unknown.Key
			if i == 1 {
				if e := remotefs.AppendKnownHost(remotefs.KnownHostsPath(), unknown.Host, unknown.Key); e != nil {
					a.connectNote = "; known_hosts not updated: " + e.Error()
				} else {
					a.connectNote = "; host key saved to known_hosts"
				}
			}
			a.connectRemote(index, c)
		})
	case errors.As(err, &cert):
		a.confirm("Unknown FTPS certificate", cert.Host+" "+cert.Fingerprint+"\n"+cert.Subject+", valid until "+cert.NotAfter.Format("2006-01-02")+".\n"+cert.Reason+"\nTrust for this connection?", []string{"Trust once", "Cancel"}, func(i int) {
			if i == 0 {
				c.CertFingerprint = cert.Fingerprint
				a.connectRemote(index, c)
			}
		})
	case errors.As(err, &keyFile) && errors.As(err, &missing):
		a.connectionForm(index, c, 4)
		a.Status = "Key " + filepath.Base(keyFile.Path) + " is passphrase protected"
	case errors.As(err, &keyFile) && errors.Is(err, x509.IncorrectPasswordError):
		a.connectionForm(index, c, 4)
		a.Status = "Wrong key passphrase"
	case errors.As(err, &keyFile) && errors.Is(err, fs.ErrNotExist):
		a.connectionForm(index, c, 3)
		a.Status = "cannot read SSH private key " + keyFile.Path + ": no such file"
	default:
		a.Status = err.Error()
		a.confirm("Connection failed", c.Label()+"\n"+err.Error(), []string{"Edit", "Retry", "Cancel"}, func(i int) {
			switch i {
			case 0:
				a.connectionForm(index, c, 0)
			case 1:
				a.connectRemote(index, c)
			}
		})
	}
}
func hostKeyWarning(e *remotefs.HostKeyChanged) string {
	var where, expected []string
	for _, w := range e.Want {
		if w.File != "" {
			where = append(where, fmt.Sprintf("%s:%d", w.File, w.Line))
		}
		expected = append(expected, w.Fingerprint+" ("+w.Type+")")
	}
	source := strings.Join(where, ", ")
	if source == "" {
		source = "the key trusted for this session"
	}
	if e.Revoked {
		return "WARNING: the host key of " + e.Host + " is revoked in " + source + ".\nReceived: " + e.Fingerprint + " (" + e.Type + ")\nThis key must not be trusted. The connection was refused."
	}
	return "WARNING: the host key of " + e.Host + " does not match " + source + ".\nReceived: " + e.Fingerprint + " (" + e.Type + ")\nExpected: " + strings.Join(expected, ", ") + "\nThis may be a man-in-the-middle attack. The connection was refused."
}
func (a *App) disconnectRemote(index int) {
	p := &a.Panels[index]
	if p.Remote == nil && p.Kube == nil {
		return
	}
	p.cancelPanelLoad()
	p.cancelTree()
	var profileErr error
	if p.Remote != nil {
		profileErr = a.syncRemoteProfiles()
	}
	home, _ := os.UserHomeDir()
	local := accessibleFolder(p.RemoteLocalPath, home)
	previous := *p
	if p.Kube != nil {
		p.Kube.close()
	}
	p.Remote, p.Kube = nil, nil
	p.RemoteLocalPath = ""
	p.states = nil
	if err := p.loadSource(local, previous, "Cannot disconnect: "); err != nil {
		a.error(err)
		return
	}
	a.Status = "Disconnected"
	if profileErr != nil {
		a.Status += "; profile not saved: " + profileErr.Error()
	}
}
func (a *App) remoteMkdir() {
	index := a.Active
	p := &a.Panels[index]
	endpoint := p.endpoint()
	base := p.Path
	a.prompt("Make remote folder", "Folder name", "", func(name string) {
		if !remotefs.ValidName(name) {
			a.error(fmt.Errorf("enter one folder name"))
			return
		}
		target := filepath.Join(base, name)
		a.backgroundTask("Creating remote folder", func(o *fsops.Operation) (func(), func(), error) {
			f, err := endpoint.Open(o.Context())
			if err != nil {
				return nil, nil, err
			}
			defer f.Close()
			a.operationQuestions(o)
			err = o.Attempt("create folder", target, "", func() error { return f.Mkdir(target) })
			if errors.Is(err, fsops.ErrSkipped) {
				return func() { a.Status = "Folder creation skipped" }, nil, nil
			}
			if err != nil {
				err = fmt.Errorf("%w (create folder %s)", err, target)
			}
			return func() { a.refresh(); a.Panels[index].pendingPosition(name, "") }, nil, err
		})
		if a.task != nil {
			a.task.reportError = true
		}
	})
}
func (a *App) remoteDelete() {
	p := &a.Panels[a.Active]
	paths := p.Selected()
	if len(paths) == 0 {
		return
	}
	endpoint, label := p.endpoint(), p.label()
	// Remote protocols have no portable Trash. Always make this distinction explicit.
	a.confirm("Delete remote files permanently", fmt.Sprintf("Delete %d selected item(s) on %s? No remote Trash is available.", len(paths), label), []string{"Delete", "Cancel"}, func(i int) {
		if i != 0 {
			return
		}
		a.workProgress("Deleting remote files", func(o *fsops.Operation) error {
			o.Streaming() // Items are counted while the trees are deleted.
			f, err := endpoint.Open(o.Context())
			if err != nil {
				return err
			}
			f = remotefs.NewSession(endpoint, o.Context(), o, f)
			defer f.Close()
			for _, p := range paths {
				if err = remotefs.RemoveTree(f, p, o); err != nil {
					return err
				}
			}
			return nil
		})
	})
}
func (a *App) remoteUnsupported() bool {
	if a.Panels[a.Active].Remote == nil {
		return false
	}
	a.Status = "This command requires a local panel. Use F5 to copy remote files first."
	return true
}

func (a *App) remoteMoveDialog() {
	source, target := a.Panels[a.Active], a.Panels[1-a.Active]
	endpoint := source.endpoint()
	if endpoint.Opener == nil || !endpoint.Same(target.endpoint()) {
		a.Status = "Remote move is available within the same server. Between servers use F5 Copy, then Delete."
		return
	}
	paths := source.Selected()
	if len(paths) == 0 {
		return
	}
	dest := target.Path
	if len(paths) == 1 {
		dest = filepath.Join(dest, filepath.Base(paths[0]))
	}
	a.prompt("Remote move / rename", "Destination on the same server", dest, func(value string) {
		if remoteTilde(value) {
			a.Status = remoteTildeStatus
			return
		}
		dst := expand(value, target.Path)
		ask := a.Settings.ConfirmOverwrite
		a.workProgress("Renaming remote files", func(o *fsops.Operation) error {
			o.Streaming()
			a.collisionQuestions(o, ask)
			a.moveMetadataQuestions(o)
			f, err := endpoint.Open(o.Context())
			if err != nil {
				return err
			}
			f = remotefs.NewSession(endpoint, o.Context(), o, f)
			defer f.Close()
			info, err := f.Lstat(dst)
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			if os.IsNotExist(err) && strings.HasSuffix(value, "/") {
				// A trailing slash names a folder: create it and move inside.
				if err = o.AttemptRequired("create folder", dst, "", func() error { return f.Mkdir(dst) }); err != nil {
					return err
				}
				info, err = f.Lstat(dst)
				if err != nil {
					return err
				}
			}
			isDir := err == nil && remotefs.FollowDir(f, dst, info).IsDir() && !(len(paths) == 1 && caseRename(paths[0], dst))
			if err == nil && !isDir && strings.HasSuffix(value, "/") {
				return fmt.Errorf("destination is not a directory: %s", dst)
			}
			if len(paths) > 1 && !isDir {
				return fmt.Errorf("destination must be an existing directory")
			}
			for _, src := range paths {
				if err = o.Check(); err != nil {
					return err
				}
				to := dst
				if transferIntoDirectory(value, dst, dest, len(paths), isDir) {
					to = filepath.Join(dst, filepath.Base(src))
				}
				if err = remotefs.Move(f, endpoint, src, to, o); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

// noteInvalidRemoteNames explains garbled names from servers that use a legacy
// encoding: they are listed, but transfers refuse them.
func (a *App) noteInvalidRemoteNames(i int) {
	p := &a.Panels[i]
	if p.Remote == nil {
		return
	}
	for _, e := range p.Entries {
		if !utf8.ValidString(e.Name) {
			a.Status = "Some names are not valid UTF-8; copying them is refused"
			return
		}
	}
}

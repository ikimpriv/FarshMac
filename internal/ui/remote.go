package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"farshmac/internal/fsops"
	"farshmac/internal/remotefs"
)

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
	entries, err := f.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) > 100000 {
		return fmt.Errorf("remote folder exceeds 100000 entries")
	}
	var files []Entry
	for _, info := range entries {
		if err := o.Check(); err != nil {
			return err
		}
		name := info.Name()
		if name == "." || name == ".." {
			continue
		}
		if !remotefs.ValidName(name) {
			return fmt.Errorf("server returned an unsafe filename")
		}
		if p.IgnoreDSStore && name == ".DS_Store" || p.HideHidden && strings.HasPrefix(name, ".") {
			continue
		}
		if !info.IsDir() && !matchesMask(name, p.activeFilter(), p.MaskGroups) {
			continue
		}
		files = append(files, Entry{Name: name, Info: info, Dir: info.IsDir()})
	}
	state := p.beginLoad(path)
	p.sortEntries(files)
	p.installEntries(files, state)
	p.TotalBytes = 0
	p.ItemCount = len(files)
	for _, e := range files {
		if !e.Dir {
			p.TotalBytes += e.Info.Size()
		}
	}
	p.InfoText = p.Remote.Label() + "\nRemote filesystem; terminal remains local"
	return nil
}

func (a *App) networkMenu() {
	a.loadConnectionProfiles()
	choices := []string{"New FTP / SFTP connection…"}
	profiles := append([]remotefs.Config(nil), a.Settings.RemoteConnections...)
	for _, c := range profiles {
		choices = append(choices, c.Label()+c.Directory)
	}
	choices = append(choices, "Disconnect active panel")
	a.choose("Network connections", choices, func(i int) {
		if i == len(choices)-1 {
			a.disconnectRemote(a.Active)
			return
		}
		c := remotefs.Config{Protocol: "sftp", Port: 22, Directory: "/", User: os.Getenv("USER")}
		if i > 0 {
			c = profiles[i-1]
		}
		a.connectionDialog(c)
	})
}
func (a *App) connectionDialog(c remotefs.Config) {
	address := "sftp://"
	if c.Host != "" {
		address = c.ServerURL()
	}
	a.form("FTP / SFTP connection", []Field{
		{Label: "Server URL (sftp://, ftp://, ftps://)", Value: address},
		{Label: "User", Value: c.User},
		{Label: "Password (memory only)", Secret: true},
		{Label: "SSH private key (blank = default keys + agent)", Value: c.KeyFile},
		{Label: "Key passphrase (memory only)", Secret: true},
	}, func(fields []Field) {
		config, err := remotefs.Parse(fields[0].Value, fields[1].Value)
		if err != nil {
			a.error(err)
			return
		}
		config.Password = fields[2].Value
		config.KeyPassphrase = fields[4].Value
		if fields[3].Value != "" {
			config.KeyFile = expand(fields[3].Value, a.Panels[a.Active].filesystemPath())
		}
		a.connectRemote(a.Active, config)
	})
	a.Form.Primary = "Connect"
}
func (a *App) connectRemote(index int, c remotefs.Config) {
	a.backgroundTask("Connecting to "+c.Label(), func(o *fsops.Operation) (func(), func(), error) {
		f, err := c.Dial(o.Context())
		if err == nil {
			_, err = f.ReadDir(c.Directory)
			f.Close()
		}
		var unknown *remotefs.UnknownHost
		if errors.As(err, &unknown) {
			return func() {
				a.confirm("Unknown SSH host key", unknown.Host+" "+unknown.Fingerprint+". Trust for this connection?", []string{"Trust once", "Cancel"}, func(i int) {
					if i == 0 {
						c.HostKey = unknown.Key
						a.connectRemote(index, c)
					}
				})
			}, nil, nil
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
			a.Status = "Connected: " + c.Label() + " (terminal is local)"
			if saveErr != nil {
				a.Status += "; profile not saved: " + saveErr.Error()
			}
		}, nil, err
	})
}
func (a *App) disconnectRemote(index int) {
	p := &a.Panels[index]
	if p.Remote == nil {
		return
	}
	p.cancelPanelLoad()
	p.cancelTree()
	home, _ := os.UserHomeDir()
	local := accessibleFolder(p.RemoteLocalPath, home)
	previous := *p
	p.Remote = nil
	p.RemoteLocalPath = ""
	p.states = nil
	if err := p.loadSource(local, previous, "Cannot disconnect: "); err != nil {
		a.error(err)
		return
	}
	a.Status = "Disconnected"
}
func (a *App) remoteMkdir() {
	index := a.Active
	p := &a.Panels[index]
	config := *p.Remote
	base := p.Path
	a.prompt("Make remote folder", "Folder name", "", func(name string) {
		if !remotefs.ValidName(name) {
			a.error(fmt.Errorf("enter one folder name"))
			return
		}
		target := filepath.Join(base, name)
		a.backgroundTask("Creating remote folder", func(o *fsops.Operation) (func(), func(), error) {
			f, err := config.Dial(o.Context())
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
	config := *p.Remote
	// Remote protocols have no portable Trash. Always make this distinction explicit.
	a.confirm("Delete remote files permanently", fmt.Sprintf("Delete %d selected item(s) on %s? No remote Trash is available.", len(paths), config.Label()), []string{"Delete", "Cancel"}, func(i int) {
		if i != 0 {
			return
		}
		a.workProgress("Deleting remote files", func(o *fsops.Operation) error {
			f, err := config.Dial(o.Context())
			if err != nil {
				return err
			}
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
	if source.Remote == nil || target.Remote == nil || !(remotefs.Endpoint{Remote: source.Remote}).Same(remotefs.Endpoint{Remote: target.Remote}) {
		a.Status = "Remote move is available within the same server. Between servers use F5 Copy, then Delete."
		return
	}
	paths := source.Selected()
	if len(paths) == 0 {
		return
	}
	config := *source.Remote
	dest := target.Path
	if len(paths) == 1 {
		dest = filepath.Join(dest, filepath.Base(paths[0]))
	}
	a.prompt("Remote move / rename", "Destination on the same server", dest, func(value string) {
		dst := expand(value, target.Path)
		ask := a.Settings.ConfirmOverwrite
		a.workProgress("Renaming remote files", func(o *fsops.Operation) error {
			a.collisionQuestions(o, ask)
			a.moveMetadataQuestions(o)
			f, err := config.Dial(o.Context())
			if err != nil {
				return err
			}
			defer f.Close()
			info, err := f.Lstat(dst)
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			isDir := err == nil && info.IsDir()
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
				if err = remotefs.Move(f, remotefs.Endpoint{Remote: &config}, src, to, o); err != nil {
					return err
				}
			}
			return nil
		})
	})
}

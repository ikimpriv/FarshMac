package shellsession

// Manager owns a local PTY and persistent remote sessions, with one foreground context.
// It never opens connections or creates ephemeral containers implicitly.
type Manager struct {
	local          Session
	localTransport interface{ Close() }
	remote         *Session
	sessions       map[string]*Session
	current, panel Context
	panelKnown     bool
	panels         map[string]Context
}

func (m *Manager) Local() *Session  { return &m.local }
func (m *Manager) Remote() *Session { return m.remote }
func (m *Manager) RemoteKey() string {
	if m.remote == nil {
		return ""
	}
	return m.remote.context.ID
}
func (m *Manager) SetRemote(s *Session, stream Stream) {
	if m.sessions == nil {
		m.sessions = map[string]*Session{}
	}
	if old := m.sessions[s.context.ID]; old != nil && old != s {
		old.Close()
	}
	s.Bind(stream)
	m.remote = s
	m.sessions[s.context.ID] = s
	m.PanelChanged(s.context)
	m.current = s.context
}
func (m *Manager) ClearRemote() {
	if m.remote != nil {
		m.remote.Close()
		delete(m.sessions, m.remote.context.ID)
	}
	m.remote = nil
}
func (m *Manager) AttachLocal(t interface{ Close() }) { m.DetachLocal(); m.localTransport = t }
func (m *Manager) DetachLocal() {
	if m.localTransport != nil {
		m.localTransport.Close()
		m.localTransport = nil
	}
}
func (m *Manager) Close() {
	for _, s := range m.sessions {
		s.Close()
	}
	m.sessions = nil
	m.remote = nil
	m.DetachLocal()
}
func (m *Manager) Suspend()                  { m.remote = nil }
func (m *Manager) Select(id string)          { m.remote = m.sessions[id] }
func (m *Manager) Lookup(id string) *Session { return m.sessions[id] }
func (m *Manager) Current() Context          { return m.current }

// An unchanged panel snapshot cannot overwrite a newer shell cwd. Identity
// changes never apply another session's directory to the active panel.
func (m *Manager) PanelChanged(c Context) {
	if m.panelKnown && m.panel == c {
		return
	}
	if m.panels == nil {
		m.panels = map[string]Context{}
	}
	previous, known := m.panels[c.ID]
	changed := !known || previous.Directory != c.Directory
	m.panels[c.ID] = c
	m.panelKnown = true
	m.panel = c
	m.current = c
	if m.remote != nil && m.remote.context.ID == c.ID {
		if changed {
			m.remote.RequestDirectory(c.Directory)
		} else {
			m.current = m.remote.context
		}
		if m.remote.context.Directory == c.Directory {
			m.current.ExecutionDirectory = m.remote.context.ExecutionDirectory
		}
	}
	if c.Kind == "local" && changed {
		m.local.context = c
	}
}
func (m *Manager) ShellChanged(c Context) {
	if m.current.ID == c.ID {
		m.current = c
	}
}
func (m *Manager) LocalDirectory(dir string) {
	m.local.context.Directory = dir
	m.local.context.ExecutionDirectory = dir
	m.ShellChanged(m.local.context)
}

// LocalExited preserves a command queued during startup across PTY retries.
func (m *Manager) LocalExited() bool {
	running := m.local.running
	queued := m.local.queued
	m.local.End()
	m.local.queued = queued
	return running
}

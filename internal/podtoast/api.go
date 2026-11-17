package podtoast

import (
	"os"
	"time"
)

type Entry struct {
	MountPoint    bool           `json:"mountPoint,omitempty"`
	Mount         *MountLocation `json:"mount,omitempty"`
	LinkDirectory *bool          `json:"linkDirectory,omitempty"`
	RawName       []byte         `json:"rawName,omitempty"`
	Filename      string         `json:"name"`
	Bytes         int64          `json:"size"`
	FileMode      os.FileMode    `json:"mode"`
	Modified      time.Time      `json:"modified"`
	UID           uint32         `json:"uid"`
	GID           uint32         `json:"gid"`
}

func (e Entry) Name() string {
	if e.RawName != nil {
		return string(e.RawName)
	}
	return e.Filename
}
func (e Entry) LinkIsDir() (bool, bool) {
	if e.LinkDirectory == nil {
		return false, false
	}
	return *e.LinkDirectory, true
}
func (e Entry) Size() int64        { return e.Bytes }
func (e Entry) Mode() os.FileMode  { return e.FileMode }
func (e Entry) ModTime() time.Time { return e.Modified }
func (e Entry) IsDir() bool        { return e.FileMode.IsDir() }
func (e Entry) Sys() any           { return nil }

type Operation struct {
	RawPath     []byte      `json:"rawPath,omitempty"`
	RawTo       []byte      `json:"rawTo,omitempty"`
	RawPrevious []byte      `json:"rawPrevious,omitempty"`
	Op          string      `json:"op"`
	Path        string      `json:"path"`
	To          string      `json:"to,omitempty"`
	Previous    string      `json:"previous,omitempty"`
	Mode        os.FileMode `json:"mode,omitempty"`
	Time        time.Time   `json:"time,omitempty"`
	Directory   bool        `json:"directory,omitempty"`
}
type Result struct {
	RawText []byte `json:"rawText,omitempty"`
	Entry   *Entry `json:"entry,omitempty"`
	Text    string `json:"text,omitempty"`
}
type Capability struct {
	DirectorySummary   bool   `json:"directorySummary,omitempty"`
	TrafficOverview    bool   `json:"trafficOverview,omitempty"`
	PortTraffic        bool   `json:"portTraffic,omitempty"`
	InteractiveTraffic bool   `json:"interactiveTraffic,omitempty"`
	Traffic            bool   `json:"traffic,omitempty"`
	Version            string `json:"version"`
	TargetPID          int    `json:"targetPID"`
	Files              bool   `json:"files"`
	NetworkScope       string `json:"networkScope"`
}
type Section struct {
	Value       any    `json:"value,omitempty"`
	Unavailable string `json:"unavailable,omitempty"`
}
type Snapshot struct {
	At        time.Time `json:"at"`
	TargetPID int       `json:"targetPID"`
	Processes Section   `json:"processes"`
	Resources Section   `json:"resources"`
	Storage   Section   `json:"storage"`
	Network   Section   `json:"network"`
}
type Probe struct {
	Kind        string `json:"kind"` // dns, tcp, http, load
	Target      string `json:"target"`
	Requests    int    `json:"requests,omitempty"`
	Concurrency int    `json:"concurrency,omitempty"`
	Rate        int    `json:"rate,omitempty"` // global requests/second; no unlimited mode
}

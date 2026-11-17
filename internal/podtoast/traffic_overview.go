package podtoast

// Socket inventory is passive and separate from connections owned by the fault
// proxy. It does not imply packet counters or control of an existing socket.
type TrafficSocket struct {
	Protocol string `json:"protocol"`
	Local    string `json:"local"`
	Remote   string `json:"remote"`
	State    string `json:"state"`
	Inode    string `json:"inode"`
}
type TrafficOverview struct {
	Sockets       []TrafficSocket `json:"sockets"`
	Truncated     bool            `json:"truncated"`
	Sent          uint64          `json:"sent"`
	Received      uint64          `json:"received"`
	CountersValid bool            `json:"countersValid"`
	Warnings      []string        `json:"warnings,omitempty"`
}

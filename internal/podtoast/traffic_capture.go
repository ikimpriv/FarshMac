package podtoast

import (
	"container/list"
	"fmt"
	"net/netip"
	"sort"
	"sync"
	"time"
)

const maxTrafficFlows = 8192
const maxTrafficGroups = 2048

type TrafficFlow struct {
	ID        uint64 `json:"id"`
	Protocol  string `json:"protocol"`
	Direction string `json:"direction"`
	Local     string `json:"local"`
	Remote    string `json:"remote"`
	Port      uint16 `json:"port"`
	Sent      uint64 `json:"sent"`
	Received  uint64 `json:"received"`
	Packets   uint64 `json:"packets"`
	State     string `json:"state"`
	First     int64  `json:"first"`
	Last      int64  `json:"last"`
}
type TrafficGroup struct {
	ID          string `json:"id"`
	Protocol    string `json:"protocol"`
	Direction   string `json:"direction"`
	Port        uint16 `json:"port"`
	Sent        uint64 `json:"sent"`
	Received    uint64 `json:"received"`
	Connections uint64 `json:"connections"`
}
type TrafficCapture struct {
	At         int64          `json:"at"`
	Since      int64          `json:"since"`
	Flows      []TrafficFlow  `json:"flows"`
	Groups     []TrafficGroup `json:"groups"`
	Dropped    uint64         `json:"dropped"`
	Unparsed   uint64         `json:"unparsed"`
	Evicted    uint64         `json:"evicted"`
	GroupLimit bool           `json:"groupLimit"`
	Error      string         `json:"error,omitempty"`
}
type trafficTuple struct {
	local, remote netip.AddrPort
	proto         string
}
type trackedFlow struct {
	TrafficFlow
	key     trafficTuple
	node    *list.Element
	synSeq  uint32
	seenSyn bool
}
type trafficCounters struct {
	mu                               sync.Mutex
	since                            time.Time
	flows                            map[trafficTuple]*trackedFlow
	history                          map[uint64]*trackedFlow
	groups                           map[string]*TrafficGroup
	listeners                        map[string]bool
	lru                              *list.List
	next, dropped, unparsed, evicted uint64
	groupLimit                       bool
	err                              string
}

func newTrafficCounters() *trafficCounters {
	return &trafficCounters{since: time.Now(), flows: map[trafficTuple]*trackedFlow{}, history: map[uint64]*trackedFlow{}, groups: map[string]*TrafficGroup{}, listeners: map[string]bool{}, lru: list.New()}
}
func listenerKey(proto string, port uint16) string { return fmt.Sprintf("%s/%d", proto, port) }
func (c *trafficCounters) create(key trafficTuple, direction string, now time.Time) *trackedFlow {
	if len(c.history) >= maxTrafficFlows {
		old := c.lru.Front().Value.(*trackedFlow)
		c.lru.Remove(old.node)
		delete(c.history, old.ID)
		if c.flows[old.key] == old {
			delete(c.flows, old.key)
		}
		c.evicted++
	}
	c.next++
	port := key.remote.Port()
	if direction == "inbound" {
		port = key.local.Port()
	}
	f := &trackedFlow{key: key, TrafficFlow: TrafficFlow{ID: c.next, Protocol: key.proto, Direction: direction, Local: key.local.String(), Remote: key.remote.String(), Port: port, First: now.UnixMilli(), Last: now.UnixMilli(), State: "Observed"}}
	f.node = c.lru.PushBack(f)
	c.flows[key] = f
	c.history[f.ID] = f
	if g := c.group(f); g != nil {
		g.Connections++
	}
	return f
}
func (c *trafficCounters) group(f *trackedFlow) *TrafficGroup {
	id := f.Direction + "/" + listenerKey(f.Protocol, f.Port)
	g := c.groups[id]
	if g == nil {
		if len(c.groups) >= maxTrafficGroups {
			c.groupLimit = true
			return nil
		}
		g = &TrafficGroup{ID: id, Protocol: f.Protocol, Direction: f.Direction, Port: f.Port}
		c.groups[id] = g
	}
	return g
}
func (c *trafficCounters) inventory(sockets []TrafficSocket) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.listeners = map[string]bool{}
	for _, s := range sockets {
		if s.State == "LISTEN" {
			if a, e := netip.ParseAddrPort(s.Local); e == nil {
				c.listeners[listenerKey(s.Protocol, a.Port())] = true
			}
		}
	}
	now := time.Now()
	for _, s := range sockets {
		if s.State == "LISTEN" || s.State == "UNCONNECTED" {
			continue
		}
		local, e := netip.ParseAddrPort(s.Local)
		if e != nil {
			continue
		}
		remote, e := netip.ParseAddrPort(s.Remote)
		if e != nil || remote.Port() == 0 {
			continue
		}
		key := trafficTuple{local, remote, s.Protocol}
		f := c.flows[key]
		if f == nil && (local.Addr() == remote.Addr() || local.Addr().IsLoopback() && remote.Addr().IsLoopback()) {
			if reverse := c.flows[trafficTuple{remote, local, s.Protocol}]; reverse != nil {
				continue
			}
		}
		if f == nil {
			direction := "outbound"
			if c.listeners[listenerKey(s.Protocol, local.Port())] {
				direction = "inbound"
			}
			f = c.create(key, direction, now)
		}
		f.State = s.State
	}
}
func (c *trafficCounters) packet(p decodedTrafficPacket, outgoing, loopback bool, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	local, remote := p.dst, p.src
	if outgoing {
		local, remote = p.src, p.dst
	}
	key := trafficTuple{local, remote, p.proto}
	f := c.flows[key]
	if loopback && f == nil {
		// Loopback is delivered twice by AF_PACKET. The reader keeps the outgoing
		// copy; represent a local conversation once, from the server endpoint.
		reverse := trafficTuple{remote, local, p.proto}
		if other := c.flows[reverse]; other != nil {
			f = other
			key = reverse
			outgoing = false
		} else if p.syn && !p.ack || c.listeners[listenerKey(p.proto, remote.Port())] {
			key = reverse
			local, remote = remote, local
			outgoing = false
			f = c.flows[key]
		}
	}
	if f == nil || p.syn && !p.ack && ((f.seenSyn && f.synSeq != p.seq) || f.State == "Closed") || p.proto[:3] == "udp" && now.UnixMilli()-f.Last > 60000 {
		direction := "outbound"
		if c.listeners[listenerKey(p.proto, local.Port())] || p.syn && !p.ack && !outgoing || !outgoing && p.proto[:3] == "udp" {
			direction = "inbound"
		}
		f = c.create(key, direction, now)
	}
	if p.syn && !p.ack {
		f.seenSyn = true
		f.synSeq = p.seq
	}
	f.Last = now.UnixMilli()
	f.Packets++
	c.lru.MoveToBack(f.node)
	if p.rst || p.fin {
		f.State = "Closed"
	} else if f.State == "Observed" {
		f.State = "Active"
	}
	g := c.group(f)
	if outgoing {
		f.Sent += p.bytes
		if g != nil {
			g.Sent += p.bytes
		}
	} else {
		f.Received += p.bytes
		if g != nil {
			g.Received += p.bytes
		}
	}
}
func (c *trafficCounters) snapshot() *TrafficCapture {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := &TrafficCapture{At: time.Now().UnixMilli(), Since: c.since.UnixMilli(), Dropped: c.dropped, Unparsed: c.unparsed, Evicted: c.evicted, GroupLimit: c.groupLimit, Error: c.err, Flows: []TrafficFlow{}, Groups: []TrafficGroup{}}
	for _, f := range c.history {
		out.Flows = append(out.Flows, f.TrafficFlow)
	}
	for _, g := range c.groups {
		out.Groups = append(out.Groups, *g)
	}
	sort.Slice(out.Flows, func(i, j int) bool { return out.Flows[i].ID < out.Flows[j].ID })
	sort.Slice(out.Groups, func(i, j int) bool { return out.Groups[i].ID < out.Groups[j].ID })
	return out
}

type decodedTrafficPacket struct {
	src, dst           netip.AddrPort
	proto              string
	bytes              uint64
	syn, ack, fin, rst bool
	seq                uint32
}

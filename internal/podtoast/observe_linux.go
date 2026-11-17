//go:build linux

package podtoast

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func smallFile(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 64<<10))
	return string(b), e
}
func section(value any, err error) Section {
	if err != nil {
		return Section{Unavailable: err.Error()}
	}
	return Section{Value: value}
}

type Process struct {
	PID        int     `json:"pid"`
	Parent     int     `json:"parent"`
	Name       string  `json:"name"`
	State      string  `json:"state"`
	CPUSeconds float64 `json:"cpuSeconds"`
	RSSBytes   int64   `json:"rssBytes"`
	Threads    int     `json:"threads"`
	StartTicks int64   `json:"startTicks"`
}

func parseProcess(pid int, line string) (Process, error) {
	l, r := strings.Index(line, "("), strings.LastIndex(line, ")")
	if l < 0 || r < l {
		return Process{}, fmt.Errorf("malformed process stat")
	}
	f := strings.Fields(line[r+1:])
	if len(f) < 22 {
		return Process{}, fmt.Errorf("short process stat")
	}
	number := func(i int) int64 { n, _ := strconv.ParseInt(f[i], 10, 64); return n }
	return Process{PID: pid, Parent: int(number(1)), Name: line[l+1 : r], State: f[0], CPUSeconds: float64(number(11)+number(12)) / 100, RSSBytes: number(21) * int64(os.Getpagesize()), Threads: int(number(17)), StartTicks: number(19)}, nil
}
func (s *Server) snapshot(ctx context.Context) Snapshot {
	out := Snapshot{At: time.Now(), TargetPID: s.TargetPID}
	proc := s.path("/proc")
	if !s.LightMetrics {
		dir, err := os.Open(proc)
		if err != nil {
			out.Processes = section(nil, err)
		} else {
			defer dir.Close()
			processes := []Process{}
			truncated := false
			for {
				names, e := dir.Readdirnames(256)
				for _, name := range names {
					if ctx.Err() != nil {
						return out
					}
					pid, e := strconv.Atoi(name)
					if e != nil {
						continue
					}
					text, e := smallFile(filepath.Join(proc, name, "stat"))
					if e != nil {
						continue
					}
					p, e := parseProcess(pid, text)
					if e == nil {
						processes = append(processes, p)
					}
					if len(processes) >= 16384 {
						truncated = true
						break
					}
				}
				if e != nil || truncated {
					break
				}
			}
			out.Processes = section(map[string]any{"scope": "visible PID namespace (may include other containers)", "items": processes, "truncated": truncated}, nil)
		}
	}
	// Never report the agent's cgroup as the application's. A cgroup mount
	// obtained through the target root is accepted only with verified membership.
	cg := s.path("/sys/fs/cgroup")
	members, e := smallFile(filepath.Join(cg, "cgroup.procs"))
	member := false
	for _, p := range strings.Fields(members) {
		if p == strconv.Itoa(s.TargetPID) {
			member = true
		}
	}
	if e != nil || !member {
		out.Resources = Section{Unavailable: "target cgroup v2 is not visible or its membership cannot be verified"}
	} else {
		metrics := map[string]any{"scope": "verified target cgroup v2"}
		for _, name := range []string{"cpu.stat", "cpu.max", "memory.current", "memory.max", "memory.peak", "memory.stat", "memory.events", "memory.swap.current", "pids.current", "pids.max", "io.stat", "cpu.pressure", "memory.pressure", "io.pressure"} {
			text, e := smallFile(filepath.Join(cg, name))
			metrics[name] = section(strings.TrimSpace(text), e)
		}
		usage := map[string]any{}
		if sec, ok := metrics["cpu.stat"].(Section); ok {
			if text, ok := sec.Value.(string); ok {
				fields := strings.Fields(text)
				for i := 0; i+1 < len(fields); i += 2 {
					v, e := strconv.ParseUint(fields[i+1], 10, 64)
					if e == nil {
						usage[fields[i]] = v
					}
				}
			}
		}
		for _, name := range []string{"memory.current", "memory.max", "memory.peak", "pids.current"} {
			if sec, ok := metrics[name].(Section); ok {
				if text, ok := sec.Value.(string); ok {
					if v, e := strconv.ParseUint(strings.TrimSpace(text), 10, 64); e == nil {
						usage[name] = v
					}
				}
			}
		}
		metrics["usage"] = usage
		out.Resources = section(metrics, nil)
	}
	var st syscall.Statfs_t
	e = syscall.Statfs(s.path("/"), &st)
	storage := map[string]any{"scope": "filesystem containing /; capacity may be shared with other containers", "capacityBytes": st.Blocks * uint64(st.Bsize), "freeBytes": st.Bavail * uint64(st.Bsize), "usedBytes": (st.Blocks - st.Bfree) * uint64(st.Bsize), "inodes": st.Files, "freeInodes": st.Ffree}
	if !s.LightMetrics {
		storage["mounts"] = s.mounts(ctx, proc)
	}
	out.Storage = section(storage, e)
	network := map[string]any{"scope": "pod network namespace"}
	names := []string{"dev", "route", "ipv6_route", "snmp", "netstat", "unix"}
	if s.LightMetrics {
		names = []string{"dev"}
	}
	for _, name := range names {
		text, e := smallFile(filepath.Join(proc, "net", name))
		network[name] = section(strings.TrimSpace(text), e)
	}
	if !s.LightMetrics {
		for _, name := range []string{"tcp", "tcp6", "udp", "udp6"} {
			items, e := socketTable(filepath.Join(proc, "net", name))
			network[name] = section(items, e)
		}
		text, e := smallFile(s.path("/etc/resolv.conf"))
		network["resolver"] = section(text, e)
	}
	out.Network = section(network, nil)
	return out
}

type Socket struct {
	Local  string `json:"local"`
	Remote string `json:"remote"`
	State  string `json:"state"`
	UID    string `json:"uid"`
	Inode  string `json:"inode"`
}

func socketAddress(value string) string {
	host, port, _ := strings.Cut(value, ":")
	b, e := hex.DecodeString(host)
	if e != nil {
		return value
	}
	if len(b) != 4 && len(b) != 16 {
		return value
	}
	for i := 0; i < len(b); i += 4 {
		v := binary.LittleEndian.Uint32(b[i : i+4])
		binary.BigEndian.PutUint32(b[i:i+4], v)
	}
	p, _ := strconv.ParseUint(port, 16, 16)
	return net.JoinHostPort(net.IP(b).String(), strconv.Itoa(int(p)))
}
func socketTable(path string) (any, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	items := []Socket{}
	scan.Scan()
	truncated := false
	states := map[string]string{"01": "ESTABLISHED", "02": "SYN_SENT", "03": "SYN_RECV", "04": "FIN_WAIT1", "05": "FIN_WAIT2", "06": "TIME_WAIT", "07": "CLOSE", "08": "CLOSE_WAIT", "09": "LAST_ACK", "0A": "LISTEN", "0B": "CLOSING"}
	for scan.Scan() {
		fields := strings.Fields(scan.Text())
		if len(fields) < 10 {
			continue
		}
		state := states[fields[3]]
		if state == "" {
			state = fields[3]
		}
		items = append(items, Socket{socketAddress(fields[1]), socketAddress(fields[2]), state, fields[7], fields[9]})
		if len(items) >= 4096 {
			truncated = true
			break
		}
	}
	return map[string]any{"items": items, "truncated": truncated}, scan.Err()
}

// Mount capacities are filesystem-wide, not exclusive container consumption.
// Pseudo-filesystems and file bind mounts do not represent application storage.
func (s *Server) mounts(ctx context.Context, proc string) Section {
	text, err := smallFile(filepath.Join(proc, strconv.Itoa(s.TargetPID), "mountinfo"))
	if err != nil {
		return section(nil, err)
	}
	mounts := []map[string]any{}
	for _, line := range strings.Split(text, "\n") {
		if ctx.Err() != nil {
			break
		}
		left, right, ok := strings.Cut(line, " - ")
		if !ok {
			continue
		}
		a, b := strings.Fields(left), strings.Fields(right)
		if len(a) < 6 || len(b) < 2 {
			continue
		}
		switch b[0] {
		case "proc", "sysfs", "devpts", "cgroup", "cgroup2", "mqueue":
			continue
		}
		mount := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(a[4])
		p := s.path(mount)
		info, e := os.Stat(p)
		if e != nil || !info.IsDir() {
			continue
		}
		var st syscall.Statfs_t
		e = syscall.Statfs(p, &st)
		m := map[string]any{"path": mount, "filesystem": b[0], "options": a[5]}
		if e != nil {
			m["unavailable"] = e.Error()
		} else {
			m["capacityBytes"] = st.Blocks * uint64(st.Bsize)
			m["availableBytes"] = st.Bavail * uint64(st.Bsize)
			m["usedBytes"] = (st.Blocks - st.Bfree) * uint64(st.Bsize)
			m["freeInodes"] = st.Ffree
		}
		mounts = append(mounts, m)
		if len(mounts) == 64 {
			break
		}
	}
	return section(mounts, nil)
}

func processMetricTotals(section Section) (count, rss, cpu float64, ok bool) {
	data, ok := section.Value.(map[string]any)
	if !ok {
		return 0, 0, 0, false
	}
	items, ok := data["items"].([]Process)
	if !ok {
		return 0, 0, 0, false
	}
	for _, p := range items {
		rss += float64(p.RSSBytes)
		cpu += p.CPUSeconds
	}
	return float64(len(items)), rss, cpu, true
}

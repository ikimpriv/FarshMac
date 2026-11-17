//go:build linux

package podtoast

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const historyDirectory = "/tmp/podtoast-metrics-history"

func historyIdentity(pid int) (string, error) {
	text, err := smallFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	i := strings.LastIndex(text, ") ")
	if i < 0 {
		return "", fmt.Errorf("invalid process identity")
	}
	fields := strings.Fields(text[i+2:])
	if len(fields) < 20 {
		return "", fmt.Errorf("invalid process identity")
	}
	return fmt.Sprintf("%d-%s.json", pid, fields[19]), nil
}

// Open before chroot. Only this agent's private history directory is retained;
// the HTTP endpoint cannot supply a path or access any other outside-root file.
func OpenMetricsHistory(pid int) (func() (MetricsHistory, error), func()) {
	name, err := historyIdentity(pid)
	if err != nil {
		return nil, func() {}
	}
	dir, err := os.Open(historyDirectory)
	if err != nil {
		return nil, func() {}
	}
	return func() (MetricsHistory, error) {
		fd, err := syscall.Openat(int(dir.Fd()), name, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if os.IsNotExist(err) {
			return MetricsHistory{}, nil
		}
		if err != nil {
			return MetricsHistory{}, err
		}
		f := os.NewFile(uintptr(fd), name)
		defer f.Close()
		var h MetricsHistory
		err = json.NewDecoder(io.LimitReader(f, 2*HistoryBytes)).Decode(&h)
		return h, err
	}, func() { dir.Close() }
}

// Collect while idle, not just while FarshMac has an exec stream open. Group by
// target root and pin PID start time; never collect the toolbox's own root.
func CollectMetricsHistory(ctx context.Context) {
	if err := os.MkdirAll(historyDirectory, 0700); err != nil {
		return
	}
	ours, err := os.Stat("/")
	if err != nil {
		return
	}
	histories := map[string]*MetricsHistory{}
	sample := func() {
		entries, err := os.ReadDir("/proc")
		if err != nil {
			return
		}
		var pids []int
		for _, e := range entries {
			if n, err := strconv.Atoi(e.Name()); err == nil {
				pids = append(pids, n)
			}
		}
		sort.Ints(pids)
		roots := map[string]bool{}
		active := map[string]bool{}
		for _, pid := range pids {
			if ctx.Err() != nil {
				return
			}
			if len(active) >= 16 {
				break
			}
			root := fmt.Sprintf("/proc/%d/root", pid)
			info, err := os.Stat(root)
			if err != nil || os.SameFile(ours, info) {
				continue
			}
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				continue
			}
			rootID := fmt.Sprintf("%d:%d", st.Dev, st.Ino)
			if roots[rootID] {
				continue
			}
			roots[rootID] = true
			key, err := historyIdentity(pid)
			if err != nil {
				continue
			}
			active[key] = true
			h := histories[key]
			if h == nil {
				h = &MetricsHistory{}
				histories[key] = h
			}
			server := Server{Root: root, TargetPID: pid, LightMetrics: true}
			request, cancel := context.WithTimeout(ctx, 2*time.Second)
			snap := server.snapshot(request)
			cancel()
			if ctx.Err() != nil {
				return
			}
			if identity, err := historyIdentity(pid); err != nil || identity != key {
				continue
			}
			var b bytes.Buffer
			writeMetrics(&b, snap)
			h.Append(time.Now(), b.String())
			data, err := json.Marshal(h)
			if err != nil {
				continue
			}
			tmp := filepath.Join(historyDirectory, key+".tmp")
			if os.WriteFile(tmp, data, 0600) == nil {
				_ = os.Rename(tmp, filepath.Join(historyDirectory, key))
			}
		}
		for key := range histories {
			if !active[key] {
				delete(histories, key)
				_ = os.Remove(filepath.Join(historyDirectory, key))
			}
		}
	}
	sample()
	ticker := time.NewTicker(HistoryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sample()
		}
	}
}

//go:build linux && (amd64 || arm64)

package podtoast

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// RetireIdle runs inside the OLD toolbox's mount namespace, before any chroot.
// The helper may be supplied by a newer toolbox to retire legacy versions.
// Refuse to signal until the idle process, root and executable are verified;
// pidfd pins the process identity across PID reuse.
func RetireIdle(ctx context.Context) error {
	root, err := os.Stat("/")
	if err != nil {
		return err
	}
	binary, err := os.Stat("/podtoast")
	if err != nil {
		return err
	}
	idle := 0
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	for _, e := range entries {
		pid, e := strconv.Atoi(e.Name())
		if e != nil || pid == os.Getpid() {
			continue
		}
		base := fmt.Sprintf("/proc/%d", pid)
		r, e := os.Stat(base + "/root")
		if e != nil || !os.SameFile(root, r) {
			continue
		}
		exe, e := os.Stat(base + "/exe")
		if e != nil || !os.SameFile(binary, exe) {
			continue
		}
		args, e := os.ReadFile(base + "/cmdline")
		if e != nil {
			continue
		}
		argv := bytes.Split(bytes.TrimRight(args, "\x00"), []byte{0})
		if len(argv) != 2 || string(argv[1]) != "idle" {
			continue
		}
		if idle != 0 {
			return fmt.Errorf("multiple idle processes; refusing ambiguous retirement")
		}
		idle = pid
	}
	if idle == 0 {
		return fmt.Errorf("no verified PodToast idle process")
	}
	fd, _, errno := syscall.Syscall(434, uintptr(idle), 0, 0) // pidfd_open, amd64/arm64
	if errno != 0 {
		return fmt.Errorf("pidfd_open: %w", errno)
	}
	defer syscall.Close(int(fd))
	base := fmt.Sprintf("/proc/%d", idle)
	verify := func() bool {
		r, e := os.Stat(base + "/root")
		if e != nil || !os.SameFile(root, r) {
			return false
		}
		exe, e := os.Stat(base + "/exe")
		return e == nil && os.SameFile(binary, exe)
	}
	if !verify() {
		return fmt.Errorf("idle process identity changed")
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("old agent still in use; left running: %w", err)
		}
		entries, err = os.ReadDir("/proc")
		if err != nil {
			return err
		}
		busy := false
		for _, entry := range entries {
			pid, e := strconv.Atoi(entry.Name())
			if e != nil || pid == idle || pid == os.Getpid() {
				continue
			}
			p := filepath.Join("/proc", entry.Name())
			// A serve process chroots into the application: its executable still
			// belongs to the old toolbox. Shells/tools retain the toolbox root.
			r, re := os.Stat(p + "/root")
			exe, ee := os.Stat(p + "/exe")
			if os.IsPermission(re) || os.IsPermission(ee) {
				busy = true
				break
			}
			if re == nil && os.SameFile(root, r) || ee == nil && os.SameFile(binary, exe) {
				stat, e := os.ReadFile(p + "/stat")
				if e == nil {
					if i := strings.LastIndex(string(stat), ") "); i >= 0 && strings.HasPrefix(string(stat)[i+2:], "Z ") {
						continue
					}
				}
				busy = true
				break
			}
		}
		if !busy {
			if !verify() {
				return fmt.Errorf("idle process identity changed")
			}
			_, _, errno = syscall.Syscall6(424, fd, uintptr(syscall.SIGTERM), 0, 0, 0, 0) // pidfd_send_signal
			if errno != 0 {
				return fmt.Errorf("pidfd_send_signal: %w", errno)
			}
			return nil
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}

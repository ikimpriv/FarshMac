//go:build linux

// PodToast is a shell-free diagnostic agent for Kubernetes ephemeral containers.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"farshmac/internal/podtoast"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "PodToast:", err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: podtoast idle | serve --target-id ID [--shared-processes] | traffic")
	}
	if os.Args[1] == "export-self" {
		f, err := os.Open("/proc/self/exe")
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(os.Stdout, f)
		return err
	}
	if os.Args[1] == "retire" {
		// A streamed helper unlinks its own temporary image before waiting.
		if len(os.Args) == 3 && os.Args[2] == "--temporary" {
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			if !strings.HasPrefix(exe, "/tmp/podtoast-retire.") || filepath.Base(exe) != "helper" {
				return fmt.Errorf("invalid temporary helper path")
			}
			if err = os.Remove(exe); err != nil {
				return err
			}
			if err = os.Remove(filepath.Dir(exe)); err != nil {
				return err
			}
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer stop()
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		return podtoast.RetireIdle(ctx)
	}
	if os.Args[1] == "traffic-watchdog" {
		if len(os.Args) != 4 {
			return fmt.Errorf("invalid watchdog arguments")
		}
		seconds, e := strconv.Atoi(os.Args[3])
		if e != nil {
			return e
		}
		return podtoast.TrafficWatchdog(os.Args[2], seconds)
	}
	if os.Args[1] == "traffic" {
		conn := &podtoast.StreamConn{Reader: os.Stdin, Writer: os.Stdout, CloseFunc: func() error { return os.Stdin.Close() }}
		server := &podtoast.TrafficServer{}
		defer server.Close()
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer stop()
		closeOnSignal := context.AfterFunc(ctx, func() { conn.Close() })
		defer closeOnSignal()
		podtoast.Serve(conn, server)
		return nil
	}

	if os.Args[1] == "idle" {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer stop()
		go podtoast.CollectMetricsHistory(ctx)
		select {
		case <-ctx.Done():
		case <-time.After(24 * time.Hour):
		}
		return nil
	}
	if os.Args[1] != "serve" {
		return fmt.Errorf("unknown command")
	}
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	id := flags.String("target-id", "", "container runtime ID")
	metricsListen := flags.String("metrics-listen", "", "optional read-only HTTP metrics address, e.g. 127.0.0.1:9108; disabled by default")
	shared := flags.Bool("shared-processes", false, "pod shares PID namespace")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if len(*id) < 12 {
		return fmt.Errorf("target container ID is required")
	}
	pid, err := targetPID(*id, *shared)
	if err != nil {
		return err
	}
	// Pin the process identity, then enter the target root before starting HTTP
	// goroutines. Chroot provides correct absolute-symlink semantics; no host
	// mounts, setns, runtime socket, or shell are required.
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	identity, err := startTime(statPath)
	if err != nil {
		return err
	}
	owner, err := processOwner(pid)
	if err != nil {
		return err
	}
	history, closeHistory := podtoast.OpenMetricsHistory(pid)
	defer closeHistory()
	root := fmt.Sprintf("/proc/%d/root", pid)
	fd, err := os.Open(root)
	if err != nil {
		return fmt.Errorf("target root (SYS_PTRACE / file permissions): %w", err)
	}
	defer fd.Close()
	if err = fd.Chdir(); err != nil {
		return err
	}
	if err = syscall.Chroot("."); err != nil {
		return fmt.Errorf("enter target root (SYS_CHROOT): %w", err)
	}
	if err = os.Chdir("/"); err != nil {
		return err
	}
	fd.Close()
	check := func() error {
		now, e := startTime(statPath)
		if e != nil || now != identity {
			return fmt.Errorf("target process exited or restarted; reconnect PodToast")
		}
		return nil
	}
	if err = check(); err != nil {
		return err
	}
	// All protocol bytes go to stdout; diagnostics only to stderr.
	conn := &podtoast.StreamConn{Reader: os.Stdin, Writer: os.Stdout, CloseFunc: func() error { return os.Stdin.Close() }}
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if check() != nil {
					conn.Close()
					return
				}
			}
		}
	}()
	server := &podtoast.Server{Root: "/", TargetPID: pid, CheckTarget: check, DefaultOwner: owner, History: history}
	if *metricsListen != "" {
		listener, err := net.Listen("tcp", *metricsListen)
		if err != nil {
			return fmt.Errorf("metrics listener: %w", err)
		}
		httpServer := &http.Server{Handler: server.MetricsHandler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8 << 10}
		defer httpServer.Close()
		go func() {
			if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
				fmt.Fprintln(os.Stderr, "PodToast metrics:", err)
			}
		}()
	}
	podtoast.Serve(conn, server)
	return nil
}
func targetPID(id string, shared bool) (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	best := 0
	for _, entry := range entries {
		pid, e := strconv.Atoi(entry.Name())
		if e != nil || pid == os.Getpid() {
			continue
		}
		data, e := os.ReadFile(filepath.Join("/proc", entry.Name(), "cgroup"))
		if e == nil && strings.Contains(string(data), id) {
			if best == 0 || pid < best {
				best = pid
			}
		}
	}
	if best != 0 {
		return best, nil
	}
	if shared {
		return 0, fmt.Errorf("cannot identify target in shared PID namespace: container ID not exposed in cgroups")
	}
	// With a private target PID namespace the application is PID 1. Refuse a
	// runtime that ignored targetContainerName and left us in our own namespace.
	ours, e1 := os.Stat("/")
	target, e2 := os.Stat("/proc/1/root")
	if e1 != nil || e2 != nil || os.SameFile(ours, target) {
		return 0, fmt.Errorf("target process namespace/root is unavailable; runtime may not support targetContainerName")
	}
	return 1, nil
}
func startTime(path string) (string, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return "", e
	}
	i := strings.LastIndex(string(b), ") ")
	if i < 0 {
		return "", fmt.Errorf("invalid process stat")
	}
	fields := strings.Fields(string(b)[i+2:])
	if len(fields) < 20 {
		return "", fmt.Errorf("short process stat")
	}
	return fields[19], nil
}

func processOwner(pid int) (*podtoast.Owner, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return nil, err
	}
	owner := &podtoast.Owner{UID: -1, GID: -1}
	for _, line := range strings.Split(string(b), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || (key != "Uid" && key != "Gid") {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) != 4 {
			return nil, fmt.Errorf("invalid target identity")
		}
		id, e := strconv.Atoi(fields[3])
		if e != nil {
			return nil, e
		}
		if key == "Uid" {
			owner.UID = id
		} else {
			owner.GID = id
		}
	}
	if owner.UID < 0 || owner.GID < 0 {
		return nil, fmt.Errorf("target filesystem UID/GID unavailable")
	}
	return owner, nil
}

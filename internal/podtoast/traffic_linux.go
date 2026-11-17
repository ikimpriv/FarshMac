//go:build linux

package podtoast

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const trafficMark = 0x504f4454

func trafficCommand(args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "iptables", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("iptables %s: %w: %.2048s", strings.Join(args, " "), err, b)
	}
	return nil
}
func trafficCleanup(rules []trafficRule) error {
	var errs []error
	for i := len(rules) - 1; i >= 0; i-- {
		err := trafficCommand(rules[i].Remove)
		if err != nil && !strings.Contains(err.Error(), "Bad rule") && !strings.Contains(err.Error(), "No chain/target/match") && !strings.Contains(err.Error(), "does not exist") {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// TrafficWatchdog holds the pod-wide abstract socket lock and a parent-liveness
// pipe. Even SIGKILL of the proxy closes that pipe and triggers exact-rule cleanup.
func TrafficWatchdog(journal string, seconds int) error {
	// A dead parent can close stdout during an acknowledgement. Never let
	// SIGPIPE terminate the rule owner before its cleanup path.
	signal.Ignore(syscall.SIGPIPE)
	interactive := seconds < 0
	if interactive {
		seconds = -seconds
	}
	if seconds < 5 || seconds > 600 {
		return fmt.Errorf("invalid watchdog deadline")
	}
	lock, life := os.NewFile(3, "lock"), os.NewFile(4, "parent")
	defer lock.Close()
	defer life.Close()
	b, err := os.ReadFile(journal)
	if err != nil {
		return err
	}
	// Refuse to stack an experiment over rules left by a failed watchdog.
	for _, table := range []string{"nat", "mangle"} {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		existing, e := exec.CommandContext(ctx, "iptables", "-w", "2", "-t", table, "-S").CombinedOutput()
		cancel()
		if e != nil {
			return fmt.Errorf("iptables preflight (NET_ADMIN required): %w: %.2048s", e, existing)
		}
		for _, line := range strings.Split(string(existing), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "-N" && strings.HasPrefix(fields[1], "PT_") && len(fields[1]) == 15 {
				return fmt.Errorf("unfinished PodToast rules %s in %s; resolve previous cleanup failure before starting", fields[1], table)
			}
		}
	}
	var rules []trafficRule
	if err = json.Unmarshal(b, &rules); err != nil {
		return err
	}
	ctxDone := make(chan struct{})
	defer close(ctxDone)
	done := make(chan struct{})
	commands := make(chan trafficGuardCommand)
	go func() {
		defer close(done)
		d := json.NewDecoder(bufio.NewReader(life))
		for {
			var c trafficGuardCommand
			if d.Decode(&c) != nil {
				return
			}
			select {
			case commands <- c:
			case <-ctxDone:
				return
			}
		}
	}()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// All mutations belong to the watchdog. Parent death during installation
	// cannot race a late iptables add against cleanup.
	for _, rule := range rules {
		select {
		case <-done:
			err = context.Canceled
		case <-ctx.Done():
			err = ctx.Err()
		default:
		}
		if err != nil {
			break
		}
		if err = trafficCommand(rule.Add); err != nil {
			break
		}
	}
	installErr := err
	extra := map[uint64][]trafficRule{}
	allRules := func() []trafficRule {
		out := append([]trafficRule(nil), rules...)
		for _, rs := range extra {
			out = append(out, rs...)
		}
		return out
	}
	persist := func(rs []trafficRule) error {
		b, e := json.Marshal(rs)
		if e != nil {
			return e
		}
		return os.WriteFile(journal, b, 0600)
	}
	if installErr == nil {
		fmt.Fprintln(os.Stdout, "ready")
		timer := time.NewTimer(time.Duration(seconds) * time.Second)
		defer timer.Stop()
	loop:
		for {
			select {
			case <-done:
				break loop
			case <-ctx.Done():
				break loop
			case <-timer.C:
				break loop
			case c := <-commands:
				var e error
				if c.Renew {
					if !interactive {
						e = fmt.Errorf("fixed deadline cannot be renewed")
					} else {
						if !timer.Stop() {
							select {
							case <-timer.C:
							default:
							}
						}
						timer.Reset(time.Duration(seconds) * time.Second)
					}
				} else if !interactive || c.ID == 0 {
					e = fmt.Errorf("interactive session required")
				} else {
					old := extra[c.ID]
					// Journal both old and new rules before any mutation, including rollback.
					e = persist(append(allRules(), c.Rules...))
					if e == nil {
						e = trafficCleanup(old)
					}
					if e == nil {
						delete(extra, c.ID)
						extra[c.ID] = c.Rules
						for _, r := range c.Rules {
							if e = trafficCommand(r.Add); e != nil {
								break
							}
						}
						if e != nil {
							if ce := trafficCleanup(c.Rules); ce != nil {
								e = errors.Join(e, ce)
							} else {
								delete(extra, c.ID)
							}
						}
						if len(extra[c.ID]) == 0 {
							delete(extra, c.ID)
						}
						e = errors.Join(e, persist(allRules()))
					}
				}
				ack := trafficGuardReply{}
				if e != nil {
					ack.Error = e.Error()
				}
				if json.NewEncoder(os.Stdout).Encode(ack) != nil {
					break loop
				}
			}
		}
	}
	rules = allRules()

	for attempt := 0; attempt < 3; attempt++ {
		err = trafficCleanup(rules)
		if err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err == nil {
		os.Remove(journal)
		os.Remove(filepath.Dir(journal))
	}
	return errors.Join(installErr, err)
}

type trafficGuardCommand struct {
	Renew bool
	ID    uint64
	Rules []trafficRule
}
type trafficGuardReply struct{ Error string }
type trafficGuardian struct {
	close   func() error
	command func(trafficGuardCommand) error
}

func trafficGuard(ctx context.Context, lock *net.UnixListener, rules []trafficRule, seconds int) (*trafficGuardian, error) {
	dir, err := os.MkdirTemp("", "podtoast-traffic-")
	if err != nil {
		return nil, err
	}
	journal := filepath.Join(dir, "rules.json")
	b, _ := json.Marshal(rules)
	if err = os.WriteFile(journal, b, 0600); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	fd, err := lock.File()
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	defer fd.Close()
	r, w, err := os.Pipe()
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	defer r.Close()
	exe, err := os.Executable()
	if err != nil {
		w.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	cmd := exec.Command(exe, "traffic-watchdog", journal, fmt.Sprint(seconds))
	cmd.ExtraFiles = []*os.File{fd, r}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	ready, err := cmd.StdoutPipe()
	if err != nil {
		w.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		w.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	ch := make(chan error, 1)
	go func() {
		buf := make([]byte, 6)
		_, e := io.ReadFull(ready, buf)
		if e == nil && string(buf) != "ready\n" {
			e = fmt.Errorf("invalid watchdog handshake")
		}
		ch <- e
	}()
	select {
	case err = <-ch:
	case <-ctx.Done():
		err = ctx.Err()
	case <-time.After(45 * time.Second):
		err = fmt.Errorf("watchdog startup timeout")
	}
	if err != nil {
		w.Close()
		cmd.Wait() // watchdog owns rollback, including partial installation
		return nil, fmt.Errorf("traffic watchdog: %w: %.2048s", err, stderr.String())
	}
	var once sync.Once
	var result error
	var commandMu sync.Mutex
	guardian := &trafficGuardian{}
	guardian.close = func() error {
		once.Do(func() {
			w.Close()
			result = cmd.Wait()
			if result != nil {
				result = fmt.Errorf("watchdog cleanup: %w: %.2048s", result, stderr.String())
			}
		})
		return result
	}
	guardian.command = func(c trafficGuardCommand) error {
		commandMu.Lock()
		defer commandMu.Unlock()
		if e := json.NewEncoder(w).Encode(c); e != nil {
			return e
		}
		var ack trafficGuardReply
		if e := json.NewDecoder(ready).Decode(&ack); e != nil {
			return e
		}
		if ack.Error != "" {
			return errors.New(ack.Error)
		}
		return nil
	}
	return guardian, nil
}

func markTraffic(c *net.TCPConn) error {
	raw, e := c.SyscallConn()
	if e != nil {
		return e
	}
	var inner error
	e = raw.Control(func(fd uintptr) { inner = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, 36, trafficMark) })
	if e != nil {
		return e
	}
	return inner
}
func originalTrafficDestination(c *net.TCPConn) (string, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return "", err
	}
	var data [16]byte
	size := uint32(len(data))
	var opErr syscall.Errno
	err = raw.Control(func(fd uintptr) {
		_, _, opErr = syscall.Syscall6(syscall.SYS_GETSOCKOPT, fd, syscall.SOL_IP, 80, uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&size)), 0)
	})
	if err != nil {
		return "", err
	}
	if opErr != 0 {
		return "", opErr
	}
	if size != 16 {
		return "", fmt.Errorf("invalid original destination")
	}
	return net.JoinHostPort(net.IP(data[4:8]).String(), fmt.Sprint(binary.BigEndian.Uint16(data[2:4]))), nil
}
func (s *TrafficServer) start(request context.Context, c TrafficConfig) (func() error, error) {
	if request.Err() != nil {
		return nil, request.Err()
	}
	// Abstract Unix sockets belong to the pod network namespace, unlike /tmp.
	lock, err := net.ListenUnix("unix", &net.UnixAddr{Name: "\x00podtoast-traffic-v1", Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("another traffic experiment is active in this pod: %w", err)
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{Port: 0})
	if err != nil {
		lock.Close()
		return nil, err
	}
	if listener.Addr().(*net.TCPAddr).Port == c.Port {
		listener.Close()
		lock.Close()
		return nil, fmt.Errorf("proxy port collision; retry")
	}
	var token [6]byte
	if _, err = rand.Read(token[:]); err != nil {
		lock.Close()
		listener.Close()
		return nil, err
	}
	name := fmt.Sprintf("PT_%x", token)
	rules := trafficRules(c, name, listener.Addr().(*net.TCPAddr).Port, fmt.Sprintf("0x%x", trafficMark))
	seconds := c.DurationSeconds
	if c.Interactive {
		seconds = -seconds
	}
	guard, err := trafficGuard(request, lock, rules, seconds)
	if err != nil {
		lock.Close()
		listener.Close()
		return nil, err
	}
	if request.Err() != nil {
		listener.Close()
		_ = guard.close()
		lock.Close()
		return nil, request.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	var result error
	var workers sync.WaitGroup
	stop := func() error {
		once.Do(func() { // Remove redirection before stopping the proxy.
			result = guard.close()
			cancel()
			listener.Close()
			workers.Wait()
			lock.Close()
			s.mu.Lock()
			s.state.Active = false
			if result != nil {
				s.state.CleanupError = result.Error()
			}
			s.mu.Unlock()
		})
		return result
	}
	flows := map[uint64]*trafficFlow{}
	s.renew = func() error {
		if !c.Interactive {
			return fmt.Errorf("fixed deadline cannot be renewed")
		}
		if e := guard.command(trafficGuardCommand{Renew: true}); e != nil {
			return e
		}
		s.mu.Lock()
		s.state.Expires = time.Now().Add(time.Duration(c.DurationSeconds) * time.Second)
		s.mu.Unlock()
		return nil
	}
	s.action = func(a TrafficAction) error {
		if !c.Interactive {
			return fmt.Errorf("interactive session required")
		}
		if a.Scope == "port" {
			if a.Port != c.Port {
				return fmt.Errorf("port is outside this monitor's scope")
			}
			if a.Action == "reset" {
				s.mu.Lock()
				list := make([]*trafficFlow, 0, len(flows))
				for _, f := range flows {
					list = append(list, f)
				}
				s.mu.Unlock()
				for _, f := range list {
					f.mu.Lock()
					if !f.closed {
						f.reset()
					}
					f.mu.Unlock()
				}
				return nil
			}
			f := a.Faults
			if a.Action == "clear" {
				f = TrafficFaults{}
			}
			var rules []trafficRule
			if f.LossPercent > 0 {
				args := []string{"-w", "2", "-t", "mangle", "-A", name, "-m", "statistic", "--mode", "random", "--probability", fmt.Sprintf("%.8f", f.LossPercent/100), "-j", "DROP"}
				undo := append([]string(nil), args...)
				undo[4] = "-D"
				rules = []trafficRule{{args, undo}}
			}
			if e := guard.command(trafficGuardCommand{ID: ^uint64(0), Rules: rules}); e != nil {
				return e
			}
			s.mu.Lock()
			s.state.PortFaults = f
			close(s.changed)
			s.changed = make(chan struct{})
			s.mu.Unlock()
			return nil
		}
		s.mu.Lock()
		flow := flows[a.ID]
		s.mu.Unlock()
		if flow == nil {
			return fmt.Errorf("connection has closed")
		}
		flow.mu.Lock()
		defer flow.mu.Unlock()
		if flow.closed {
			return fmt.Errorf("connection has closed")
		}
		if a.Action == "reset" {
			flow.reset()
			return nil
		}
		f := a.Faults
		if a.Action == "clear" {
			f = TrafficFaults{}
		}
		var rules []trafficRule
		if f.LossPercent > 0 {
			src, sp, e := net.SplitHostPort(flow.client)
			if e != nil {
				return e
			}
			dst, dp, e := net.SplitHostPort(flow.destination)
			if e != nil {
				return e
			}
			args := []string{"-w", "2", "-t", "mangle", "-A", name, "-p", "tcp", "-s", src, "--sport", sp, "-d", dst, "--dport", dp, "-m", "statistic", "--mode", "random", "--probability", fmt.Sprintf("%.8f", f.LossPercent/100), "-j", "DROP"}
			undo := append([]string(nil), args...)
			undo[4] = "-D"
			rules = []trafficRule{{args, undo}}
		}
		if e := guard.command(trafficGuardCommand{ID: a.ID, Rules: rules}); e != nil {
			return e
		}
		flow.faults = f
		s.mu.Lock()
		close(s.changed)
		s.changed = make(chan struct{})
		for i := range s.state.Connections {
			if s.state.Connections[i].ID == a.ID {
				s.state.Connections[i].Faults = f
			}
		}
		s.mu.Unlock()
		return nil
	}
	s.mu.Lock()
	s.changed = make(chan struct{})
	s.state = TrafficStatus{Active: true, Config: c, Expires: time.Now().Add(time.Duration(c.DurationSeconds) * time.Second), Note: "IPv4 TCP only; first HTTP/1 request/response headers per connection; TLS opaque. Delay once per connection; bandwidth per connection and direction. Packet loss is real, before NAT. Original client source IP is not preserved upstream. Existing connections are not redirected."}
	s.mu.Unlock()
	sem := make(chan struct{}, 128)
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			conn, e := listener.AcceptTCP()
			if e != nil {
				return
			}
			select {
			case sem <- struct{}{}:
				workers.Add(1)
				go func() { defer workers.Done(); defer func() { <-sem }(); s.proxyTraffic(ctx, c, conn, flows, guard) }()
			default:
				s.mu.Lock()
				s.state.Rejected++
				s.mu.Unlock()
				conn.Close()
			}
		}
	}()
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if time.Now().After(s.snapshot().Expires) {
					_ = stop()
					return
				}
			}
		}
	}()

	return stop, nil
}

type trafficFlow struct {
	closed              bool
	mu                  sync.Mutex
	faults              TrafficFaults
	reset               func()
	client, destination string
}

func (s *TrafficServer) proxyTraffic(ctx context.Context, c TrafficConfig, client *net.TCPConn, flows map[uint64]*trafficFlow, guard *trafficGuardian) {
	defer client.Close()
	dst, err := originalTrafficDestination(client)
	if err == nil {
		_, port, e := net.SplitHostPort(dst)
		if e != nil || port != fmt.Sprint(c.Port) {
			return
		}
		err = markTraffic(client)
	}
	if err != nil {
		s.mu.Lock()
		s.state.Rejected++
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	s.state.Accepted++
	id := s.state.Accepted
	entry := TrafficConnection{ID: id, Client: client.RemoteAddr().String(), Destination: dst, Opened: time.Now()}
	if len(s.state.Connections) >= 256 {
		for i, e := range s.state.Connections {
			if e.Closed {
				s.state.Connections = append(s.state.Connections[:i], s.state.Connections[i+1:]...)
				break
			}
		}
	}
	s.state.Connections = append(s.state.Connections, entry)
	s.mu.Unlock()
	update := func(fn func(*TrafficConnection)) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i := range s.state.Connections {
			if s.state.Connections[i].ID == id {
				fn(&s.state.Connections[i])
				break
			}
		}
	}
	defer update(func(e *TrafficConnection) { e.Closed = true })
	connctx, cancel := context.WithCancel(ctx)
	defer cancel()
	closeClient := context.AfterFunc(connctx, func() { client.Close() })
	defer closeClient()
	if !trafficWait(connctx, time.Duration(c.DelayMS)*time.Millisecond) {
		return
	}
	dial := net.Dialer{Timeout: 10 * time.Second, Control: func(_, _ string, raw syscall.RawConn) error {
		var e error
		err := raw.Control(func(fd uintptr) { e = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, 36, trafficMark) })
		if err != nil {
			return err
		}
		return e
	}}
	remote, err := dial.DialContext(connctx, "tcp4", dst)
	if err != nil {
		update(func(e *TrafficConnection) { e.Error = err.Error() })
		return
	}
	defer remote.Close()
	upstream := remote.(*net.TCPConn)
	closeRemote := context.AfterFunc(connctx, func() { remote.Close() })
	defer closeRemote()
	flow := &trafficFlow{client: client.RemoteAddr().String(), destination: dst, faults: TrafficFaults{RateKiB: c.RateKiB}}
	flow.reset = func() {
		client.SetLinger(0)
		upstream.SetLinger(0)
		s.mu.Lock()
		s.state.Resets++
		s.mu.Unlock()
		cancel()
	}
	s.mu.Lock()
	flows[id] = flow
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(flows, id)
		s.mu.Unlock()
		// Clear only this flow's rules. No server control lock: stop waits for workers.
		flow.mu.Lock()
		defer flow.mu.Unlock()
		flow.closed = true
		loss := flow.faults.LossPercent
		if loss > 0 {
			if e := guard.command(trafficGuardCommand{ID: id}); e != nil && ctx.Err() == nil {
				s.mu.Lock()
				s.state.CleanupError = "flow rule cleanup: " + e.Error()
				s.mu.Unlock()
			}
		}
	}()
	var reset *time.Timer
	if c.ResetAfterMS > 0 {
		reset = time.AfterFunc(time.Duration(c.ResetAfterMS)*time.Millisecond, func() {
			client.SetLinger(0)
			upstream.SetLinger(0)
			s.mu.Lock()
			s.state.Resets++
			s.mu.Unlock()
			cancel()
		})
		defer reset.Stop()
	}
	pump := func(to, from *net.TCPConn, response bool) {
		buf := make([]byte, 16<<10)
		prefix := make([]byte, 0, 8192)
		observed := false
		for {
			n, re := from.Read(buf)
			if n > 0 {
				if !observed {
					prefix = append(prefix, buf[:min(n, 8192-len(prefix))]...)
					if len(prefix) == 8192 || strings.Contains(string(prefix), "\r\n\r\n") || prefix[0] == 0x16 {
						method, path, status := trafficHTTP(prefix, response)
						update(func(e *TrafficConnection) {
							if response {
								e.HTTPStatus = status
							} else {
								e.HTTPMethod, e.HTTPPath = method, path
							}
						})
						observed = true
						prefix = nil
					}
				}
				// Re-evaluate waits when either scope changes so clearing a
				// long delay/rate limit resumes a queued chunk immediately.
				started := time.Now()
				for {
					s.mu.Lock()
					port, changed := s.state.PortFaults, s.changed
					s.mu.Unlock()
					flow.mu.Lock()
					f := flow.faults
					flow.mu.Unlock()
					delay := time.Duration(f.DelayMS+port.DelayMS) * time.Millisecond
					rate := f.RateKiB
					if port.RateKiB > 0 && (rate == 0 || port.RateKiB < rate) {
						rate = port.RateKiB
					}
					if rate > 0 {
						delay += time.Duration(float64(n) / float64(rate*1024) * float64(time.Second))
					}
					remaining := delay - time.Since(started)
					if remaining <= 0 {
						break
					}
					timer := time.NewTimer(remaining)
					select {
					case <-connctx.Done():
						timer.Stop()
						return
					case <-changed:
						timer.Stop()
						continue
					case <-timer.C:
					}
					break
				}

				written, we := to.Write(buf[:n])
				s.mu.Lock()
				if response {
					s.state.Received += uint64(written)
				} else {
					s.state.Sent += uint64(written)
				}
				s.mu.Unlock()
				update(func(e *TrafficConnection) {
					if response {
						e.Received += uint64(written)
					} else {
						e.Sent += uint64(written)
					}
				})
				if we != nil {
					return
				}
			}
			if re != nil {
				to.CloseWrite()
				return
			}
		}
	}
	done := make(chan struct{})
	go func() { pump(upstream, client, false); close(done) }()
	pump(client, upstream, true)
	cancel()
	<-done
}

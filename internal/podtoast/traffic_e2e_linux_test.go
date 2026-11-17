//go:build linux

package podtoast

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func trafficTestClient(t *testing.T) (*Client, *exec.Cmd) {
	t.Helper()
	cmd := exec.Command("/podtoast", "traffic")
	in, e := cmd.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	cmd.Stderr = os.Stderr
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	conn := &StreamConn{Reader: out, Writer: in, CloseFunc: func() error { out.Close(); return in.Close() }}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, e := Connect(ctx, conn)
	if e != nil {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close(); cmd.Wait() })
	return c, cmd
}
func TestTrafficNetworkNamespace(t *testing.T) {
	if os.Getenv("PODTOAST_TRAFFIC_TEST") != "1" {
		t.Skip("isolated NET_ADMIN container only")
	}
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			time.Sleep(300 * time.Millisecond)
		}
		if r.URL.Path == "/bulk" {
			io.WriteString(w, strings.Repeat("x", 32<<10))
			return
		}
		io.WriteString(w, "hello")
	})}
	go server.Serve(listener)
	defer server.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	endpoint := "http://" + listener.Addr().String() + "/hello?token=private"
	request := func() (time.Duration, error) {
		start := time.Now()
		client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
		resp, e := client.Get(endpoint)
		if e == nil {
			_, e = io.ReadAll(resp.Body)
			resp.Body.Close()
		}
		return time.Since(start), e
	}
	if b, e := exec.Command("iptables", "-t", "nat", "-N", "USER_KEEP").CombinedOutput(); e != nil {
		t.Fatalf("NET_ADMIN fixture: %v %s", e, b)
	}
	defer exec.Command("iptables", "-t", "nat", "-X", "USER_KEEP").Run()
	c, cmd := trafficTestClient(t)
	cfg := TrafficConfig{Direction: "outbound", Port: port, PeerCIDR: "127.0.0.0/8", DurationSeconds: 20, DelayMS: 180}
	ctx := context.Background()
	var state TrafficStatus
	if e = c.JSON(ctx, "POST", "/v1/traffic/start", cfg, &state); e != nil {
		t.Fatal(e)
	}
	elapsed, e := request()
	if e != nil || elapsed < 160*time.Millisecond {
		t.Fatalf("delay/proxy: %v %v", elapsed, e)
	}
	time.Sleep(60 * time.Millisecond)
	if e = c.JSON(ctx, "GET", "/v1/traffic", nil, &state); e != nil || state.Accepted != 1 || state.Sent == 0 || state.Received == 0 || len(state.Connections) != 1 || state.Connections[0].HTTPPath != "/hello" || state.Connections[0].HTTPStatus != "200" {
		t.Fatalf("stats: %+v %v", state, e)
	}
	// A second process in the same network namespace must not install rules.
	second, _ := trafficTestClient(t)
	if e = second.JSON(ctx, "POST", "/v1/traffic/start", cfg, &state); e == nil {
		t.Fatal("overlapping session accepted")
	}
	second.Close()
	// Kill the controller, not a graceful stop. The independent watchdog removes rules.
	if e = cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, _ := exec.Command("iptables", "-t", "nat", "-S").CombinedOutput()
		if !strings.Contains(string(b), "PT_") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("watchdog left rules: %s", b)
		}
		time.Sleep(100 * time.Millisecond)
	}
	elapsed, e = request()
	if e != nil || elapsed > 160*time.Millisecond {
		t.Fatalf("restored: %v %v", elapsed, e)
	}
	third, _ := trafficTestClient(t)
	cfg.DelayMS = 0
	cfg.LossPercent = 100
	if e = third.JSON(ctx, "POST", "/v1/traffic/start", cfg, &state); e != nil {
		t.Fatal(e)
	}
	if _, e = request(); e == nil {
		t.Fatal("100 percent packet loss did not drop packets")
	}
	if e = third.JSON(ctx, "POST", "/v1/traffic/stop", nil, &state); e != nil {
		t.Fatal(e)
	}
	if _, e = request(); e != nil {
		t.Fatal("stop did not restore traffic", e)
	}
	cfg.LossPercent = 0
	cfg.RateKiB = 64
	endpoint = "http://" + listener.Addr().String() + "/bulk"
	if e = third.JSON(ctx, "POST", "/v1/traffic/start", cfg, &state); e != nil {
		t.Fatal(e)
	}
	elapsed, e = request()
	if e != nil || elapsed < 450*time.Millisecond {
		t.Fatalf("rate: %v %v", elapsed, e)
	}
	if e = third.JSON(ctx, "POST", "/v1/traffic/stop", nil, &state); e != nil {
		t.Fatal(e)
	}
	cfg.RateKiB = 0
	cfg.ResetAfterMS = 40
	endpoint = "http://" + listener.Addr().String() + "/slow"
	if e = third.JSON(ctx, "POST", "/v1/traffic/start", cfg, &state); e != nil {
		t.Fatal(e)
	}
	if _, e = request(); e == nil {
		t.Fatal("reset did not break slow response")
	}
	if e = third.JSON(ctx, "POST", "/v1/traffic/stop", nil, &state); e != nil {
		t.Fatal(e)
	}
	cfg.ResetAfterMS = 0
	cfg.DurationSeconds = 5
	if e = third.JSON(ctx, "POST", "/v1/traffic/start", cfg, &state); e != nil {
		t.Fatal(e)
	}
	time.Sleep(5500 * time.Millisecond)
	if e = third.JSON(ctx, "GET", "/v1/traffic", nil, &state); e != nil || state.Active || state.CleanupError != "" {
		t.Fatalf("deadline: %+v %v", state, e)
	}
	if b, e := exec.Command("iptables", "-t", "nat", "-S", "USER_KEEP").CombinedOutput(); e != nil {
		t.Fatalf("unrelated chain changed: %v %s", e, b)
	}

}

func TestTrafficInteractiveNamespace(t *testing.T) {
	if os.Getenv("PODTOAST_TRAFFIC_TEST") != "1" {
		t.Skip("isolated NET_ADMIN container only")
	}
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	c, _ := trafficTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	var st TrafficStatus
	cfg := TrafficConfig{Interactive: true, Direction: "outbound", Port: l.Addr().(*net.TCPAddr).Port, PeerCIDR: "127.0.0.0/8", DurationSeconds: 5}
	if err = c.JSON(ctx, "POST", "/v1/traffic/start", cfg, &st); err != nil {
		t.Fatal(err)
	}
	defer c.JSON(context.Background(), "POST", "/v1/traffic/stop", nil, &st)
	renewCtx, stopRenew := context.WithCancel(ctx)
	defer stopRenew()
	renewDone := make(chan struct{})
	go func() {
		defer close(renewDone)
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-renewCtx.Done():
				return
			case <-tick.C:
				var state TrafficStatus
				_ = c.JSON(renewCtx, "POST", "/v1/traffic/renew", nil, &state)
			}
		}
	}()
	dial := func() net.Conn {
		conn, e := net.Dial("tcp4", l.Addr().String())
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { conn.Close() })
		return conn
	}
	first, second := dial(), dial()
	echo := func(conn net.Conn, timeout time.Duration) error {
		conn.SetDeadline(time.Now().Add(timeout))
		if _, e := conn.Write([]byte("ping")); e != nil {
			return e
		}
		b := make([]byte, 4)
		_, e := io.ReadFull(conn, b)
		return e
	}
	if e := echo(first, time.Second); e != nil {
		t.Fatal(e)
	}
	if e := echo(second, time.Second); e != nil {
		t.Fatal(e)
	}
	if err = c.JSON(ctx, "GET", "/v1/traffic", nil, &st); err != nil {
		t.Fatal(err)
	}
	var id uint64
	for _, flow := range st.Connections {
		if flow.Client == first.LocalAddr().String() {
			id = flow.ID
		}
	}
	if id == 0 {
		t.Fatalf("missing selected flow: %+v", st)
	}
	action := func(kind string, f TrafficFaults) {
		t.Helper()
		if e := c.JSON(ctx, "POST", "/v1/traffic/action", TrafficAction{ID: id, Action: kind, Faults: f}, &st); e != nil {
			t.Fatal(e)
		}
	}
	action("faults", TrafficFaults{DelayMS: 100, RateKiB: 1024})
	start := time.Now()
	if e := echo(first, time.Second); e != nil {
		t.Fatal(e)
	}
	if time.Since(start) < 180*time.Millisecond {
		t.Fatal("selected delay not applied to both directions")
	}
	start = time.Now()
	if e := echo(second, time.Second); e != nil {
		t.Fatal(e)
	}
	if time.Since(start) > 150*time.Millisecond {
		t.Fatal("peer inherited selected delay")
	}
	action("faults", TrafficFaults{LossPercent: 100})
	if e := echo(first, 250*time.Millisecond); e == nil {
		t.Fatal("100% packet loss did not block selected flow")
	}
	if e := echo(second, time.Second); e != nil {
		t.Fatalf("packet loss affected peer: %v", e)
	}
	action("clear", TrafficFaults{})
	first.SetDeadline(time.Now().Add(3 * time.Second))
	b := make([]byte, 4)
	if _, e := io.ReadFull(first, b); e != nil {
		t.Fatalf("clear did not resume lost transmission: %v", e)
	}
	action("reset", TrafficFaults{})
	if e := echo(first, time.Second); e == nil {
		t.Fatal("reset left selected flow alive")
	}
	if e := echo(second, time.Second); e != nil {
		t.Fatalf("reset affected peer: %v", e)
	}

	portAction := func(kind string, f TrafficFaults) {
		t.Helper()
		if e := c.JSON(ctx, "POST", "/v1/traffic/action", TrafficAction{Scope: "port", Port: cfg.Port, Action: kind, Faults: f}, &st); e != nil {
			t.Fatal(e)
		}
	}
	if e := c.JSON(ctx, "POST", "/v1/traffic/action", TrafficAction{Scope: "port", Port: cfg.Port%65535 + 1, Action: "clear"}, &st); e == nil {
		t.Fatal("action escaped selected port")
	}
	portAction("faults", TrafficFaults{DelayMS: 100})
	start = time.Now()
	if e := echo(second, time.Second); e != nil {
		t.Fatal(e)
	}
	if time.Since(start) < 180*time.Millisecond {
		t.Fatal("port delay not applied to existing flow")
	}
	third := dial()
	start = time.Now()
	if e := echo(third, time.Second); e != nil {
		t.Fatal(e)
	}
	if time.Since(start) < 180*time.Millisecond {
		t.Fatal("port delay not inherited by future flow")
	}
	for _, flow := range st.Connections {
		if flow.Client == second.LocalAddr().String() {
			id = flow.ID
		}
	}
	action("faults", TrafficFaults{DelayMS: 100})
	start = time.Now()
	if e := echo(second, time.Second); e != nil {
		t.Fatal(e)
	}
	if time.Since(start) < 360*time.Millisecond {
		t.Fatal("connection and port delays did not add")
	}
	action("clear", TrafficFaults{})
	start = time.Now()
	if e := echo(second, time.Second); e != nil {
		t.Fatal(e)
	}
	if time.Since(start) < 180*time.Millisecond {
		t.Fatal("connection clear incorrectly cleared port")
	}
	portAction("faults", TrafficFaults{LossPercent: 100})
	if e := echo(second, 200*time.Millisecond); e == nil {
		t.Fatal("port block missed existing flow")
	}
	if e := echo(third, 200*time.Millisecond); e == nil {
		t.Fatal("port block missed other flow")
	}
	other, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	go func() {
		conn, e := other.Accept()
		if e == nil {
			defer conn.Close()
			io.Copy(conn, conn)
		}
	}()
	unaffected, e := net.Dial("tcp4", other.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer unaffected.Close()
	if e := echo(unaffected, time.Second); e != nil {
		t.Fatalf("port block affected another port: %v", e)
	}
	if blocked, e := net.DialTimeout("tcp4", l.Addr().String(), 200*time.Millisecond); e == nil {
		blocked.Close()
		t.Fatal("port block allowed new flow")
	}
	portAction("clear", TrafficFaults{})
	for _, conn := range []net.Conn{second, third} {
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		if _, e := io.ReadFull(conn, make([]byte, 4)); e != nil {
			t.Fatal(e)
		}
	}
	portAction("faults", TrafficFaults{DelayMS: 30000})
	resumed := make(chan error, 1)
	go func() { resumed <- echo(second, 2*time.Second) }()
	time.Sleep(100 * time.Millisecond)
	portAction("clear", TrafficFaults{})
	if e := <-resumed; e != nil {
		t.Fatalf("clearing delay did not wake queued data: %v", e)
	}
	portAction("reset", TrafficFaults{})
	for _, conn := range []net.Conn{second, third} {
		if e := echo(conn, time.Second); e == nil {
			t.Fatal("port reset left flow open")
		}
	}
	second = dial()
	if e := echo(second, time.Second); e != nil {
		t.Fatalf("port reset affected future connection: %v", e)
	}
	// Renewal must keep the session alive beyond its original 5-second lease.
	time.Sleep(6 * time.Second)
	if e := c.JSON(ctx, "GET", "/v1/traffic", nil, &st); e != nil || !st.Active {
		t.Fatalf("renewal: %v %+v", e, st)
	}
	for _, flow := range st.Connections {
		if flow.Client == second.LocalAddr().String() {
			id = flow.ID
		}
	}
	action("faults", TrafficFaults{LossPercent: 20})
	stopRenew()
	<-renewDone
	time.Sleep(6 * time.Second)
	if e := c.JSON(ctx, "GET", "/v1/traffic", nil, &st); e != nil || st.Active {
		t.Fatalf("lease expiry: %v %+v", e, st)
	}
	for _, table := range []string{"nat", "mangle"} {
		out, e := exec.Command("iptables", "-t", table, "-S").CombinedOutput()
		if e != nil || strings.Contains(string(out), "PT_") {
			t.Fatalf("rules remain: %s %v", out, e)
		}
	}
}

func TestTrafficOverviewNamespace(t *testing.T) {
	if os.Getenv("PODTOAST_TRAFFIC_TEST") != "1" {
		t.Skip("disposable namespace only")
	}
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	conn, e := net.Dial("tcp4", listener.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	accepted, e := listener.Accept()
	if e != nil {
		t.Fatal(e)
	}
	defer accepted.Close()
	udp, e := net.ListenPacket("udp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer udp.Close()
	c, _ := trafficTestClient(t)
	for _, method := range []string{"GET", "POST"} {
		var st TrafficStatus
		if e := c.JSON(context.Background(), method, "/v1/traffic/overview", nil, &st); e != nil {
			t.Fatal(e)
		}
		if st.Active || st.Overview == nil {
			t.Fatalf("passive view activated proxy: %+v", st)
		}
		listening, established, udpFound := false, false, false
		for _, s := range st.Overview.Sockets {
			if s.Protocol == "tcp" && s.Local == listener.Addr().String() {
				if s.State == "LISTEN" {
					listening = true
				}
				if s.State == "ESTABLISHED" {
					established = true
				}
			}
			if s.Protocol == "udp" && s.Local == udp.LocalAddr().String() {
				udpFound = true
			}
		}
		if !listening || !established || !udpFound {
			t.Fatalf("pre-existing sockets missing: %+v", st.Overview)
		}
	}
	for _, table := range []string{"nat", "mangle"} {
		out, e := exec.Command("iptables", "-t", table, "-S").CombinedOutput()
		if e != nil || strings.Contains(string(out), "PT_") {
			t.Fatalf("observation installed rules: %v %s", e, out)
		}
	}
}

func TestTrafficCaptureNamespace(t *testing.T) {
	if os.Getenv("PODTOAST_TRAFFIC_TEST") != "1" {
		t.Skip("disposable namespace only")
	}
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	go func() {
		for {
			conn, e := listener.Accept()
			if e != nil {
				return
			}
			go func() { defer conn.Close(); io.Copy(conn, conn) }()
		}
	}()
	c, _ := trafficTestClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var st TrafficStatus
	if e := c.JSON(ctx, "GET", "/v1/traffic/overview", nil, &st); e != nil || st.Capture == nil || st.Capture.Error != "" {
		t.Fatalf("capture unavailable: %+v %v", st.Capture, e)
	}
	for i := 0; i < 12; i++ {
		conn, e := net.Dial("tcp4", listener.Addr().String())
		if e != nil {
			t.Fatal(e)
		}
		conn.SetDeadline(time.Now().Add(time.Second))
		if _, e = conn.Write([]byte("short request")); e != nil {
			t.Fatal(e)
		}
		if _, e = io.ReadFull(conn, make([]byte, 13)); e != nil {
			t.Fatal(e)
		}
		conn.Close()
	}
	udp, e := net.ListenPacket("udp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer udp.Close()
	sender, e := net.Dial("udp4", udp.LocalAddr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer sender.Close()
	sender.Write([]byte("datagram"))
	udp.SetDeadline(time.Now().Add(time.Second))
	udp.ReadFrom(make([]byte, 64))
	time.Sleep(100 * time.Millisecond)
	if e := c.JSON(ctx, "GET", "/v1/traffic/overview", nil, &st); e != nil {
		t.Fatal(e)
	}
	tcpBytes, udpBytes := uint64(0), uint64(0)
	connections := uint64(0)
	for _, g := range st.Capture.Groups {
		if int(g.Port) == listener.Addr().(*net.TCPAddr).Port {
			tcpBytes += g.Sent + g.Received
			connections += g.Connections
		}
		if int(g.Port) == udp.LocalAddr().(*net.UDPAddr).Port {
			udpBytes += g.Sent + g.Received
		}
	}
	if tcpBytes < 12*26 || udpBytes < 8 || connections < 12 {
		t.Fatalf("short TCP/UDP not captured: %+v", st.Capture)
	}
	if st.Active {
		t.Fatal("capture enabled traffic modification")
	}
}

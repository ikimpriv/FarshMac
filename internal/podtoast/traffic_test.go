package podtoast

import (
	"math"
	"strings"
	"testing"
)

func TestTrafficValidationAndScope(t *testing.T) {
	valid := TrafficConfig{Direction: "outbound", Port: 8080, PeerCIDR: "10.0.0.0/8", DurationSeconds: 30}
	if e := valid.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*TrafficConfig){func(c *TrafficConfig) { c.Port = 0 }, func(c *TrafficConfig) { c.Direction = "all" }, func(c *TrafficConfig) { c.DurationSeconds = 0 }, func(c *TrafficConfig) { c.PeerCIDR = "::/0" }, func(c *TrafficConfig) { c.PeerCIDR = "0/0; bad" }, func(c *TrafficConfig) { c.LossPercent = math.NaN() }, func(c *TrafficConfig) { c.RateKiB = -1 }} {
		c := valid
		change(&c)
		if c.Validate() == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
	valid.LossPercent = 12.5
	rules := trafficRules(valid, "PT_test", 32000, "0x504f4454")
	for _, r := range rules {
		add := strings.Join(r.Add, " ")
		remove := strings.Join(r.Remove, " ")
		if strings.Contains(add, "PREROUTING") {
			t.Fatal("outbound modified inbound", add)
		}
		if strings.Contains(add, "-A OUTPUT") && (!strings.Contains(add, "--dport 8080") || !strings.Contains(add, "10.0.0.0/8") || !strings.Contains(add, "! --mark")) {
			t.Fatal("unscoped rule", add)
		}
		if strings.Contains(remove, "-F") || !strings.Contains(remove, "PT_test") {
			t.Fatal("destructive cleanup", remove)
		}
	}
}
func TestTrafficHTTPPrivacy(t *testing.T) {
	m, p, _ := trafficHTTP([]byte("GET /hello?token=secret HTTP/1.1\r\nAuthorization: Bearer secret\r\nCookie: secret\r\n\r\nbody"), false)
	if m != "GET" || p != "/hello" {
		t.Fatal(m, p)
	}
	_, _, status := trafficHTTP([]byte("HTTP/1.1 204 No Content\r\n\r\n"), true)
	if status != "204" {
		t.Fatal(status)
	}
	m, p, status = trafficHTTP([]byte{0x16, 3, 3, 0, 12}, false)
	if m != "" || p != "" || status != "" {
		t.Fatal("TLS inspected")
	}
}

func TestInteractiveTrafficValidation(t *testing.T) {
	for _, a := range []TrafficAction{{}, {ID: 1, Action: "unknown"}, {ID: 1, Action: "faults", Faults: TrafficFaults{LossPercent: 101}}, {ID: 1, Action: "faults", Faults: TrafficFaults{DelayMS: -1}}, {ID: 1, Action: "faults", Faults: TrafficFaults{RateKiB: 1048577}}} {
		if a.Validate() == nil {
			t.Fatalf("accepted %+v", a)
		}
	}
	for _, kind := range []string{"reset", "clear", "faults"} {
		if e := (TrafficAction{ID: 1, Action: kind}).Validate(); e != nil {
			t.Fatal(e)
		}
	}
}

func TestPortTrafficActionScope(t *testing.T) {
	for _, a := range []TrafficAction{{Scope: "port", Port: 0, Action: "clear"}, {Scope: "port", Port: 80, ID: 1, Action: "clear"}, {Scope: "connection", Port: 80, ID: 1, Action: "clear"}, {Scope: "all", Action: "reset"}} {
		if a.Validate() == nil {
			t.Fatalf("invalid scope accepted: %+v", a)
		}
	}
	for _, kind := range []string{"reset", "faults", "clear"} {
		if e := (TrafficAction{Scope: "port", Port: 8080, Action: kind}).Validate(); e != nil {
			t.Fatal(e)
		}
	}
}

package podtoast

import (
	"fmt"
	"strconv"
)

type trafficRule struct{ Add, Remove []string }

func trafficRules(c TrafficConfig, name string, listen int, mark string) []trafficRule {
	rules := []trafficRule{}
	add := func(table string, args ...string) {
		undo := append([]string(nil), args...)
		if args[0] == "-N" {
			undo[0] = "-X"
		} else {
			undo[0] = "-D"
		}
		rules = append(rules, trafficRule{append([]string{"-w", "2", "-t", table}, args...), append([]string{"-w", "2", "-t", table}, undo...)})
	}
	add("nat", "-N", name)
	add("nat", "-A", name, "-p", "tcp", "-j", "REDIRECT", "--to-ports", strconv.Itoa(listen))
	if c.LossPercent > 0 || c.Interactive {
		add("mangle", "-N", name)
		if c.LossPercent > 0 {
			add("mangle", "-A", name, "-m", "statistic", "--mode", "random", "--probability", fmt.Sprintf("%.8f", c.LossPercent/100), "-j", "DROP")
		}
	}
	for _, dir := range []string{"inbound", "outbound"} {
		if c.Direction != dir && c.Direction != "both" {
			continue
		}
		chain, peer := "PREROUTING", "-s"
		if dir == "outbound" {
			chain, peer = "OUTPUT", "-d"
		}
		args := []string{"-A", chain, "-p", "tcp", "--dport", strconv.Itoa(c.Port), peer, c.PeerCIDR, "-m", "mark", "!", "--mark", mark, "-j", name}
		// Loss runs before NAT, and only in the requested direction. The proxy's
		// own marked sockets bypass both loss and redirection.
		if c.LossPercent > 0 || c.Interactive {
			add("mangle", args...)
		}
		add("nat", args...)
	}
	return rules
}

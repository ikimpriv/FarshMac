package ui

import (
	"path"
	"strings"
	"time"
)

func (p *Panel) dockerContainerCatalog() bool {
	return p.Docker != nil && (p.Path == "/Containers" || path.Dir(p.Path) == "/Compose")
}
func dockerCreatedAt(value string) time.Time {
	for _, layout := range []string{"2006-01-02 15:04:05 -0700 MST", "2006-01-02 15:04:05 -0700", time.RFC3339Nano} {
		if t, e := time.Parse(layout, value); e == nil {
			return t
		}
	}
	return time.Time{}
}
func (i dockerCatalogInfo) containerAge() string {
	if i.created.IsZero() {
		return "—"
	}
	return kubeAge(i.created)
}
func (i dockerCatalogInfo) containerStatus() string {
	state := strings.ToLower(i.state)
	if state == "" {
		return "Unknown"
	}
	label := strings.ToUpper(state[:1]) + state[1:]
	if state == "running" {
		switch {
		case strings.Contains(i.status, "(unhealthy)"):
			label += " (unhealthy)"
		case strings.Contains(i.status, "(healthy)"):
			label += " (healthy)"
		case strings.Contains(i.status, "health: starting"):
			label += " (starting)"
		}
	}
	if state == "exited" || state == "restarting" {
		if start := strings.Index(i.status, "("); start >= 0 {
			if end := strings.Index(i.status[start:], ")"); end > 0 {
				label += " " + i.status[start:start+end+1]
			}
		}
	}
	return label
}
func (i dockerCatalogInfo) stateColor() uint32 {
	if strings.Contains(i.status, "(unhealthy)") {
		return 0xff5555
	}
	switch strings.ToLower(i.state) {
	case "dead", "restarting":
		return 0xff5555
	case "exited":
		if strings.Contains(i.status, "Exited (0)") {
			return 0x888899
		}
		if strings.Contains(i.status, "Exited (") {
			return 0xff5555
		}
		return 0x888899
	case "created", "removing":
		return 0x888899
	case "paused":
		return Yellow
	}
	return White
}

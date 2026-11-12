package ui

import (
	"strings"
)

const invalidFilterStatus = "Panel filter refers to unknown mask group; edit it in Commands → File panel filter…"

// activeFilter is the panel filter that loads apply. A filter that no longer
// validates (for example after its mask group was deleted) is ignored, so the
// panel shows every file instead of silently hiding all of them.
func (p *Panel) activeFilter() string {
	if p.Filter == "" || validateMasks(p.Filter, p.MaskGroups) != nil {
		return ""
	}
	return p.Filter
}

// filterFooter prefixes the panel footer with the filter; an ignored filter
// replaces the totals with an explanation.
func (p *Panel) filterFooter(status string) string {
	if p.Filter == "" {
		return status
	}
	if p.activeFilter() == "" {
		return "[" + p.Filter + "] invalid filter — showing all"
	}
	return "[" + p.Filter + "] " + status
}

// warnInvalidFilter explains an ignored panel filter in the status line.
func (a *App) warnInvalidFilter(i int) {
	p := &a.Panels[i]
	if p.Filter == "" {
		return
	}
	if err := validateMasks(p.Filter, p.MaskGroups); err != nil {
		if strings.Contains(err.Error(), "unknown mask group") {
			a.Status = invalidFilterStatus
		} else {
			a.Status = "Panel filter is invalid (" + err.Error() + "); edit it in Commands → File panel filter…"
		}
	}
}

// maskGroupUse names the first setting that refers to <name>, or "".
func (a *App) maskGroupUse(name string) string {
	ref := "<" + name + ">"
	for _, p := range a.Panels {
		if strings.Contains(p.Filter, ref) {
			return "a panel filter"
		}
	}
	if strings.Contains(a.Settings.SortGroups, ref) {
		return "sort groups"
	}
	for _, assoc := range a.Settings.Associations {
		if strings.Contains(assoc.Mask, ref) {
			return "file associations"
		}
	}
	return ""
}

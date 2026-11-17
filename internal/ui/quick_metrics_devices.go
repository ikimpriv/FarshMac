package ui

import (
	"fmt"
	"html"
	"math"
	"sort"
	"strings"
)

// deviceBars uses Kubernetes allocation units. A shared GPU may expose many
// slots; neither the bars nor their label claim physical GPU utilization.
func deviceBars(c *compactMetricsPanel, width, height int, fg string) (string, int) {
	if c.packet == nil {
		return "", 0
	}
	type device struct {
		scope, name                      string
		allocated, allocatable, capacity *float64
	}
	entries := map[string]*device{}
	for _, ref := range c.packet.References {
		if ref.Resource != "device" {
			continue
		}
		scope := metricScope(ref.Descriptor)
		name := ref.Labels["device"]
		key := scope + "/" + name
		d := entries[key]
		if d == nil {
			d = &device{scope: scope, name: name}
			entries[key] = d
		}
		value := ref.Value
		switch ref.Bound {
		case "allocated":
			d.allocated = &value
		case "allocatable":
			d.allocatable = &value
		case "capacity":
			d.capacity = &value
		}
	}
	if len(entries) == 0 {
		return "", 0
	}
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := max(1, min(4, (height/3-26)/44))
	rows = min(rows, len(keys))
	pages := (len(keys) + rows - 1) / rows
	offset := (c.legendPage % pages) * rows
	var b strings.Builder
	text := func(x, y int, s string) {
		fmt.Fprintf(&b, `<text x="%d" y="%d" fill="%s">%s</text>`, x, y, fg, html.EscapeString(s))
	}
	heading := "Devices · allocated / allocatable (not utilization)"
	if pages > 1 {
		heading += fmt.Sprintf(" · PgUp/PgDn %d/%d", c.legendPage%pages+1, pages)
	}
	text(4, 14, heading)
	for i := 0; i < rows && offset+i < len(keys); i++ {
		d := entries[keys[offset+i]]
		y := 32 + i*44
		label := d.scope + " · " + d.name
		fmt.Fprintf(&b, `<g class="device-allocation"><title>%s</title>`, html.EscapeString(label))
		text(4, y, tail(label, max(1, (width-8)/8)))
		value := "Unavailable"
		ratio := 0.0
		if d.allocated != nil && d.allocatable != nil {
			value = fmt.Sprintf("%.3g / %.3g allocated", *d.allocated, *d.allocatable)
			if *d.allocatable > 0 {
				ratio = math.Min(1, math.Max(0, *d.allocated / *d.allocatable))
			}
			if d.capacity != nil {
				value += fmt.Sprintf(" · capacity %.3g", *d.capacity)
			}
		}
		barWidth := max(24, min(150, width/3))
		fmt.Fprintf(&b, `<rect x="4" y="%d" width="%d" height="9" fill="none" stroke="%s" stroke-opacity="0.5"/><rect x="4" y="%d" width="%.2f" height="9" fill="%s" fill-opacity="0.6"/>`, y+7, barWidth, fg, y+7, ratio*float64(barWidth), fg)
		text(barWidth+12, y+16, value)
		b.WriteString("</g>")
	}
	return b.String(), 26 + rows*44
}

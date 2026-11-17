package ui

import (
	"encoding/base64"
	"fmt"
	"html"
	"math"
	"sort"
	"strings"
	"time"

	"farshmac/internal/metrics"
)

func (a *App) quickMetricsPreview() *ImagePreview {
	c := a.compactMetrics
	if c == nil || a.Panels[c.index].Hidden || a.Doc != nil || a.TermVisible {
		a.previewSent = ""
		return nil
	}
	x, w := a.panelRect(c.index)
	h := a.Rows - 10
	if w < 16 || h < 6 {
		return nil
	}
	data := quickMetricsSVG(c, a.Settings.ColorTheme, (w-4)*8, h*16)
	encoded := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(data))
	p := &ImagePreview{X: x + 2, Y: 5, W: w - 4, H: h, Stretch: true}
	if a.previewSent != encoded {
		p.Data = encoded
		a.previewSent = encoded
	}
	return p
}
func quickMetricsSVG(c *compactMetricsPanel, theme string, width, height int) string {
	bg, fg := "#000080", "#55ffff"
	colors := []string{"#55ffff", "#ffff55", "#ff88ff", "#55ff55", "#ff8888", "#ffffff"}
	if theme == "Dark" {
		bg, fg = "#151b24", "#c7d7e9"
	}
	if theme == "Light" {
		bg, fg = "#f1f3f5", "#182b49"
		colors = []string{"#006a8a", "#885000", "#8a2a8a", "#1d6f1d", "#a02020", "#182b49"}
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" preserveAspectRatio="none"><rect width="100%%" height="100%%" fill="%s"/><g font-family="Menlo,monospace" font-size="12" fill="%s">`, width, height, width, height, bg, fg)
	text := func(x, y int, value, color string) {
		fmt.Fprintf(&b, `<text x="%d" y="%d" fill="%s">%s</text>`, x, y, color, html.EscapeString(value))
	}
	var groups [3][]metrics.Series
	scopeIDs := map[string]int{}
	var scopes []string
	if c.packet != nil {
		for _, s := range c.packet.Series {
			i := basicMetric(s.Name)
			if i >= 0 && (i != 1 || quickMemoryMatches(s.Name, c.memoryMode)) {
				groups[i] = append(groups[i], s)
				scope := metricScope(s.Descriptor)
				if _, ok := scopeIDs[scope]; !ok {
					scopeIDs[scope] = 0
					scopes = append(scopes, scope)
				}
			}
		}
	}
	sort.Strings(scopes)
	for i, scope := range scopes {
		scopeIDs[scope] = i
	}
	count := 0
	for _, enabled := range c.enabled {
		if enabled {
			count++
		}
	}
	if count == 0 {
		text(4, 22, "Enable CPU, Memory or Storage above", fg)
	}
	rowHeight := (height - 20) / max(1, count)
	if count > 0 && rowHeight < 68 {
		text(4, 22, "More space needed; disable a metric above", fg)
		b.WriteString("</g></svg>")
		return b.String()
	}
	end := int64(0)
	for _, list := range groups {
		for _, series := range list {
			for _, point := range series.Points {
				end = max(end, point.Time)
			}
		}
	}
	top := 0
	for group, name := range []string{"CPU", "Memory · " + quickMemoryNames[c.memoryMode], "Ephemeral storage"} {
		if !c.enabled[group] {
			continue
		}
		list := groups[group]
		sort.Slice(list, func(i, j int) bool { return metricScope(list[i].Descriptor) < metricScope(list[j].Descriptor) })
		unit := func(v float64) string {
			if group == 0 {
				return fmt.Sprintf("%.3g cores", v)
			}
			return humanSize(int64(v))
		}
		text(4, top+13, name, fg)
		if len(list) == 0 {
			text(4, top+32, "Unavailable / waiting for source", fg)
			top += rowHeight
			continue
		}
		// Use a common time axis and zero baseline, never compare raw counters.
		start := end - 300000
		maximum := 0.0
		for _, s := range list {
			for _, p := range s.Points {
				v := compactSeriesValue(s, p)
				if p.Time >= start && v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) {
					maximum = max(maximum, *v)
				}
			}
		}
		var refs []metrics.Reference
		if c.packet != nil && len(scopes) == 1 {
			chosen := map[string]metrics.Reference{}
			for _, ref := range c.packet.References {
				if ref.Resource != []string{"cpu_cores", "memory_bytes", "ephemeral_storage_bytes"}[group] {
					continue
				}
				for _, series := range list {
					if metricScope(ref.Descriptor) != metricScope(series.Descriptor) {
						continue
					}
					key := metricScope(ref.Descriptor) + ref.Bound
					old, exists := chosen[key]
					if !exists || strings.HasPrefix(ref.Name, "farshmac_container_") || !strings.HasPrefix(old.Name, "farshmac_container_") {
						chosen[key] = ref
					}
				}
			}
			for _, ref := range chosen {
				refs = append(refs, ref)
				maximum = max(maximum, ref.Value)
			}
			sort.Slice(refs, func(i, j int) bool { return refs[i].Value > refs[j].Value })
		}
		if maximum <= 0 {
			maximum = 1
		}
		maximum = niceMetricMaximum(maximum, group != 0)
		legendRows := min(len(list), max(1, min(4, (rowHeight-73)/15)))
		plotTop := top + 28
		plotBottom := max(plotTop+12, top+rowHeight-legendRows*15-36)

		// Reserve a value-axis gutter; quarter divisions carry actual units.
		left, right := min(96, width/3), max(97, width-8)
		if right <= left {
			right = left + 1
		}
		for i := 0; i <= 4; i++ {
			y := plotTop + (plotBottom-plotTop)*i/4
			fmt.Fprintf(&b, `<path class="value-grid" d="M%d %d H%d" fill="none" stroke="%s" stroke-opacity="0.30"/>`, left, y, right, fg)
			fmt.Fprintf(&b, `<text class="value-tick" x="%d" y="%d" text-anchor="end" font-size="11" fill="%s">%s</text>`, left-8, y+4, fg, html.EscapeString(unit(maximum*float64(4-i)/4)))
		}
		text(max(left, width-140), top+13, time.UnixMilli(end).Local().Format("15:04:05")+" latest", fg)
		divisions := 5
		if right-left < 300 {
			divisions = 2
		}
		for i := 0; i <= divisions; i++ {
			x := left + (right-left)*i/divisions
			label := fmt.Sprintf("−%g min", 5*float64(divisions-i)/float64(divisions))
			anchor := "middle"
			if i == 0 {
				anchor = "start"
			}
			if i == divisions {
				label = "Latest"
				anchor = "end"
			}
			fmt.Fprintf(&b, `<text class="time-tick" x="%d" y="%d" text-anchor="%s" font-size="11" fill="%s">%s</text>`, x, plotBottom+15, anchor, fg, html.EscapeString(label))
			fmt.Fprintf(&b, `<path d="M%d %d V%d" fill="none" stroke="%s" stroke-opacity="0.18"/>`, x, plotTop, plotBottom, fg)
		}
		fmt.Fprintf(&b, `<rect class="plot-frame" x="%d" y="%d" width="%d" height="%d" fill="none" stroke="%s" stroke-opacity="0.6" stroke-width="1"/>`, left, plotTop, right-left, plotBottom-plotTop, fg)
		labelY := plotTop - 12
		for _, ref := range refs {
			y := float64(plotBottom) - ref.Value/maximum*float64(plotBottom-plotTop)
			color := "#ff6666"
			if theme == "Light" {
				color = "#bb2222"
			}
			dash := "0.1 4"
			if ref.Bound == "request" {
				color = "#55dd77"
				if theme == "Light" {
					color = "#16803a"
				}
			}
			label := strings.ToUpper(ref.Bound[:1]) + ref.Bound[1:] + " " + unit(ref.Value)
			fmt.Fprintf(&b, `<path d="M%d %.2f H%d" fill="none" stroke="%s" stroke-opacity="0.65" stroke-width="1" stroke-linecap="round" stroke-dasharray="%s"><title>%s</title></path>`, left, y, right, color, dash, html.EscapeString(label))
			ly := max(int(y)-3, labelY+12)
			if ly < plotBottom {
				b.WriteString(`<g opacity="0.65">`)
				text(left+4, ly, tail(label, max(1, (right-left)/8-2)), color)
				b.WriteString("</g>")
				labelY = ly
			}
		}
		for _, s := range list {
			color := colors[scopeIDs[metricScope(s.Descriptor)]%len(colors)]
			var points strings.Builder
			pen := false
			previous := int64(0)
			for _, p := range s.Points {
				if p.Time < start {
					continue
				}
				v := compactSeriesValue(s, p)
				if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) {
					pen = false
					continue
				}
				if previous > 0 && p.Time-previous > 15000 {
					pen = false
				}
				previous = p.Time
				x := float64(left) + float64(p.Time-start)/300000*float64(right-left)
				y := float64(plotBottom) - max(0, *v)/maximum*float64(plotBottom-plotTop)
				command := "L"
				if !pen {
					command = "M"
				}
				fmt.Fprintf(&points, "%s%.2f %.2f ", command, x, y)
				pen = true
			}
			fmt.Fprintf(&b, `<path d="%s" fill="none" stroke="%s" stroke-width="2" vector-effect="non-scaling-stroke"/>`, points.String(), color)
		}
		pages := (len(list) + legendRows - 1) / legendRows
		offset := (c.legendPage % pages) * legendRows
		for n := 0; n < legendRows && offset+n < len(list); n++ {
			s := list[offset+n]
			value := "—"
			if len(s.Points) > 0 {
				if v := compactSeriesValue(s, s.Points[len(s.Points)-1]); v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) {
					value = unit(*v)
				}
			}
			label := metricScope(s.Descriptor) + "  " + value
			text(4, plotBottom+34+n*15, tail(label, max(1, width/8)), colors[scopeIDs[metricScope(s.Descriptor)]%len(colors)])
		}
		if pages > 1 {
			text(4, top+rowHeight-2, fmt.Sprintf("PgUp/PgDn · legend %d/%d", c.legendPage%pages+1, pages), fg)
		}
		top += rowHeight
	}
	status := "5 min · separate line per container"
	if c.packet != nil {
		if c.packet.Limit || len(quickSeriesIDs(c.packet.Catalog)) > metrics.MaxSelected {
			status = "Series limit reached · narrow selection"
		} else {
			for _, ep := range c.packet.Endpoints {
				if ep.Enabled && ep.History != "" {
					status = ep.History
				}
				if ep.Enabled && ep.Error != "" {
					status = "Some sources unavailable"
					break
				}
			}
		}
	}
	text(4, height-3, status, fg)
	b.WriteString("</g></svg>")
	return b.String()
}

// Four equal intervals, with decimal CPU steps and binary byte steps.
func niceMetricMaximum(value float64, bytes bool) float64 {
	if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 1
	}
	step := value / 4
	if bytes {
		return 4 * math.Pow(2, math.Ceil(math.Log2(math.Max(1, step))))
	}
	base := math.Pow(10, math.Floor(math.Log10(step)))
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if step <= m*base*(1+1e-12) {
			return 4 * m * base
		}
	}
	return 40 * base
}

// Package metrics collects bounded live Prometheus series for interactive charts.
package metrics

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

const MaxBody = 2 << 20
const MaxSamples = 8192

type Sample struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels,omitempty"`
	Kind   string            `json:"kind"`
	Help   string            `json:"help,omitempty"`
	Unit   string            `json:"unit,omitempty"`
	Value  float64           `json:"-"`
}

func (s Sample) Key() string {
	keys := make([]string, 0, len(s.Labels))
	for k := range s.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(s.Name)
	for _, k := range keys {
		b.WriteByte('{')
		b.WriteString(strconv.Quote(k))
		b.WriteByte('=')
		b.WriteString(strconv.Quote(s.Labels[k]))
		b.WriteByte('}')
	}
	return b.String()
}
func Parse(body []byte) ([]Sample, error) {
	if len(body) > MaxBody {
		return nil, fmt.Errorf("metrics exceed 2 MiB")
	}
	// Negotiate Prometheus text. Accept the compatible OpenMetrics text subset,
	// dropping exemplars and sample timestamps (this is a live scrape, not ingest).
	var normalized bytes.Buffer
	scan := bufio.NewScanner(bytes.NewReader(body))
	scan.Buffer(make([]byte, 4096), 128<<10)
	types := map[string]string{}
	units := map[string]string{}
	lines := 0
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			f := strings.Fields(line)
			if len(f) == 4 && f[1] == "TYPE" {
				types[f[2]] = f[3]
				if f[3] == "info" || f[3] == "stateset" {
					line = "# TYPE " + f[2] + " gauge"
				}
			}
			if len(f) == 4 && f[1] == "UNIT" {
				units[f[2]] = f[3]
			}
			normalized.WriteString(line)
			normalized.WriteByte('\n')
			continue
		}
		lines++
		if lines > MaxSamples {
			return nil, fmt.Errorf("metrics exceed %d samples", MaxSamples)
		}
		// Find whitespace outside labels/quoted names; retain only metric and value.
		quoted, escaped := false, false
		depth := 0
		split := -1
		for i, r := range line {
			if escaped {
				escaped = false
				continue
			}
			if quoted && r == '\\' {
				escaped = true
				continue
			}
			if r == '"' {
				quoted = !quoted
				continue
			}
			if !quoted {
				if r == '{' {
					depth++
				}
				if r == '}' {
					depth--
				}
				if depth == 0 && (r == ' ' || r == '\t') {
					split = i
					break
				}
			}
		}
		if split < 0 {
			return nil, fmt.Errorf("invalid metric line")
		}
		value := strings.Fields(line[split:])
		if len(value) == 0 {
			return nil, fmt.Errorf("missing metric value")
		}
		normalized.WriteString(line[:split])
		normalized.WriteByte(' ')
		normalized.WriteString(value[0])
		normalized.WriteByte('\n')
	}
	if err := scan.Err(); err != nil {
		return nil, err
	}
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(&normalized)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(families))
	for n := range families {
		names = append(names, n)
	}
	sort.Strings(names)
	out := []Sample{}
	seen := map[string]bool{}
	for _, name := range names {
		family := families[name]
		for _, m := range family.Metric {
			labels := map[string]string{}
			for _, l := range m.Label {
				labels[l.GetName()] = l.GetValue()
			}
			add := func(n, kind string, v float64, extra, value string) error {
				ls := map[string]string{}
				for k, v := range labels {
					ls[k] = v
				}
				if extra != "" {
					ls[extra] = value
				}
				s := Sample{Name: n, Kind: kind, Value: v, Labels: ls, Help: family.GetHelp(), Unit: units[name]}
				// OpenMetrics counters declare the base name, then expose _total.
				if kind == "untyped" && strings.HasSuffix(n, "_total") && types[strings.TrimSuffix(n, "_total")] == "counter" {
					s.Kind = "counter"
				}
				if len(s.Help) > 2048 {
					s.Help = s.Help[:2048]
				}
				key := s.Key()
				if seen[key] {
					return fmt.Errorf("duplicate series %s", n)
				}
				seen[key] = true
				out = append(out, s)
				if len(out) > MaxSamples {
					return fmt.Errorf("too many metric samples")
				}
				return nil
			}
			switch family.GetType() {
			case dto.MetricType_COUNTER:
				err = add(name, "counter", m.GetCounter().GetValue(), "", "")
			case dto.MetricType_GAUGE:
				err = add(name, "gauge", m.GetGauge().GetValue(), "", "")
			case dto.MetricType_HISTOGRAM:
				h := m.GetHistogram()
				if len(h.Bucket) == 0 && h.GetSampleCount() > 0 {
					return nil, fmt.Errorf("native histograms require classic bucket exposition")
				}
				for _, b := range h.Bucket {
					if err = add(name+"_bucket", "histogram bucket", float64(b.GetCumulativeCount()), "le", strconv.FormatFloat(b.GetUpperBound(), 'g', -1, 64)); err != nil {
						break
					}
				}
				if err == nil {
					err = add(name+"_count", "counter", float64(h.GetSampleCount()), "", "")
				}
				if err == nil {
					err = add(name+"_sum", "counter", h.GetSampleSum(), "", "")
				}
			case dto.MetricType_SUMMARY:
				h := m.GetSummary()
				for _, q := range h.Quantile {
					if err = add(name, "summary quantile", q.GetValue(), "quantile", strconv.FormatFloat(q.GetQuantile(), 'g', -1, 64)); err != nil {
						break
					}
				}
				if err == nil {
					err = add(name+"_count", "counter", float64(h.GetSampleCount()), "", "")
				}
				if err == nil {
					err = add(name+"_sum", "counter", h.GetSampleSum(), "", "")
				}
			default:
				err = add(name, "untyped", m.GetUntyped().GetValue(), "", "")
			}
			if err != nil {
				return nil, err
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("endpoint returned no numeric metrics")
	}
	return out, nil
}
func finite(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}

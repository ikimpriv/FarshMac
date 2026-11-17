package podtoast

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"
)

type ProbeResult struct {
	Kind      string      `json:"kind"`
	Target    string      `json:"target"`
	Addresses []string    `json:"addresses,omitempty"`
	Completed int         `json:"completed,omitempty"`
	Failed    int         `json:"failed,omitempty"`
	Status    map[int]int `json:"status,omitempty"`
	ElapsedMS float64     `json:"elapsedMS"`
	P50MS     float64     `json:"p50MS,omitempty"`
	P95MS     float64     `json:"p95MS,omitempty"`
	P99MS     float64     `json:"p99MS,omitempty"`
	Errors    []string    `json:"errors,omitempty"`
}

// Probes originate from the pod network. Load is opt-in, bounded globally,
// does not follow redirects, and retains only aggregates and five errors.
func runProbe(parent context.Context, p Probe) (ProbeResult, error) {
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	start := time.Now()
	result := ProbeResult{Kind: p.Kind, Target: p.Target, Status: map[int]int{}}
	switch p.Kind {
	case "dns":
		ips, err := net.DefaultResolver.LookupHost(ctx, p.Target)
		result.Addresses = ips
		result.ElapsedMS = float64(time.Since(start).Microseconds()) / 1000
		return result, err
	case "tcp":
		c, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", p.Target)
		if c != nil {
			c.Close()
		}
		result.ElapsedMS = float64(time.Since(start).Microseconds()) / 1000
		return result, err
	case "http", "load":
	default:
		return result, fmt.Errorf("unknown probe %q", p.Kind)
	}
	u, err := url.Parse(p.Target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return result, fmt.Errorf("enter an http(s) URL without credentials")
	}
	if p.Kind == "http" {
		p.Requests, p.Concurrency, p.Rate = 1, 1, 1
	}
	if p.Requests < 1 || p.Requests > 10000 || p.Concurrency < 1 || p.Concurrency > 32 || p.Rate < 1 || p.Rate > 1000 {
		return result, fmt.Errorf("limits: 1–10000 requests, 1–32 workers, 1–1000 requests/s, 60 seconds")
	}
	transport := &http.Transport{Proxy: nil, MaxIdleConns: 32, MaxIdleConnsPerHost: 32, ResponseHeaderTimeout: 10 * time.Second, TLSHandshakeTimeout: 5 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	tasks := make(chan struct{})
	var mu sync.Mutex
	var wg sync.WaitGroup
	durations := make([]float64, 0, p.Requests)
	for i := 0; i < p.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range tasks {
				if ctx.Err() != nil {
					return
				}
				beg := time.Now()
				req, _ := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
				resp, e := client.Do(req)
				status := 0
				if resp != nil {
					status = resp.StatusCode
					n, re := io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<20+1))
					resp.Body.Close()
					if e == nil {
						e = re
					}
					if n > 8<<20 {
						e = fmt.Errorf("response exceeds 8 MiB")
					}
				}
				ms := float64(time.Since(beg).Microseconds()) / 1000
				mu.Lock()
				result.Completed++
				durations = append(durations, ms)
				if status != 0 {
					result.Status[status]++
				}
				if e != nil {
					result.Failed++
					if len(result.Errors) < 5 {
						result.Errors = append(result.Errors, e.Error())
					}
				}
				mu.Unlock()
			}
		}()
	}
	ticker := time.NewTicker(time.Second / time.Duration(p.Rate))
	defer ticker.Stop()
send:
	for i := 0; i < p.Requests; i++ {
		select {
		case <-ctx.Done():
			break send
		case <-ticker.C:
		}
		select {
		case tasks <- struct{}{}:
		case <-ctx.Done():
			break send
		}
	}
	close(tasks)
	wg.Wait()
	sort.Float64s(durations)
	if len(durations) > 0 {
		percentile := func(p float64) float64 { return durations[min(len(durations)-1, int(float64(len(durations))*p))] }
		result.P50MS = percentile(.50)
		result.P95MS = percentile(.95)
		result.P99MS = percentile(.99)
	}
	result.ElapsedMS = float64(time.Since(start).Microseconds()) / 1000
	if parent.Err() != nil {
		return result, parent.Err()
	}
	return result, nil
}

package ui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"farshmac/internal/fsops"
	"farshmac/internal/kube"
	"farshmac/internal/podtoast"
)

func (a *App) podToastBatchItems(p *Panel, refs []kube.ContainerRef) []menuItem {
	k := p.Kube
	clients := map[string]*podtoast.Client{}
	k.mu.Lock()
	for key, c := range k.toasts {
		for _, ref := range refs {
			if key == toastKey(ref) || (ref.Container == "" && strings.HasPrefix(key, toastKey(ref))) {
				clients[key] = c
			}
		}
	}
	k.mu.Unlock()
	var keys []string
	for key := range clients {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var items []menuItem
	if len(keys) > 0 {
		items = append(items, menuItem{fmt.Sprintf("PodToast snapshots · %d containers", len(keys)), "", func() {
			var jobs []contextJob
			for _, key := range keys {
				client := clients[key]
				jobs = append(jobs, contextJob{key, func(o *fsops.Operation) ([]byte, error) {
					var snapshot podtoast.Snapshot
					if err := client.JSON(o.Context(), "GET", "/v1/snapshot", nil, &snapshot); err != nil {
						return nil, err
					}
					return json.MarshalIndent(snapshot, "", "  ")
				}})
			}
			a.contextBatch(p, "PodToast snapshots", jobs, false)
		}})
	}
	networkClients := map[string]*podtoast.Client{}
	var pods []string
	for _, ref := range refs {
		name := ref.Namespace + "/" + ref.Pod
		if networkClients[name] != nil {
			continue
		}
		if c := k.podNetworkToast(ref); c != nil {
			networkClients[name] = c
			pods = append(pods, name)
		}
	}
	if len(pods) > 0 {
		items = append(items, menuItem{fmt.Sprintf("Pod network diagnostics · %d pods…", len(pods)), "", func() {
			a.form("Pod network diagnostics", []Field{
				{Label: "Probe", Toggle: true, Options: []string{"dns", "tcp", "http", "load"}, Value: "http"},
				{Label: "Target: DNS name, host:port or HTTP(S) URL", Value: "http://127.0.0.1:8080/"},
				{Label: "Load: requests per pod (1–10000)", Value: "100"},
				{Label: "Load: concurrent requests per pod (1–32)", Value: "4"},
				{Label: "Load: requests/second per pod (1–1000)", Value: "10"},
			}, func(f []Field) {
				request := podtoast.Probe{Kind: f[0].Value, Target: strings.TrimSpace(f[1].Value)}
				request.Requests, _ = strconv.Atoi(f[2].Value)
				request.Concurrency, _ = strconv.Atoi(f[3].Value)
				request.Rate, _ = strconv.Atoi(f[4].Value)
				var jobs []contextJob
				for _, name := range pods {
					client := networkClients[name]
					jobs = append(jobs, contextJob{name, func(o *fsops.Operation) ([]byte, error) {
						var result podtoast.ProbeResult
						if err := client.JSON(o.Context(), "POST", "/v1/probe", request, &result); err != nil {
							return nil, err
						}
						return json.MarshalIndent(result, "", "  ")
					}})
				}
				a.contextBatch(p, "Pod network diagnostics", jobs, false)
			})
			a.Form.Message = "Run independently in each selected pod; network namespaces are deduplicated."
		}})
	}
	return items
}

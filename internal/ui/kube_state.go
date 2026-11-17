package ui

import (
	"farshmac/internal/kube"
	"strings"
)

// Error reasons take precedence over terminal lifecycle states.
func kubeStateTone(state string) int {
	s := strings.ToLower(strings.TrimSpace(state))
	if s == "pending" || strings.HasPrefix(s, "pending ") {
		return 2
	}
	if strings.HasPrefix(s, "terminated:") {
		if strings.TrimPrefix(s, "terminated:") == "0" {
			return 1
		}
		return 2
	}
	for _, bad := range []string{"error", "failed", "failure", "crashloopbackoff", "imagepullbackoff", "errimagepull", "oomkilled", "invalidimagename", "notready", "unschedulable", "evicted", "cannotrun", "deadlineexceeded", "backofflimitexceeded"} {
		if strings.Contains(s, bad) {
			return 2
		}
	}
	for _, inactive := range []string{"terminating", "terminated", "completed", "complete", "succeeded", "deleted", "deleting", "stopped", "shutdown"} {
		if s == inactive || strings.HasPrefix(s, inactive+" ") {
			return 1
		}
	}
	return 0
}
func kubeObjectTone(o *kube.Object) int {
	tone := kubeStateTone(o.ShortStatus())
	for _, c := range o.Columns {
		if strings.EqualFold(c.Name, "status") || strings.EqualFold(c.Name, "state") || strings.EqualFold(c.Name, "reason") {
			if t := kubeStateTone(c.Value); t > tone {
				tone = t
			}
		}
	}
	if o.Pod != nil {
		for _, c := range o.Pod.Containers {
			if kubeStateTone(c.State) == 2 {
				return 2
			}
		}
	}
	if tone == 2 {
		return tone
	}
	if o.Deleting {
		return 1
	}
	return tone
}
func kubeStatusFolderEntry(name string, objects []*kube.Object) Entry {
	e := kubeFolderEntry(name, len(objects))
	tone := 1
	if len(objects) == 0 {
		tone = 0
	}
	for _, o := range objects {
		t := kubeObjectTone(o)
		if t == 2 {
			tone = 2
			break
		}
		if t == 0 {
			tone = 0
		}
	}
	e.Info.(*kubeEntry).stateTone = tone
	return e
}

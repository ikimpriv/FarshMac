package kube

import (
	"os"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// TableColumn is one printer column of the server's Table representation,
// the same columns kubectl get shows.
type TableColumn struct{ Name, Value string }

// Object is the panel's view of one resource instance: metadata and printer
// columns, never the full manifest.
type Object struct {
	GVR             schema.GroupVersionResource
	Kind            string
	Name            string
	Namespace       string
	UID             types.UID
	ResourceVersion string
	Created         time.Time
	Owners          []types.UID
	Labels          map[string]string
	Columns         []TableColumn
	Selector        map[string]string // Services: spec.selector.
}

// Column returns the value of a printer column by name (case-insensitive).
func (o *Object) Column(name string) (string, bool) {
	for _, c := range o.Columns {
		if strings.EqualFold(c.Name, name) {
			return c.Value, true
		}
	}
	return "", false
}

// ShortStatus is the one value worth a panel column: STATUS, READY or TYPE
// depending on the kind, otherwise the first column that is not the name
// or the age.
func (o *Object) ShortStatus() string {
	var preferred []string
	switch strings.ToLower(o.GVR.Resource) {
	case "pods", "namespaces", "jobs", "persistentvolumeclaims", "nodes":
		preferred = []string{"STATUS", "READY"}
	case "deployments", "statefulsets", "daemonsets", "replicasets":
		preferred = []string{"READY", "STATUS"}
	case "services", "secrets", "ingresses":
		preferred = []string{"TYPE", "CLASS"}
	case "cronjobs":
		preferred = []string{"SCHEDULE"}
	default:
		preferred = []string{"STATUS", "READY"}
	}
	for _, name := range preferred {
		if v, ok := o.Column(name); ok {
			return v
		}
	}
	for _, c := range o.Columns {
		switch strings.ToUpper(c.Name) {
		case "NAME", "AGE", "NAMESPACE":
			continue
		}
		return c.Value
	}
	return ""
}

// Ref addresses the object for API calls.
func (o *Object) Ref() Ref { return Ref{GVR: o.GVR, Namespace: o.Namespace, Name: o.Name} }

// ObjectInfo presents an object as a directory entry: the panel takes the
// name and the date from it and the object from Sys().
type ObjectInfo struct{ *Object }

func (i ObjectInfo) Name() string       { return i.Object.Name }
func (i ObjectInfo) Size() int64        { return 0 }
func (i ObjectInfo) Mode() os.FileMode  { return os.ModeDir | 0555 }
func (i ObjectInfo) ModTime() time.Time { return i.Created }
func (i ObjectInfo) IsDir() bool        { return true }
func (i ObjectInfo) Sys() any           { return i.Object }

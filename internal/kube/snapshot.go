package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// tableAccept asks the server for printer columns plus the row's metadata.
const tableAccept = "application/json;as=Table;v=v1;g=meta.k8s.io, application/json"

// SkippedResource is a type that could not be listed, with the reason.
type SkippedResource struct {
	Resource Resource
	Reason   string
}

// Snapshot is everything the panel knows about one namespace at one moment:
// objects of every listable type, indexed for the tree rules.
type Snapshot struct {
	Namespace string
	Taken     time.Time
	Objects   map[types.UID]*Object
	ByFolder  map[string][]*Object // Type folder -> objects sorted by name.
	Skipped   []SkippedResource
	// Resources maps each listed folder to its resource, and
	// ResourceVersions to the list's resourceVersion, so a watch can
	// continue from the moment the snapshot was taken.
	Resources        map[string]Resource
	ResourceVersions map[string]string
}

// resourcePath is the collection URL of a resource, cluster-wide when ns is
// empty.
func resourcePath(gvr schema.GroupVersionResource, ns string) string {
	var parts []string
	if gvr.Group == "" {
		parts = []string{"api", gvr.Version}
	} else {
		parts = []string{"apis", gvr.Group, gvr.Version}
	}
	if ns != "" {
		parts = append(parts, "namespaces", ns)
	}
	parts = append(parts, gvr.Resource)
	return "/" + strings.Join(parts, "/")
}

// listTable lists a resource in Table form; every row becomes an Object.
// The list's resourceVersion is returned for watches.
func (c *Client) listTable(ctx context.Context, r Resource, ns string) ([]*Object, string, error) {
	raw, err := c.raw.Get().AbsPath(resourcePath(r.GVR, ns)).SetHeader("Accept", tableAccept).Param("includeObject", "Metadata").Do(ctx).Raw()
	if err != nil {
		return nil, "", err
	}
	return decodeTable(raw, r, ns)
}

// decodeTable converts a Table (or, if the server ignored the Accept header,
// a plain list) into objects, with the list's resourceVersion.
func decodeTable(raw []byte, r Resource, ns string) ([]*Object, string, error) {
	var probe struct {
		Kind     string `json:"kind"`
		Metadata struct {
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, "", err
	}
	if probe.Kind != "Table" {
		objects, err := decodePlainList(raw, r, ns)
		return objects, probe.Metadata.ResourceVersion, err
	}
	var table metav1.Table
	if err := json.Unmarshal(raw, &table); err != nil {
		return nil, "", err
	}
	objects, _ := decodeTableRows(&table, r, ns, nil)
	return objects, table.ResourceVersion, nil
}

// decodeTableRows turns the rows of a Table into objects. A watch stream
// carries columnDefinitions only in its first Table; defs remembers them
// and is returned (updated when the table has its own).
func decodeTableRows(table *metav1.Table, r Resource, ns string, defs []metav1.TableColumnDefinition) ([]*Object, []metav1.TableColumnDefinition) {
	if len(table.ColumnDefinitions) > 0 {
		defs = table.ColumnDefinitions
	}
	var objects []*Object
	for _, row := range table.Rows {
		o := &Object{GVR: r.GVR, Kind: r.Kind, Namespace: ns}
		for i, col := range defs {
			if i < len(row.Cells) && row.Cells[i] != nil {
				o.Columns = append(o.Columns, TableColumn{Name: col.Name, Value: fmt.Sprint(row.Cells[i])})
			} else {
				o.Columns = append(o.Columns, TableColumn{Name: col.Name})
			}
		}
		if len(row.Object.Raw) > 0 {
			var meta metav1.PartialObjectMetadata
			if err := json.Unmarshal(row.Object.Raw, &meta); err == nil {
				applyMetadata(o, meta.ObjectMeta)
			}
		}
		if o.Name == "" {
			if v, ok := o.Column("NAME"); ok {
				o.Name = v
			}
		}
		if o.Name == "" {
			continue
		}
		objects = append(objects, o)
	}
	return objects, defs
}

func applyMetadata(o *Object, m metav1.ObjectMeta) {
	o.Name = m.Name
	o.UID = m.UID
	o.ResourceVersion = m.ResourceVersion
	if m.Namespace != "" {
		o.Namespace = m.Namespace
	}
	o.Created = m.CreationTimestamp.Time
	o.Labels = m.Labels
	o.Owners = nil
	for _, owner := range m.OwnerReferences {
		o.Owners = append(o.Owners, owner.UID)
	}
}

// plainItem is an object as a server without Table support sends it.
type plainItem struct {
	Metadata metav1.ObjectMeta `json:"metadata"`
	Status   struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

func (item plainItem) object(r Resource, ns string) *Object {
	o := &Object{GVR: r.GVR, Kind: r.Kind, Namespace: ns}
	applyMetadata(o, item.Metadata)
	if item.Status.Phase != "" {
		o.Columns = []TableColumn{{Name: "STATUS", Value: item.Status.Phase}}
	}
	return o
}

// decodePlainList handles servers (or tests) that answer a plain list.
func decodePlainList(raw []byte, r Resource, ns string) ([]*Object, error) {
	var list struct {
		Items []plainItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	var objects []*Object
	for _, item := range list.Items {
		if o := item.object(r, ns); o.Name != "" {
			objects = append(objects, o)
		}
	}
	return objects, nil
}

// NamespacesResource describes the cluster's namespaces for listing and
// watching.
var NamespacesResource = Resource{GVR: schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}, Kind: "Namespace", Singular: "namespace", Folder: "namespaces"}

// Namespaces lists the cluster's namespaces with their phase.
func (c *Client) Namespaces(ctx context.Context) ([]*Object, error) {
	objects, _, err := c.NamespacesRV(ctx)
	return objects, err
}

// NamespacesRV lists the namespaces and returns the list's resourceVersion,
// the starting point of a namespace watch.
func (c *Client) NamespacesRV(ctx context.Context) ([]*Object, string, error) {
	objects, rv, err := c.listTable(ctx, NamespacesResource, "")
	if err != nil {
		return nil, "", err
	}
	sortObjects(objects)
	return objects, rv, nil
}

func sortObjects(objects []*Object) {
	sort.SliceStable(objects, func(i, j int) bool { return objects[i].Name < objects[j].Name })
}

// LoadSnapshot lists every listable type of the namespace in parallel.
// Types that cannot be listed are recorded in Skipped; the call fails only
// when the namespace itself cannot be read or the context ends.
func (c *Client) LoadSnapshot(ctx context.Context, ns string, hidden map[string]bool) (*Snapshot, error) {
	if _, err := c.core.CoreV1().Namespaces().Get(ctx, ns, metav1.GetOptions{}); err != nil {
		return nil, err
	}
	resources, err := c.Resources(ctx)
	if err != nil {
		return nil, err
	}
	s := &Snapshot{Namespace: ns, Taken: time.Now(), Objects: map[types.UID]*Object{}, ByFolder: map[string][]*Object{}, Resources: map[string]Resource{}, ResourceVersions: map[string]string{}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, r := range resources {
		if hidden[r.Folder] {
			continue
		}
		wg.Add(1)
		go func(r Resource) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			objects, rv, err := c.listTable(ctx, r, ns)
			if err == nil && r.GVR.Group == "" && r.GVR.Resource == "services" {
				err = c.attachSelectors(ctx, ns, objects)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if ctx.Err() == nil {
					s.Skipped = append(s.Skipped, SkippedResource{Resource: r, Reason: skipReason(err)})
				}
				return
			}
			sortObjects(objects)
			s.ByFolder[r.Folder] = objects
			s.Resources[r.Folder] = r
			s.ResourceVersions[r.Folder] = rv
			for _, o := range objects {
				if o.UID != "" {
					s.Objects[o.UID] = o
				}
			}
		}(r)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(s.Skipped, func(i, j int) bool { return s.Skipped[i].Resource.Folder < s.Skipped[j].Resource.Folder })
	return s, nil
}

func skipReason(err error) string {
	switch {
	case apierrors.IsForbidden(err):
		return "forbidden"
	case apierrors.IsMethodNotSupported(err):
		return "not listable"
	case apierrors.IsNotFound(err):
		return "not found"
	}
	return err.Error()
}

// attachSelectors reads the services as full objects, the only type whose
// spec the tree needs (selector children, rule 3 of 5.2).
func (c *Client) attachSelectors(ctx context.Context, ns string, services []*Object) error {
	raw, err := c.raw.Get().AbsPath(resourcePath(schema.GroupVersionResource{Version: "v1", Resource: "services"}, ns)).Do(ctx).Raw()
	if err != nil {
		return err
	}
	var list struct {
		Items []struct {
			Metadata metav1.ObjectMeta `json:"metadata"`
			Spec     struct {
				Selector map[string]string `json:"selector"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return err
	}
	byName := map[string]*Object{}
	for _, o := range services {
		byName[o.Name] = o
	}
	for _, item := range list.Items {
		if o, ok := byName[item.Metadata.Name]; ok {
			o.Selector = item.Spec.Selector
			if o.UID == "" {
				applyMetadata(o, item.Metadata)
			}
		}
	}
	return nil
}

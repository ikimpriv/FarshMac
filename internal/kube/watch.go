package kube

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// EventType is the kind of change a watch reports.
type EventType string

const (
	Added    EventType = "ADDED"
	Modified EventType = "MODIFIED"
	Deleted  EventType = "DELETED"
)

// Event is one change of an object in a type folder.
type Event struct {
	Type   EventType
	Folder string
	Object *Object
}

// ErrWatchExpired reports that the server no longer has the history the
// watch asked for (410 Gone): the caller lists again and restarts from the
// new resourceVersion.
var ErrWatchExpired = errors.New("watch expired: list again")

// WatchFolder streams the changes of one resource type after resourceVersion,
// in Table form so events carry the same printer columns as a snapshot
// (kubectl get -w). ns is "" for cluster-scoped resources such as namespaces.
// The channel closes when ctx ends or the watch fails for good; the returned
// func waits for that and reports the error: nil after cancellation,
// ErrWatchExpired after 410, otherwise the server's error. Network drops are
// retried from the last seen resourceVersion with a backoff up to 10 s.
func (c *Client) WatchFolder(ctx context.Context, r Resource, ns, resourceVersion string) (<-chan Event, func() error) {
	events := make(chan Event, 64)
	done := make(chan struct{})
	var result error
	go func() {
		defer close(done)
		defer close(events)
		result = c.watchLoop(ctx, r, ns, resourceVersion, events)
	}()
	return events, func() error { <-done; return result }
}

func (c *Client) watchLoop(ctx context.Context, r Resource, ns, rv string, events chan<- Event) error {
	backoff := 500 * time.Millisecond
	var defs []metav1.TableColumnDefinition
	for {
		next, err := c.watchOnce(ctx, r, ns, rv, &defs, events)
		if next != "" {
			rv = next
		}
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			if errors.Is(err, ErrWatchExpired) || isTerminalStatus(err) {
				return err
			}
		}
		// A closed stream (server timeout) or a network error: reconnect.
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil
		}
		if backoff *= 2; backoff > 10*time.Second {
			backoff = 10 * time.Second
		}
	}
}

// isTerminalStatus tells API status errors that retrying cannot fix
// (forbidden, not found, bad request) from transport failures.
func isTerminalStatus(err error) bool {
	var status apierrors.APIStatus
	if !errors.As(err, &status) {
		return false
	}
	code := status.Status().Code
	return code >= 400 && code < 500
}

func expired(err error) bool {
	return apierrors.IsGone(err) || apierrors.IsResourceExpired(err)
}

// watchOnce runs one watch request until the stream ends; it returns the
// last resourceVersion seen, so the next request continues from there.
func (c *Client) watchOnce(ctx context.Context, r Resource, ns, rv string, defs *[]metav1.TableColumnDefinition, events chan<- Event) (string, error) {
	req := c.raw.Get().AbsPath(resourcePath(r.GVR, ns)).
		SetHeader("Accept", tableAccept).
		Param("watch", "true").Param("allowWatchBookmarks", "true").Param("includeObject", "Metadata")
	if rv != "" {
		req = req.Param("resourceVersion", rv)
	}
	body, err := req.Stream(ctx)
	if err != nil {
		if expired(err) {
			return "", ErrWatchExpired
		}
		return "", err
	}
	defer body.Close()
	dec := json.NewDecoder(body)
	last := ""
	for {
		var ev metav1.WatchEvent
		if err := dec.Decode(&ev); err != nil {
			if err == io.EOF {
				return last, nil
			}
			return last, err
		}
		switch ev.Type {
		case "ERROR":
			var status metav1.Status
			if json.Unmarshal(ev.Object.Raw, &status) == nil && (status.Code == 410 || status.Reason == metav1.StatusReasonGone || status.Reason == metav1.StatusReasonExpired) {
				return last, ErrWatchExpired
			}
			return last, errors.New("watch error: " + string(ev.Object.Raw))
		case "BOOKMARK":
			if v := rawResourceVersion(ev.Object.Raw); v != "" {
				last = v
			}
			continue
		}
		objects, v, err := decodeWatchObject(ev.Object.Raw, r, ns, defs)
		if err != nil {
			return last, err
		}
		if v != "" {
			last = v
		}
		for _, o := range objects {
			select {
			case events <- Event{Type: EventType(ev.Type), Folder: r.Folder, Object: o}:
			case <-ctx.Done():
				return last, nil
			}
		}
	}
}

// rawResourceVersion reads metadata.resourceVersion of any object.
func rawResourceVersion(raw []byte) string {
	var probe struct {
		Metadata struct {
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
	}
	json.Unmarshal(raw, &probe)
	return probe.Metadata.ResourceVersion
}

// decodeWatchObject decodes the object of a watch event: a Table with one
// row (and columnDefinitions in the first event only), or a plain object
// when the server ignored the Accept header.
func decodeWatchObject(raw []byte, r Resource, ns string, defs *[]metav1.TableColumnDefinition) ([]*Object, string, error) {
	var probe struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, "", err
	}
	if probe.Kind == "Table" {
		var table metav1.Table
		if err := json.Unmarshal(raw, &table); err != nil {
			return nil, "", err
		}
		objects, next := decodeTableRows(&table, r, ns, *defs)
		*defs = next
		rv := table.ResourceVersion
		for _, o := range objects {
			if o.ResourceVersion != "" {
				rv = o.ResourceVersion
			}
		}
		return objects, rv, nil
	}
	var item plainItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, "", err
	}
	o := item.object(r, ns)
	if o.Name == "" {
		return nil, o.ResourceVersion, nil
	}
	return []*Object{o}, o.ResourceVersion, nil
}

// Apply returns a snapshot with one event applied. The receiver is never
// changed, so readers holding it keep a consistent view.
func (s *Snapshot) Apply(ev Event) *Snapshot {
	if ev.Object == nil {
		return s
	}
	n := &Snapshot{Namespace: s.Namespace, Taken: time.Now(), Objects: make(map[types.UID]*Object, len(s.Objects)+1), ByFolder: make(map[string][]*Object, len(s.ByFolder)+1), Skipped: s.Skipped, Resources: s.Resources, ResourceVersions: make(map[string]string, len(s.ResourceVersions)+1)}
	for k, v := range s.Objects {
		n.Objects[k] = v
	}
	for k, v := range s.ByFolder {
		n.ByFolder[k] = v
	}
	for k, v := range s.ResourceVersions {
		n.ResourceVersions[k] = v
	}
	if ev.Object.ResourceVersion != "" {
		n.ResourceVersions[ev.Folder] = ev.Object.ResourceVersion
	}
	same := func(o *Object) bool {
		if ev.Object.UID != "" && o.UID != "" {
			return o.UID == ev.Object.UID
		}
		return o.Name == ev.Object.Name
	}
	old := s.ByFolder[ev.Folder]
	folder := make([]*Object, 0, len(old)+1)
	for _, o := range old {
		if same(o) {
			if o.UID != "" {
				delete(n.Objects, o.UID)
			}
			continue
		}
		folder = append(folder, o)
	}
	if ev.Type != Deleted {
		obj := ev.Object
		// A Table row carries metadata only: keep what the list knew.
		for _, o := range old {
			if same(o) {
				if obj.Selector == nil {
					obj.Selector = o.Selector
				}
				if len(obj.Columns) == 0 {
					obj.Columns = o.Columns
				}
				break
			}
		}
		folder = append(folder, obj)
		sort.SliceStable(folder, func(i, j int) bool { return folder[i].Name < folder[j].Name })
		if obj.UID != "" {
			n.Objects[obj.UID] = obj
		}
	}
	if len(folder) == 0 {
		delete(n.ByFolder, ev.Folder)
	} else {
		n.ByFolder[ev.Folder] = folder
	}
	return n
}

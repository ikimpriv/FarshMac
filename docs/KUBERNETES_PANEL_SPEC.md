# Kubernetes panel: requirements, gap analysis, implementation specification

Status: phases 0–2 are implemented on the `kubernetes-panel` branch (2026-11-13); phase 3 (apply when copying into a cluster, scale/restart/port-forward, restoring from the session, Info panel, Quick view, `du`) has not been started. Differences between the implementation and the text below: `Panel` keeps the `Remote`/`Archive` fields and gains `Kube`, and `panelBackend` is computed from them (`panel_backend.go`); `remotefs.Endpoint{Opener}` is implemented; `documentSource` is implemented for server and container files (`remoteSource`) and for manifests (`manifestSource`); instead of `terminal.Stream`, `App` has `Foreign foreignStream` (`foreign.go`) for exec and follow-logs; when type names collide, built-in types retain `plural` and extensions use `plural.group`; Shift+F4 inside a container filesystem remains "new file"; objects without child objects are shown as files (Enter = manifest), not as empty folders; the panel columns are Kind / Status / Age (Detailed: creation time); after a namespace is listed, its types are observed through a Table watch (`kube.WatchFolder`, `Snapshot.Apply`), the snapshot is updated in memory and the panel re-reads itself, the TTL remains a fallback; after a panel command finishes in a container, the panels return automatically and Cmd+O resumes the same shell with its output; Quick view (Ctrl+Q) next to a Kubernetes panel shows the describe of the current object with highlighting (moved forward from phase 3); apply when copying into a namespace (phase 3, item 1) is implemented in `kube_apply.go`: the target namespace is always substituted, without Override/Skip questions, a kind mismatch with the type folder is not checked, on an apply error — Retry/Skip/Cancel through `Operation.Attempt`, there is a Force checkbox. Tests: `internal/kube` (fake API in `internal/kube/kubetest`, local `sh` in `internal/kube/localexec`), `internal/ui/kube_*_test.go`.

Adopted directions (revision 3):
- Backend — **client-go**, no `kubectl`.
- Panel modes are moved to a **strategy pattern** (the `panelBackend` interface) with a `backendKind` enum for cheap checks. This is a separate preparatory phase with no behavior change.
- The key bar **reacts to holding Shift/Ctrl/Alt/Cmd**, as in Far Manager, for all panel modes.
- The purpose is a **dev tool for quick access to objects**, not a universal cluster maintenance tool. Cluster-scoped objects (Node, PV, ClusterRole, CRD) are not shown in the panel.
- Tree: `namespace > <resource type> > object > <child resource type> > object > … > pod > container`. Intermediate owners (ReplicaSet, ControllerRevision) are **collapsed** by hard-coded rules so that the tree stays shallow.
- Describe — through `k8s.io/kubectl/pkg/describe`.

## 1. Requirements (as formulated)

1. **Commands → Network connections** gets a **New Kubernetes cluster…** item: a kubeconfig path is given and a context is selected. The profile is saved and shown in the connection list.
2. Kubernetes is shown in the panel as a file system:
   - the root is namespaces;
   - inside a namespace — folders by resource type (`deployments`, `services`, `configmaps`, CRD types, …);
   - inside a type folder — objects of that type;
   - inside an object — folders of child types with the objects it owns, where inconvenient intermediate levels (ReplicaSet) are skipped: `deployments > app > pods > app-7c9d-x > container`.
3. As you move between levels, the bottom key bar and the available actions change.
4. F5 on an object copies its manifest. F5 of a manifest (a local file) into a namespace creates the object.
5. Entering a Pod shows the list of containers (if there are several). Entering a container (or a single-container Pod) shows the container's file system.
6. A "Logs" menu command for a Pod or container.
7. Copying files from a container and into a container.
8. Inside a container, a command from the command line is executed in the Pod (if permitted).

## 2. Checking the idea for consistency

| # | Problem | Solution |
|---|----------|---------|
| C1 | An object may have several owners, an owner in another namespace, or a cluster-scoped owner (Node). | The type folder at the namespace level shows **all** objects of the type regardless of ownership (like `kubectl get <type>`). Inside an object only child objects by the rules of 5.2 are shown. One Pod is visible both in `ns/pods/` and in `ns/deployments/app/pods/`: this is a view, not storage. |
| C2 | Entry names in a folder must be unique and must not contain `/`. | In a type folder all objects belong to one GVR, names are unique by definition, so the entry = object name without a suffix. The type folder is named after the plural resource name (`deployments`); if one resource name exists in several groups, the folder is named `deployments.<group>`. Parsing names back is not required: the snapshot keeps a "path → object" map. |
| C3 | Single-container Pod: on entering, the FS is shown immediately, but then there is nowhere to show Pod-level actions. | The "FS immediately" rule applies only when the Pod has exactly one regular container and no init/ephemeral containers. Otherwise a container list is shown with the marks ` (init)`, ` (ephemeral)`. Logs and the manifest are available on the Pod entry in the parent folder. The virtual path is unambiguous: after the Pod segment there is either always a container or always an FS path (the snapshot decides). |
| C4 | F5 has three meanings: the object manifest, container files, applying a manifest. | The meaning is determined by the **source level** and the **destination level** (table in 5.3). A container entry cannot be copied. A local file cannot be copied into a container list. |
| C5 | A Pod not in the Running state has no accessible FS. | Entering shows an error in the panel; the other actions (manifest, logs, describe, delete) work. |
| C6 | A container may have no `sh`/`tar` (distroless). | On first entry a probe is run (`/bin/sh`, `/bin/bash`, presence of `tar`, the `find`/`stat`/`ls` variant). The result is cached by (pod uid, container). Without a shell: the FS and commands are unavailable with an explicit error. Without `tar`: trees are unavailable, single files go through `cat`. An ephemeral debug container is phase 3. |
| C7 | F6 (rename) makes no sense for Kubernetes objects. | At the object level F6 is disabled. In the container FS F6 = `mv` within the same container. |
| C8 | F8 on a namespace destroys all of its contents. | Deleting a namespace requires typing its name. Deleting objects shows kind/name and the cascade. Shift+F8 on an object = `PropagationPolicy: Orphan`. |
| C9 | Syncing the panel folder with the terminal (Cmd+O) is not applicable. | The Kubernetes panel behaves like Remote: the local shell stays in the last local folder. |
| C10 | A command "in the Pod" and the terminal. | With client-go an interactive exec is a SPDY/WebSocket stream with a TTY. The terminal area gets a `terminal.Stream` abstraction: while a command runs in the container, output goes to xterm, input and resize go to the exec stream; the local shell idles. This also gives "Open shell in container" as a real interactive shell. Panel commands share a persistent shell with Cmd+O. The cwd is taken from the panel, and the shell environment is preserved. A session-specific prompt marker triggers automatic return to the panels after a command. |
| C11 | Editing a manifest (F4) and concurrent changes. | Saving = `Update` with the `resourceVersion` of the loaded object. `errors.IsConflict` is shown as an error; the buffer and the modified flag are kept (consistent with TODO.md about save errors). |
| C12 | Copying Secret manifests to disk writes secrets in the clear (base64). | If the selection contains a Secret, an additional confirmation. |
| C13 | Exec authentication plugins (`gke-gcloud-auth-plugin`, `aws`, `kubelogin`) are launched by client-go via the process PATH, and the PATH of a GUI application launched from Finder is minimal. | On the first access to Kubernetes the login-shell PATH is requested once (`$SHELL -lc 'printf %s "$PATH"'`, 5 s timeout) and applied to the process via `os.Setenv`. The profile has an optional "Extra PATH" field. A plugin error is shown together with its stderr. |
| C14 | Cluster-scoped objects (Node, PV, ClusterRole, CRD) do not fit into "root = namespaces". | Not shown. The tool is oriented toward application objects, not cluster maintenance. A developer rarely needs Node; for a Pod the node name is shown in the Info panel and in Describe. |
| C15 | A type folder inside an object and a container name inside a Pod are different levels, but both follow the object name. | Inside a Pod no type folders are shown (a Pod has no interesting child objects), so the segment after the Pod is always a container or an FS path (C3). Inside the other objects — only type folders. |
| C16 | Tree depth. The chains `Deployment → ReplicaSet → Pod` and `StatefulSet → ControllerRevision` make the tree one level deeper with no benefit. | A hard-coded list of **transparent** types through which ownership passes straight through: `replicasets`, `controllerrevisions`. They remain available in `ns/replicasets/`, but they are absent inside a Deployment. The list is extensible in code, not in settings (5.2). |

Conclusion: the idea is consistent provided rules C2–C4 are followed. The main work is not the Kubernetes UI but the preparatory refactoring of panel modes (section 4) and the key bar (section 6).

## 3. Gap analysis against the current code

| # | What exists | What is missing | Solution |
|---|----------|-----------------|---------|
| G1 | There are no Kubernetes dependencies; `go.mod` contains 7 direct modules. | API client, exec/stream, YAML. | **client-go** (`k8s.io/client-go`, `k8s.io/apimachinery`, `k8s.io/api`) at one consistent `v0.3x` version matching current clusters, pinned to an exact version. YAML — `sigs.k8s.io/yaml` and `k8s.io/apimachinery/pkg/util/yaml` (come transitively). Cost: `go.sum` grows by roughly 150 modules, the binary by roughly 25–35 MB, the first build takes longer; `THIRD_PARTY_NOTICES.txt` is extended with Apache-2.0 (Kubernetes, gogo/protobuf, gnostic, …) and the BSD/MIT of the rest. This is a deliberate price for not having an external binary. |
| G2 | `Panel` has two modes via the fields `Remote *remotefs.Config` and `Archive *fsops.ArchiveIndex`; 37 places in `internal/ui` check `Remote != nil`/`== nil`, and a number of other places check `Archive != nil`. | A third mode without multiplying conditions. | Strategy pattern: the `panelBackend` interface with four implementations (local, archive, remote, kube) and the `backendKind` enum. Details in section 4. First a refactoring of the existing modes with no behavior change, then Kubernetes. |
| G3 | `remotefs.FS` is the remote FS interface; `remotefs.Copy`/`Move`/`RemoveTree` work through it with progress, collisions and cancellation. `Endpoint{Remote *Config}`. | Opening a container FS. | `kube.ContainerFS` implements `remotefs.FS` on top of `remotecommand` exec. `Endpoint` is generalized to `{Opener}` (4.4). |
| G4 | The listing is `[]Entry{Name, Info os.FileInfo, Dir}`; drawing takes the size and date from `Info`, special columns through `Info.Sys()`. | Object status and age. | `kube.ObjectInfo` implements `os.FileInfo`; `Sys()` returns `*kube.Object`. The status column is taken from the server-side Table representation (5.2), which gives the same columns as `kubectl get`, including CRDs with `additionalPrinterColumns`. |
| G5 | The key bar is static (`draw.go:39`); it only changes F8 for Remote; it does not react to held modifiers. The renderer does not report holding. | Context-sensitive labels by mode, level and modifiers. | Section 6. |
| G6 | `menuSections()` is dynamic. | A "Kubernetes" section in F9. | The section is taken from `panelBackend.MenuSection()`. |
| G7 | Profiles are `[]remotefs.Config` in `connections.json`; older versions will not survive records of another type. | Storing cluster profiles. | A separate file `kubernetes.json` (`[]kube.Profile`), the field `Settings.KubeProfiles`. No secrets. |
| G8 | The `a.form` form has no drop-down lists. | Context selection. | Two steps: form (kubeconfig, name) → `a.choose` with the list of contexts from `clientcmd.LoadFromFile`. The current context is preselected. |
| G9 | `Document` is static. | Logs with follow. | Static logs (tail N) — in the viewer. Follow — through `terminal.Stream` in the terminal area (like exec, C10): `GetLogs(...).Stream(ctx)` with `Follow: true`, Ctrl+C cancels the context. |
| G10 | `execute()` always sends the command to the local shell. | Execution in a container. | A branch through `panelBackend.Execute(a, cmd) bool`: the kube backend at the container FS level starts a `terminal.Stream` exec (C10). The typed text goes into the history. The prompt is `pod:<ns>/<pod>/<ctr>:<dir>>`. |
| G11 | The session saves only the local path; Remote panels are not restored. | Restoring a Kubernetes panel. | MVP as Remote. Restoring by profile is phase 3; it requires no secrets. |
| G12 | `openRemoteDocument`/`saveRemoteDocument` are tied to `remotefs.Config.Dial`. | Viewing/editing container files and manifests. | `documentSource` with `Fetch(ctx, o) (localPath, info)` and `Store(ctx, data)`; implementations remote, container, manifest. Provided through `panelBackend.DocumentSource`. |
| G13 | The terminal area is hard-wired to `terminal.Session` (local PTY): `App.Term`, `a.pending`, `Handle("input")`. | A second byte source and input sink. | The `terminal.Stream` interface `{Write([]byte) error; Resize(cols, rows) error; Events() <-chan Event; Close()}`; `Session` implements it; `kube.ExecStream` implements it on top of `remotecommand.StreamWithContext` with a `TerminalSizeQueue`. `App.Foreign terminal.Stream`, while a remote stream is active, intercepts input and resize and hands control back to the local shell when it finishes. |
| G14 | Cancellation through `fsops.Operation.Context()`. | — | All client-go calls take `o.Context()`. |
| G15 | Tests use loopback servers (`internal/testnet`). | A test API server. | An `httptest.Server` with a hand-written router for discovery, LIST (Table and JSON), GET/UPDATE/DELETE/PATCH, logs; exec — a test SPDY/WebSocket server on top of `k8s.io/apimachinery/pkg/util/httpstream`, or injection of an `Executor` interface with a local `sh`. A real cluster — only opt-in `FARSHMAC_KUBE_CHECK` (by analogy with `FARSHMAC_SFTP_CHECK`). |

## 4. Panel mode refactoring (phase 0)

### 4.1 Goal

Remove the scattered `Remote != nil` and `Archive != nil` checks and give the Kubernetes mode a single attachment point. The behavior of local, archive and remote does not change after phase 0; existing tests pass without changing expectations.

### 4.2 Interface

```go
type backendKind uint8
const (
    backendLocal backendKind = iota
    backendArchive
    backendRemote
    backendKube
)

type panelCaps uint32
const (
    capEdit panelCaps = 1 << iota   // F4 and saving
    capMkdir                         // F7
    capDelete                        // F8
    capTrash                         // F8 = Trash (otherwise Delete)
    capRename                        // F6
    capCopyIn                        // the panel can be an F5 destination
    capFolderSize                    // F3 on a folder, Cmd+Option+F3
    capQuickView                     // Ctrl+Q on the current item
    capArchiveEnter                  // Enter on an archive
    capShellSync                     // Cmd+O syncs the cwd
    capDescriptions                  // files.bbs and the like
    capExecute                       // a command line command is executed by the backend
)

type panelBackend interface {
    Kind() backendKind
    Label() string                                   // panel title prefix ("", "archive:", "sftp://…", "k8s:ctx")
    Load(p *Panel, path string, o *fsops.Operation) ([]Entry, loadMeta, error)
    FilesystemPath(p *Panel) string                  // local folder for the terminal and relative paths
    Caps(p *Panel, path string) panelCaps            // may depend on the level inside the backend
    KeyBar(p *Panel, mods modifiers) [10]string      // section 6
    Endpoint(p *Panel) remotefs.Endpoint             // for F5/F6 through remotefs.Copy / Move
    DocumentSource(p *Panel, path string) documentSource
    Delete(a *App, paths []string, permanent bool)
    Mkdir(a *App)
    Execute(a *App, cmd string) bool                 // true = handled; false = local shell
    MenuSection() *menuSection                       // nil if there is no section
    SessionState(p *Panel) SessionPanel              // what to save in the session
    Close()                                          // disconnect
}
```

`loadMeta` carries what `loadRemote`/`loadArchiveDirectory` currently write directly into panel fields: `InfoText`, `TotalBytes`, `ItemCount`, `Descriptions`.

### 4.3 What moves

| Now | Where to |
|--------|------|
| `Panel.Remote`, `Panel.RemoteLocalPath`, `Panel.Archive` | `Panel.backend panelBackend` (`localBackend{}` by default), `remoteBackend{config, localPath}`, `archiveBackend{index, localPath}` |
| `Panel.Load` branching | `p.backend.Load(...)` plus the shared code `beginLoad/sortEntries/installEntries` |
| `filesystemPath()` | `p.backend.FilesystemPath(p)` |
| `function(n)` branches by Remote/Archive | `caps` and backend methods |
| `transferDialog` conditions | `Caps(...)&capCopyIn`, `Endpoint()` |
| `deleteSelected`, `remoteDelete`, `archiveReadOnly` | `Delete`, `capDelete`, `capTrash` |
| `openConfigured` branches | `DocumentSource` |
| `draw.go` F8 labels and the `local:` prefix | `KeyBar`, `Label` |
| `syncCwd`, `showTerminal`, `finishPanelReturn` checks of `Remote == nil` | `capShellSync` |
| `image_preview.go`, `folder_size_cache.go`, `info_panel.go`, `panel_options.go`, `folder_shortcuts.go` | `capQuickView`, `capFolderSize`, `Kind()` where the exact type matters |
| `session.go` saving | `SessionState` |

The `Kind()` enum may be used only where type identity itself is needed (for example, tests and messages), not behavior. Review rule: a new condition of the form `Kind() == backendX` in action code is considered a smell; a cap or a method is added instead.

### 4.4 `remotefs.Endpoint`

```go
type Opener interface {
    Open(ctx context.Context) (FS, error)
    Identity() string      // for Same(): the same server/container
}
type Endpoint struct{ Opener Opener } // nil = local FS
```

`*Config` implements `Opener`. The kube container implements it in the `kube` package, importing `remotefs` (not the other way around).

### 4.5 Phase 0 verification

- `grep -n "Remote != nil\|Remote == nil\|Archive != nil" internal/ui/*.go` outside `*_test.go` and the backend files yields zero.
- `./scripts/check.sh --race` is green; `remote_test.go`, `archive_panel_test.go`, `transfer_collision_test.go`, `far_behavior_test.go` do not change their expectations.
- Manual check: SFTP panel, archive panel, copying in both directions, session.

## 5. Kubernetes mode

### 5.1 The `internal/kube` package

```
internal/kube/
  profile.go      Profile{Name, Kubeconfig, Context, ExtraPath}; Label() = "k8s:"+Context
  client.go       Client{rest *rest.Config; dyn dynamic.Interface; core kubernetes.Interface (CoreV1 for logs/exec);
                         disc discovery.CachedDiscoveryInterface; mapper meta.RESTMapper}
                  Connect(ctx, Profile) — clientcmd.NewNonInteractiveDeferredLoadingClientConfig with
                  ConfigOverrides{CurrentContext}; import _ "k8s.io/client-go/plugin/pkg/client/auth" for auth providers;
                  QPS/Burst raised (kubectl-like: 50/300); Timeout from settings
  contexts.go     Contexts(kubeconfig) ([]string, current string, error) through clientcmd.LoadFromFile
  discovery.go    Resources(ctx) ([]Resource, error) — ServerPreferredNamespacedResources, cached per session;
                  filter by the list/get verbs; Resource{GVR, Kind, Singular, Namespaced, Verbs}
  snapshot.go     Snapshot{Namespace, Objects map[types.UID]*Object, ByType map[GVR][]*Object, children map[UID][]*Object,
                           Taken time.Time, Skipped []SkippedResource}
  tree.go         transparent = {replicasets.apps, controllerrevisions.apps}; selectorChildren = {services → pods};
                  Children(obj) map[GVR][]*Object by the rules of 5.2; TypeFolders(ns) with priority order
                  LoadSnapshot(ctx, c, ns, set ResourceSet) — parallel LISTs (semaphore 8) in Table format
                  (Accept: application/json;as=Table;v=v1;g=meta.k8s.io, includeObject=Metadata):
                  rows contain printer columns and PartialObjectMetadata with ownerReferences and labels; forbidden → Skipped;
                  services are additionally LISTed as full objects for the sake of spec.selector
  object.go       Object{GVR, Kind, Name, Namespace, UID, Created, Owners []UID, Labels, Columns []string, Status string}
                  ShortStatus() picks the STATUS/READY/TYPE column by kind, otherwise the first non-NAME/AGE column
  pod.go          PodSummary(ctx, ns, name) — containers (regular/init/ephemeral), states, phase
  manifest.go     Get(ctx, ref) (*unstructured.Unstructured, error); ExportYAML(obj, clean bool) ([]byte, error)
                  (clean: managedFields, status, uid, resourceVersion, creationTimestamp, generation,
                  ownerReferences when copying into another namespace/cluster);
                  Decode(data) ([]*unstructured.Unstructured, error) — YAMLOrJSONDecoder, multi-document;
                  Apply(ctx, obj, ns) — server-side apply, FieldManager "farshmac", Force=false;
                  Update(ctx, obj) — with resourceVersion; Delete(ctx, refs, propagation)
  describe.go     Describe(ctx, ref) (string, error) — k8s.io/kubectl/pkg/describe (the k8s.io/kubectl module, decision made)
  logs.go         Logs(ctx, ref, opts) (io.ReadCloser, error) — corev1 GetLogs(...).Stream
  exec.go         Executor interface { Stream(ctx, ExecOptions) error }; remotecommand implementation
                  (NewWebSocketExecutor with FallbackExecutor to SPDY, as in kubectl)
  containerfs.go  ContainerRef{Profile, Namespace, Pod, Container}; ContainerFS implements remotefs.FS;
                  probe (shell, tar, lister); ReadDir through `find -maxdepth 1 -printf` → `stat -c` → `ls -lan`;
                  Open = `cat`, Create = `cat > path` through stdin; Remove = rm/rmdir; Rename = mv; Chmod/Chtimes
  execstream.go   ExecStream implements terminal.Stream: TTY, stdin from xterm, resize through TerminalSizeQueue
```

All methods take a `context.Context`. Data from the server is never interpreted as commands.

### 5.2 Virtual path and snapshot

`Panel.Path` is an absolute path:

```
/                                        namespaces
/<ns>                                    resource type folders
/<ns>/<type>                             objects of the type (all, like kubectl get)
/<ns>/<type>/<name>                      child type folders (see the ownership rules)
/<ns>/<type>/<name>/<childtype>/<name>/… recursively
/<ns>/…/pods/<pod>/<container>           only if containers > 1 (C3)
/<ns>/…/pods/<pod>[/<container>]/<abs path in container>
```

Levels (`kubeLevel`): `root`, `namespace`, `typeFolder`, `object`, `containers`, `containerFS`. A child type folder inside an object is the same `typeFolder` level, but with a parent.

**Ownership rules (hard-coded, `internal/kube/tree.go`):**

1. The child objects of object X are the snapshot objects that have X in `ownerReferences`.
2. If a child object has a **transparent** type (`replicasets`, `controllerrevisions`), it is not shown; its child objects are shown instead (recursively). Thus `deployments/app/pods/` contains the Pods of all ReplicaSets, including old ones after a rollout; old Pods differ by the status column.
3. **Selector children**: `services/<svc>/pods/` — Pods whose labels satisfy the service's `spec.selector`. This is not ownership; it is marked in the panel's InfoText as "matched by selector". For this, Services are loaded with `spec` (one LIST of full objects, they are small), Pods — metadata only (labels are present in `PartialObjectMetadata`).
4. Everything else, including CRDs, uses only rules 1–2. Example: Argo `rollouts/app/pods/` is obtained automatically through the transparent ReplicaSet; `cronjobs/backup/jobs/<job>/pods/` — two levels, both useful.
5. Inside a Pod there are no type folders (C15): containers or FS.
6. An object without children is an empty folder (entering shows an empty list, not an error).

Extending rules 2–3 is a code change with a test, not a setting: this is deliberate, so that the tree is the same for everyone.

**Type folders at the namespace level.** Types that have at least one object in the snapshot are shown; the `KubeShowEmptyTypes` setting (off) shows all. The "size" column of a type folder = number of objects, the date is empty. The "Unsorted" order (Ctrl+F7) is a priority list: `deployments, statefulsets, daemonsets, cronjobs, jobs, pods, services, ingresses, configmaps, secrets, persistentvolumeclaims, serviceaccounts`, then the rest alphabetically; sorting by name is pure alphabetical.

**Snapshot.** The namespace snapshot is cached in the backend (`snapshots[ns]`), refreshed on Ctrl+R, on re-entering the namespace and by TTL (`KubeSnapshotTTL`, 30 s). Loading goes through the asynchronous `loadPanel`; `Esc` cancels the context of all LISTs. The resource set: all namespaced resources with the `list` verb, except subresources and `*.metrics.k8s.io` (these are not objects). `events` is an ordinary type folder: in a structure with folders by type, events get in nobody's way, and a developer sometimes needs them. The `KubeHiddenTypes` setting (empty by default) allows hiding noisy types of a specific cluster. Types with Forbidden are silently skipped; their number is shown in InfoText ("3 resource types skipped: forbidden").

Entries:

| Level | Name | Dir | Date | Size field |
|---------|-----|-----|------|--------------|
| root | namespace name | yes | creation | STATUS (Active/Terminating) |
| namespace | type name (`deployments`) | yes | — | number of objects |
| typeFolder | object name | yes | creation | Table column: Deployment `READY 2/3`, Pod `Running`, Job `Complete`, Service `ClusterIP`, CRD — the first printer column after NAME |
| object | child type name (`pods`) | yes | — | number of objects |
| containers | name [` (init)`/` (ephemeral)`] | yes | start time | `running` / `waiting:CrashLoopBackOff` / `terminated:0` |
| containerFS | as in Remote | by mode | mtime | size |

Panel masks apply to entry names at any level.

### 5.3 Actions by level

| Key | root | namespace | typeFolder (objects) | object (child type folders) | containers | containerFS |
|---------|------|-----------|----------------------|-------------------------------|------------|-------------|
| Enter / Ctrl+PgDn | enter | enter | enter (Pod per C3) | enter | enter the FS | enter |
| F3 | namespace manifest | — | object manifest (yaml, read-only) | — | Pod manifest | view file |
| F4 | edit manifest | — | edit manifest (C11) | — | — | edit file |
| F5 | export namespace (all type folders → yaml) | export type folder (all manifests) | object manifest → local/k8s | export child type folder | forbidden | files → local/container |
| F6 | — | — | — (C7) | — | — | `mv` |
| F7 | new namespace | — | — | — | — | `mkdir` |
| F8 | delete namespace (type the name) | — ("select objects inside") | delete (confirmation, Background cascade) | — | — | `rm -r` |
| Shift+F8 | — | — | delete with Orphan | — | — | as F8 |
| Shift+F3 | — | — | logs (Pod; for a Pod owner — choose a Pod from `pods/`) | — | container logs | — |
| Shift+F4 | describe | — | describe | — | describe Pod | — |
| Alt+F3 | — | — | follow logs | — | follow logs | — |
| Ctrl+R | refresh | refresh snapshot | refresh | refresh | refresh | refresh |

Export rule (F5 from Kube to local): a folder = recursive export of its contents as `yaml` files (`namespace/` → `deployments/app.yaml`, `services/web.yaml`, …); an **object** is exported only as its own manifest `<name>.yaml`, without child objects, because child objects are derived controller state. The file name `<name>.yaml` is unambiguous: the selection is always within one type folder. Types from `KubeHiddenTypes` and `events` are not included in the recursive export.

Copying into a Kube panel (destination):

| Destination | Source: local files | Source: k8s objects | Source: container files |
|------------|----------------------|----------------------|---------------------------|
| root | forbidden | forbidden | forbidden |
| namespace | server-side apply into this namespace; namespace in the file ≠ target → Override/Skip/Cancel; object exists → Apply/Skip/Cancel (through `fsops.Operation.Collision`) | the same, clean export, `ownerReferences` are stripped with a warning | forbidden |
| typeFolder | as namespace; manifest kind ≠ folder type → "Apply anyway / Skip" | as namespace | forbidden |
| object / child typeFolder | as typeFolder (the namespace is taken from the path) | as namespace | forbidden |
| containers | forbidden | forbidden | forbidden |
| containerFS | upload through `remotefs.Copy` | forbidden | `remotefs.Copy` |

Manifest copy dialog: the checkbox "Clean export (strip status and server fields)", on by default. Multi-document YAML and JSON are supported on apply.

### 5.4 Menu F9 → Kubernetes (when a Kube panel is active)

- Logs… (`Shift+F3`): for a Pod owner — choose a Pod from its `pods/`; container (choose, if > 1), Tail lines (`KubeLogsTail` = 1000), Previous instance, Since.
- Follow logs in terminal (`Alt+F3`).
- Describe (`Shift+F4`).
- Open shell in container: `ExecStream` with a TTY and `sh`/`bash` from the probe.
- Refresh snapshot (`Ctrl+R`).
- Switch context…
- Phase 3: Scale…, Restart rollout, Port-forward…, Copy object reference to the clipboard.

### 5.5 Command in a container

`kubeBackend.Execute(a, cmd)`: at the container FS level the command is submitted to the persistent native or ephemeral toolbox shell, creating it asynchronously if necessary. It waits for a ready prompt before submitting the panel directory and command. Keyboard input and resize go to the stream, output to xterm. When the next prompt arrives, the panels return automatically and the container panel is refreshed; the shell and output remain available through Cmd+O. Ctrl+C is sent to the stream as the byte 0x03 (TTY); Cmd+. cancels the context. At the other levels of a Kube panel the command is executed locally in `FilesystemPath()` with the prompt `local:<path>>`.

### 5.6 Connecting

The "Kubernetes cluster" form: Kubeconfig path (by default the first path from `$KUBECONFIG` or `~/.kube/config`), Profile name (blank = context). Then `a.choose("Context", contexts)` with the current-context preselected. `backgroundTask`: Connect, `ServerVersion`, discovery, LIST namespaces. An error shows the client-go text (including the exec plugin's stderr). Success: the panel moves to `/`, the profile is saved, the status is "Connected: k8s:<context> (terminal is local)".

### 5.7 Limitations reported to the user

- Folder size is not computed in a Kube panel (phase 3 via `du`).
- Quick view on an object — phase 3 (manifest).
- Info panel: context, server, version, number of namespaces — phase 2.
- Archives inside a container FS are not opened as folders (as in Remote).

## 6. Key bar with modifiers

### 6.1 Renderer → Go protocol

A new message `{Type: "modifiers", Shift, Ctrl, Alt, Meta bool}`. The renderer (`app.js`) sends it on `keydown`/`keyup` of the Shift, Control, Alt, Meta keys, only if the state changed, and on window `blur`/`visibilitychange` — reset to false. The message is sent in both modes (panels and terminal), but Go uses it only for drawing the panels. A keypress with a modifier (`key` with flags) does not change the protocol.

Go: `App.mods modifiers` (a bitmask), updated in `Handle`, marks `dirty`. Draw calls `p.backend.KeyBar(p, a.mods)`; the document (viewer/editor) and the menu have their own tables in `document_draw.go` and also receive `mods`.

Stuck-key protection: if within 2 s no message has arrived while a modifier is held and a `key` arrives without that flag, the mask is reset according to the flags of that `key`.

### 6.2 Tables

Local (matches the F1 help and Far Manager):

| Mods | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 | 10 |
|------|---|---|---|---|---|---|---|---|---|----|
| — | Help | UserMn | View | Edit | Copy | RenMov | MkFold | Trash | Menu | |
| Shift | AddArc | ExtrAr | ArcCmd | | Copy | Rename | | Delete | Save | |
| Ctrl | | | Name | Ext | Modif | Size | Unsort | Creat | Access | Descr |
| Alt | | | | | | | Find | Histry | | |
| Alt+Shift | | | | | | | | | | |

Remote: as Local, but F8 `Delete`, Shift+F1..F3 empty, Ctrl+F8..F10 empty if sorting by these fields is not supported by the server.
Archive: F4, F6, F7, F8 empty; Shift+F2 `Extract`.
Kube — by level:

| Level | Mods | 1 | 2 | 3 | 4 | 5 | 6 | 7 | 8 | 9 |
|---------|------|---|---|---|---|---|---|---|---|---|
| root | — | Help | UserMn | Manif | Edit | Export | | NewNS | Delete | Menu |
| root | Shift | | | | Descr | | | | | |
| namespace | — | Help | UserMn | | | Export | | | | Menu |
| typeFolder (objects) | — | Help | UserMn | Manif | Edit | Copy | | | Delete | Menu |
| typeFolder (objects) | Shift | | | Logs | Descr | | | | Orphan | |
| typeFolder (objects) | Alt | | | Follow | | | | | | |
| object (type folders) | — | Help | UserMn | | | Export | | | | Menu |
| containers | — | Help | UserMn | Manif | | | | | | Menu |
| containers | Shift | | | Logs | Descr | | | | | |
| containers | Alt | | | Follow | | | | | | |
| containerFS | — | Help | UserMn | View | Edit | Copy | RenMov | MkFold | Delete | Menu |
| containerFS | Shift | | | | | Copy | Rename | | Delete | |

The `Logs`/`Follow` labels are shown at the typeFolder level only if the current object is a Pod or has a Pod among its children (Deployment, StatefulSet, Job, Service, etc.); the key bar is recomputed as the cursor moves.

The Ctrl row (sorting) is the same for all modes wherever the field exists.

### 6.3 Tests

- `internal/native/tests/input.cjs`: `keydown Shift` → message `modifiers{Shift:true}`, a repeated `keydown` does not duplicate, `keyup` → `Shift:false`, `blur` → all false.
- Go: `draw_test` on key bar labels for every table, including the stuck-key reset.
- Consistency check: a test that every non-empty label corresponds to a handler in `far_keys.go`/`function()` (the "label → action" table is single; both the key bar and part of the F1 help are built from it).

## 7. Implementation plan

### Phase 0 — refactoring (no new features)

1. `panelBackend`, `backendKind`, `panelCaps`; `localBackend`, `archiveBackend`, `remoteBackend`.
2. Migration per table 4.3, removal of the fields `Remote`, `RemoteLocalPath`, `Archive` from `Panel`.
3. `remotefs.Endpoint{Opener}`.
4. `documentSource` and moving `openRemoteDocument`/`saveRemoteDocument`/archive viewing onto it.
5. `terminal.Stream` and `App.Foreign` (without implementations other than `Session`).
6. Key bar with modifiers (section 6) for local/archive/remote.
7. Verification 4.5.

### Phase 1 — cluster, namespaces, objects, manifests

1. client-go dependencies, `THIRD_PARTY_NOTICES.txt`, CI cache update.
2. `internal/kube`: profile, client, contexts, discovery, snapshot (Table), object, manifest (Get, ExportYAML, Decode, Delete), describe, logs. Tests on `httptest`.
3. `Settings.KubeProfiles`, `kubernetes.json`, the item in Network connections, the connection dialog, disconnect.
4. `kubeBackend`: levels root/namespace/typeFolder/object, `tree.go` (transparent types, selector children), `ObjectInfo`, status in `panel_draw.go`, key bar, menu.
5. F3/F4 of manifests through `documentSource` (F4 with Update and conflict), F5 object → local and recursive folder export, F8, F7 namespace, Shift+F4, Shift+F3.
6. Documentation: README, F1 help, `MENU_PARITY.md`, CHANGELOG.

### Phase 2 — containers

1. `exec.go`, `containerfs.go` with probe; tests on a local `sh` through an injected `Executor`.
2. `Opener` for a container, routing F5/F6/F7/F8 into the container FS.
3. The containers/containerFS levels, rule C3.
4. `ExecStream`, `Execute` in a container, "Open shell in container", follow logs, the `pod:` prompt.
5. F3/F4 of container files.

### Phase 3 — apply and extensions

1. Apply when copying into a namespace (local → k8s, k8s → k8s), the Override/Apply/Skip questions.
2. Scale, Restart rollout, Port-forward (`k8s.io/client-go/tools/portforward`), restoring the panel from the session, Info panel, Quick view, `du`, streaming copy of trees with a single `tar`, ephemeral debug container, merged logs of all Pods of an owner.

## 8. Testing

- `internal/kube`: an `httptest` server with discovery, Table LIST, GET/PUT/PATCH/DELETE, logs; `tree.go`: collapsing of ReplicaSet (including old ReplicaSets after a rollout), ControllerRevision under StatefulSet, Service → Pod by selector (including an empty selector = no children), CronJob → Job → Pod, an object with several owners, a CRD without rules; type folder names with group conflicts; clean export; recursive folder export; multi-document Decode; Update conflict; forbidden → Skipped.
- `ContainerFS`: all three listing variants, a symlink to a directory, names with spaces and non-ASCII, absence of shell/tar.
- `internal/ui`: navigation across levels with the cache, C3, key bar by level and modifiers, F5 routing per table C4, namespace deletion with name entry, `Execute` builds the command with correct quoting, the `Foreign` stream intercepts input and returns the shell, profiles do not touch `connections.json`.
- `./scripts/check.sh --race` without access to a real cluster; `FARSHMAC_KUBE_CHECK` — opt-in.

## 9. Decisions on open questions

| Question | Decision | Rationale |
|--------|---------|-------------|
| Cluster-scoped objects (`_cluster`) | Not doing it. | The tool is a dev tool for quick access to application objects, not a cluster maintenance tool. |
| Namespace layout and resource set | Folders by resource type; inside an object — child type folders; ReplicaSet and ControllerRevision are collapsed; Service shows Pods by selector. No type blacklist is needed: `events` is an ordinary folder. | A type folder isolates the noise of any type and mirrors the familiar `kubectl get <type>`; the collapsing is defined in code (`tree.go`) so that the tree is the same and shallow. |
| Describe | `k8s.io/kubectl/pkg/describe`. | Covers all built-in types and events; the binary growth is moderate. |

There are no open questions. The next step is phase 0 (section 7).

### Node scheduling links

Core Nodes expose a lazily loaded `pods` folder using `spec.nodeName` rather than ownerReferences. Both LIST and watch use a server-side field selector across namespaces. Entries use `namespace:pod`, while manifest, container and filesystem operations preserve the original namespace and pod name. Search does not implicitly expand Nodes into all their pods.

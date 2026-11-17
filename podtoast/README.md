# PodToast

PodToast is a Go file/diagnostic agent with an ephemeral-container toolbox for inspecting and operating on running
Kubernetes containers, including distroless and scratch applications. FarshMac
connects to it through an ephemeral container and a persistent Kubernetes exec
stream. No shell, `ls`, `cat`, `tar`, SSH server, service or listening agent port
is needed in the application container.

The standalone MIT-licensed project is [PodToast](https://github.com/ikimpriv/PodToast).
Its public image is `ghcr.io/ikimpriv/podtoast:0.8.1` (Linux amd64/arm64).
This checkout also contains the agent and protocol used by FarshMac integration tests.

## Use from FarshMac

1. Use `ghcr.io/ikimpriv/podtoast:0.8.1`, or mirror it to a registry your cluster can pull.
   The cluster must permit ephemeral containers and the requested access profile.
2. Select a pod, or enter/select its container, then press **Alt+Enter / F11 → Attach ephemeral container… → PodToast**.
   Enter the image, image pull policy and startup timeout. A sole running application
   container is selected automatically. For multiple containers, choose targets with checkboxes.
   With `shareProcessNamespace: true`, FarshMac reuses one matching agent image and
   opens a separate target-root session for each running application container.
   A target that cannot be identified is reported explicitly. The image setting is saved with setup.
3. Enter the container. Existing viewer/editor, copy, move, local copy/rename,
   mkdir, delete, chmod, timestamps and symbolic links use PodToast. Transfers
   retain FarshMac's collision, staging, recovery and metadata policies. Same-
   container copies stay inside the container; rename uses a filesystem rename.
4. The Kubernetes menu also provides **PodToast live monitor**, **PodToast
   snapshot**, and **PodToast network / HTTP load test…**. Network diagnostics use
   any healthy PodToast session in the selected pod, without a container choice.
   Metrics discovery likewise covers the entire pod, including each target of a shared agent.

Esc cancels file operations and network tests. Esc / Ctrl+C / Cmd+. stops the
live monitor. Cancelling one HTTP/2 request does not close unrelated streams.
The original shell backend remains available when PodToast has not been started.
Once connected, errors do not silently switch backends. Reconnect explicitly
when the target restarts or the connection expires (30 minutes idle).

PodToast does not provide a shell to a distroless application. Existing shell
commands still require an actual shell in the selected application container.

## Other ephemeral images and launch options

Alt+Enter or F11 opens the panel/object context menu. On a pod or its container, **Attach
 ephemeral container…** offers PodToast, the last 16 successful launch presets,
and **New image…**. Nothing is created until **Start / attach** is submitted.
History retains the image and options across restarts; target pod/container names
are not reused across pods. PodToast is an explicit integration role: an arbitrary
image is never treated as the agent just because its name contains “podtoast”.

For other images the dialog supports:

- A target container (blank means no target PID namespace; pod networking remains).
- A command as a JSON argv array, e.g. `["/bin/sh"]`; blank uses the image entrypoint.
- `Default` access (image/pod defaults), `Restricted` (non-root, drop capabilities,
  no privilege escalation, RuntimeDefault seccomp), `Network` (NET_RAW/NET_ADMIN),
  `Processes` (SYS_PTRACE), or explicit `Privileged` access. Network/Processes also
  drop other capabilities, disable privilege escalation and use RuntimeDefault.
- An optional run-as UID. Restricted requires a non-root image or nonzero UID.
- Image pull policy: IfNotPresent, Always or Never; 5–600 second startup timeout.
- Interactive TTY and read-only debug-container root switches.

FarshMac attaches to the chosen image's existing entrypoint through Kubernetes
attach, rather than launching a second shell. Short-lived commands show their
logs and exit code. These images do not enable PodToast file/diagnostic integration.
Admission/RBAC failures are reported; profiles are never broadened automatically.
Cancelling creation/wait/attach cannot remove a container already accepted by
Kubernetes. PodToast keeps its required filesystem-access profile and offers only
compatible image, target, pull-policy and startup-timeout settings.

Adding a debugger changes a single-container pod's layout; open panels are
remapped to the original container and folder. Window maximize/restore uses **Alt+F9**.

## Build

From the FarshMac repository root:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /tmp/podtoast-amd64 ./cmd/podtoast
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o /tmp/podtoast-arm64 ./cmd/podtoast

docker build -f podtoast/Dockerfile -t podtoast:dev .
kind load docker-image --name YOUR_LOCAL_CLUSTER podtoast:dev
```

For a registry, substitute a repository you control:

```sh
docker buildx build --platform linux/amd64,linux/arm64 \
  -f podtoast/Dockerfile -t REGISTRY/OWNER/podtoast:VERSION --push .
```

Use the resulting digest in FarshMac for reproducibility. Registry credentials
are managed by the pod/cluster image pull configuration, not passed to the agent.
The final image contains the static Go agent and an Alpine diagnostic toolbox. FarshMac does not require
Docker or kubectl to attach; those tools are only used for building/testing.

## Metrics

PodToast 0.2.0 provides Prometheus `GET /metrics` over its exec connection, plus
an opt-in read-only HTTP listener. See the [metrics specification](https://github.com/ikimpriv/PodToast/blob/main/docs/METRICS.md).

## Available observations

| Area | Implemented |
| --- | --- |
| Files | Persistent multiplexed API, directory records streamed in batches of 256, binary streams, regular-file copy within the target, atomic filesystem rename, links, mode and modification time |
| Processes | Visible PID namespace: PID, parent, command name, state, threads, cumulative CPU time, RSS and process start ticks; capped at 16,384 records |
| CPU | Verified target cgroup v2 usage, quota, throttling and pressure; live CPU percentage uses deltas, where 100% is one core |
| Memory | Current/peak/limit, memory breakdown, swap, pressure and OOM counters when exposed by the target cgroup |
| Storage | Root and up to 64 non-pseudo directory mounts: capacity, available bytes, used bytes and inodes; cgroup I/O counters and pressure |
| Network | Pod-scoped TCP/UDP IPv4/IPv6 sockets, states and endpoints (up to 4096 per table), Unix socket table, interface counters, routes, TCP/IP counters and resolver configuration |
| Probes | DNS lookup, TCP connect, HTTP(S) GET and bounded HTTP load test from the pod network |
| Load test | Explicit target, request count, concurrency and global rate; status counts, transport failures, p50/p95/p99 elapsed request latency |

Live monitor shows a compact overview once per second; the JSON snapshot contains
more detail. File browsing remains independent from monitoring and probes.
Counters/tables that the kernel does not expose carry an `unavailable` reason.
Some diagnostic text files are capped at 64 KiB. The monitor has no growing
history buffer; snapshots and socket/process counts are bounded. Directory
listings are streamed at the agent, but FarshMac still holds the current panel's
entries in memory to sort and display them.

Load tests are HTTP GET only, at most 10,000 requests, 32 workers, 1,000 requests
per second and 60 seconds. Individual requests time out after 10 seconds and
consume at most 8 MiB of response body. Redirects are not followed; TLS
verification is enabled and uses the target root's trust store. HTTP status
counts are separate from transport/read failures. Percentiles include completed
failed requests; deadline-limited runs report how many requests completed.

## Access model and limitations

- Creation is an explicit user action. It requires access to pods and the
  `pods/ephemeralcontainers` subresource, plus `pods/exec`. Admission policies may
  refuse the requested security context. PodToast never installs a DaemonSet,
  mounts the host filesystem, uses a runtime socket, or changes cluster RBAC.
- The agent runs as root with a writable ephemeral root for shell temporary files, no privilege escalation,
  `RuntimeDefault` seccomp, all capabilities dropped except SYS_PTRACE,
  SYS_CHROOT, DAC_OVERRIDE, FOWNER, CHOWN and FSETID. These permit intentional
  target-root access and file operations; Pod Security Restricted will normally
  reject this. There is no automatic privileged retry.
- The runtime must support `targetContainerName`. The agent identifies the target
  via its runtime ID in `/proc/*/cgroup`. For a non-shared PID namespace it can
  use PID 1 after verifying that this is not its own root. Ambiguous shared-PID
  targets are refused. Host PID/network pods are not supported.
- Before serving, the dedicated exec process enters `/proc/PID/root` using
  chroot, so absolute symlinks have the application's semantics. Process start
  time is pinned and checked; a target exit/restart ends the session. Chroot is
  for path semantics, not an independent security sandbox. Kubernetes RBAC and
  the debug container security context remain the access boundary.
- New files, directories and symlinks use the target process filesystem UID/GID,
  so running the diagnostic process as root does not leave ordinary application
  uploads owned by root. This is not preservation of arbitrary source ownership.
- Read-only mounts stay read-only. The generic FarshMac remote transfer layer
  currently preserves mode and modification time, not all Linux ACLs/xattrs,
  owner/group or sparse/hard-link topology. Special-file payloads are refused.
- Cgroup metrics are reported only if target membership is verified; cgroup v1
  and inaccessible or ambiguous cgroup layouts show unavailable. Process RSS
  sums must not be mistaken for cgroup memory. Network counters are shared by
  the pod. Filesystem capacity/usage can be shared with other pods and is **not**
  an exclusive container writable-layer or directory size.
- A running matching ephemeral container is reused. Its idle process expires
  after 24 hours. Disconnecting closes exec sessions, not the idle container.
  Kubernetes keeps ephemeral-container records until the pod is deleted, and
  does not restart a terminated ephemeral container. Cancelling startup cannot
  undo an already accepted creation request.

## Verification

```sh
go test -race ./internal/podtoast ./internal/kube
./podtoast/test.sh
```

The second command requires local Docker and kind. It creates a uniquely named
cluster with its own kubeconfig, runs a non-root, shell-free scratch application
with a read-only root and writable volume, then removes that test cluster. It
checks real ephemeral-container startup/reuse, binary transfers, absolute and
relative symlinks, internal copy/rename, permissions, read-only errors,
observations, HTTP load and cancellation. It also runs the protocol tests inside
a Linux scratch container. The PodToast GitHub workflow runs Linux race tests,
both binary cross-builds and this integration test.

## Further integrations worth adding

These are opportunities, **not implemented features**:

- Process detail screens: file descriptors with socket-to-PID correlation,
  deleted-but-open files, cwd/executable, limits, I/O, signals and namespace IDs.
  `/proc` access is permission-dependent; environment values should be opt-in
  because they commonly contain credentials.
- Directory watch/inotify updates and agent-side recursive size/search jobs with
  progress, bounded queues and cancellation. These can remove per-directory
  network round trips for very large trees.
- Application-specific endpoints: Prometheus/OpenMetrics, Go pprof, JVM metrics
  and health endpoints. These require the application to expose them.
- Network screens with time-series rates, connection churn, DNS timing, TLS
  certificate inspection, configurable HTTP methods/headers/bodies and latency
  histograms. UDP probes and protocol-aware checks need explicit semantics.
- Packet capture and syscall profiling as separate capability profiles. Capture
  generally needs NET_RAW; ptrace profiling can disturb an application. eBPF
  often needs node/kernel access and cannot be assumed to work from this profile.
- Exact writable-layer usage, GPU telemetry, host-wide conntrack and historical
  metrics generally require runtime, node, device or external monitoring access.
  An ephemeral container alone cannot promise these.

Design references: [Kubernetes ephemeral containers](https://kubernetes.io/docs/concepts/workloads/pods/ephemeral-containers/),
[process namespace sharing](https://kubernetes.io/docs/tasks/configure-pod-container/share-process-namespace/),
[Linux cgroup v2](https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html).

## Storage interpretation

The agent currently reports mount paths, filesystem types, mount options and
filesystem-wide capacity/free space. It does not classify mounts as PVC, emptyDir,
projected volumes or container writable layers. That requires joining the target
container's mount information with the pod's `volumeMounts` and `volumes` (and PVC/PV
resources when permitted). A filesystem's used space is not the container's own disk
usage. The container writable layer and disk-backed emptyDir both contribute to
local ephemeral storage; memory-backed emptyDir is charged to memory.

## Ephemeral shell and toolbox

Version 0.3.1 adds shell utilities. FarshMac runs commands in the ephemeral
container, at the target folder shown in the panel. See [execution context, target selection and component licenses](TOOLBOX.md).

## Background metrics history

New builds collect basic target cgroup, filesystem-capacity and network-interface
metrics during `idle`, even with no connected client. The collector samples every
five seconds and retains up to ten minutes (120 frames), bounded to 1 MiB of metric
text per target and at most 16 distinct target roots. Under high cardinality, the
byte limit may shorten retention. It avoids process enumeration, mount inventory
and socket-table scans for each sample. Filesystem capacity is not writable-layer
or ephemeral-storage usage.

History lives in the toolbox's private `/tmp/podtoast-metrics-history` directory;
it does not write into target containers or expose a new network listener. The
`/metrics/history` endpoint is available over the existing target-bound exec stream
(and the optional read-only metrics listener). PID start time prevents history from
being reused after a target restart. Clients replay original timestamps before
live observations. Existing deployed images must be replaced to gain this feature;
older agents still provide live metrics without history.


Version 0.8.0 adds agent-side streamed directory summaries (QuickLook and folder
size calculation), request cancellation, and autonomous retained metrics history.
Existing ephemeral containers keep their original image; attach a new 0.8.0
agent to use these features. FarshMac falls back to client-side directory walks
with older agents and reports unavailable history.

Use **F11 → PodToast version / update…** to inspect running agent image versions.
When the selected image differs, FarshMac offers to attach that image explicitly;
it never silently substitutes an older agent. A new running agent becomes the
preferred connection for its target (or covered targets in a shared PID namespace).
File sessions reconnect to the preferred agent, and active metrics readers switch
on their next scrape. Older ephemeral containers remain in the pod. An image
that fails to start does not displace the working agent.

After replacement, superseded agents are retired only when all their covered
targets have working replacements. Existing file operations drain before the old
connection closes. A bounded background retirement waits for shells and other
diagnostics; busy or unverifiable agents are kept and reported in the status line.

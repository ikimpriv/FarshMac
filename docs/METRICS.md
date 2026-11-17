# Live pod metrics

Select one or more pods (using normal file-panel marks), or enter a container, and choose **Alt+Enter / F11 → Live metrics…**. Pod selections open charts immediately in a separate window. From a container, choose a compact neighboring panel, charts for that container, or charts for the whole pod. The Kubernetes menu offers the same action. No external Prometheus server, chart library, kubectl or local exporter is required.

## Discovery and access

FarshMac discovers endpoints from:

- `prometheus.io/scrape: "true"`, `prometheus.io/port`, `prometheus.io/path` (default `/metrics`) and `prometheus.io/scheme` (default `http`). Ports can be numbers or declared names.
- `monitoring.coreos.com/v1` PodMonitor selectors, namespace selectors, endpoint ports, paths, schemes and query parameters. Missing CRDs are harmless; access errors are visible. Listing all namespaces falls back to the pod namespace when forbidden.
- Declared TCP ports whose port or container names contain `metrics`, `prometheus` or `exporter`, probing `/metrics`, `/actuator/prometheus` and `/q/metrics`.
- An already running PodToast ephemeral container (all running application targets when the pod shares its PID namespace), over its own multiplexed exec connection. Opening charts never attaches an ephemeral container automatically.

HTTP endpoints are read through the Kubernetes API pod proxy, using the selected connection's credentials. RBAC must allow `get pods` and `get pods/proxy`; PodMonitor discovery needs `list podmonitors.monitoring.coreos.com`. PodToast uses `create pods/exec`. The pod UID is checked on each scrape to avoid silently following a replacement pod.

**Endpoints** shows source, status, errors and enable/disable controls. **Rediscover** creates a fresh dashboard session after endpoint configuration or container changes. Probe failures are normal when a server only supports one candidate path. No arbitrary port scan is performed.

PodMonitor authentication, custom TLS configuration and relabeling are currently reported as unsupported for that endpoint rather than silently ignored. Credentials from Kubernetes Secrets are not read. ServiceMonitor discovery is not implemented.

## Charts

Charts initially select only CPU usage across the selected targets, preferring verified PodToast cgroup counters over duplicate resource-metrics values. Late-arriving CPU sources are added until the user changes the selection. The catalog opens with **Metrics…** or Cmd+F; series and chart settings stay collapsed by default. Selecting memory, storage or network metrics creates a corresponding chart automatically. Available measurements depend on the exporter: FarshMac does not invent missing CPU, filesystem or network data.

- Search metric names, labels, types and sources; Cmd+F focuses search.
- Check metrics to plot them; selected metrics have a chart selector, Auto/Raw/Rate mode, multiplier and offset.
- Add, rename or remove charts and put several series on one chart.
- Normalize each series over the visible time range: original units, min–max 0–100, absolute peak = 100, first nonzero = 100, or z-score. Use normalization or explicit multipliers when overlaying incompatible units.
- Hover to inspect values. Change the visible time range and collection interval. Esc pauses collection and cancels outstanding requests; Resume restarts it.

Gauges show raw values. Counters and histogram buckets default to per-second deltas with reset detection; the first rate needs two successful scrapes. Classic histogram buckets also produce p50/p95/p99 estimates from interval bucket deltas. Summary quantiles are displayed as exported; sum/count components remain separately selectable. Errors, missing samples, nonfinite values and interrupted rate baselines are gaps, not zeroes.

The parser supports Prometheus text and compatible OpenMetrics text samples, including exemplars (ignored for plotting). Native histograms and nonstandard exposition formats are not supported.

## Windows and resource limits

Charts always open in an independent macOS window with their own collector and settings. It keeps collecting after the original file-manager window closes and does not start another shell. Closing the chart window stops its requests; Cmd+Q still quits the entire application. Dashboard history and configuration are session-local and are not restored after restarting the application.

Each dashboard accepts up to 32 selected pods and each pod contributes at most 32 discovered endpoints. HTTP/PodToast discovery is capped at 128 targets per dashboard, with up to one additional resource-metrics source per pod. Each session allows four concurrent requests, a five-second request timeout, at most 2 MiB / 8,192 samples per response, 1,024 retained series, 300 points per series, and 48 plotted series across up to 12 charts. PodMonitor discovery examines at most 500 monitors and reports truncation. Failed requests back off up to 60 seconds; unselected stale series expire after ten minutes. A long time-range selection does not create older samples: retained history depends on the scrape interval. Catalog and chart data are sent to the renderer only when changed, not on every UI poll.

## Validation

Race-enabled Go tests cover parsing, counter resets and gaps, histogram quantiles, bounded history, cancellation, discovery selectors and detached-window ownership. Renderer tests cover normalization and default selection. `podtoast/test.sh` additionally exercises annotation-based HTTP scraping and PodToast metrics against an isolated kind cluster with a distroless workload.

## Container attribution and compact mode

Sources carry `farshmac_namespace`, `farshmac_pod` and `farshmac_container` labels.
PodToast data is attributed to its **target**, never its ephemeral execution
container. Container-only sessions exclude other targets. Pod-wide HTTP exporters
can supply their own `container` labels; otherwise ownership is inferred only
when a declared port has one container owner. Unknown ownership remains pod-scoped.
Kubernetes `metrics.k8s.io/v1beta1` is also queried for CPU cores and memory bytes,
when metrics-server and RBAC permit it; no metrics-server is installed implicitly.

Compact mode occupies the neighboring Quick view panel in the current FarshMac
color theme, with small history charts. Click a checkbox or focus the panel with
Tab; keys **1 / 2 / 3** toggle
CPU / memory / ephemeral storage; **P** pauses/resumes; **Esc** restores the previous panel
and cancels collection. Ctrl+Q also closes the compact view. CPU counters need two scrapes. Memory is a current gauge.
Ephemeral usage is shown only when an exporter provides a container-scoped
`container_ephemeral_storage_used_bytes` or `ephemeral_storage_used_bytes` metric.
PodToast mount capacity and filesystem-wide used space are deliberately not
substituted for writable-layer/log/emptyDir usage. Missing measurements display
unavailable, not zero. Closing the compact panel releases its independent clients.

## Integrated container presentation

Running PodToast agents are hidden from the container list. A ◆ badge marks pods and running target containers covered by an agent (all running targets only when PID sharing and the agent security profile permit full filesystem management; otherwise only the agent’s explicit target). Other ephemeral containers remain visible. Watch updates remove coverage when the agent stops. Shared coverage requires an explicit root identity and SYS_PTRACE, SYS_CHROOT, DAC_OVERRIDE, FOWNER, CHOWN and FSETID (or privileged root). A shared PID namespace alone is insufficient. This is a conservative eligibility check; runtime security policies and read-only mounts still apply. Filesystem paths and operations continue to refer to the target container. The shell prompt identifies PodToast execution; absolute paths still refer to the ephemeral root, and `$TARGET_ROOT` refers to the target root.

Charts identify target containers rather than agent containers. PodToast metric names are presented without their exporter prefix; the original series identifiers remain unchanged. Use the macOS Window menu or Command-backtick / Command-Shift-backtick to switch between file manager and chart windows.

## Overlapping PodToast observations

Multiple agents do not contribute additive copies of the same measurement. Network counters are pod-scoped. Visible process totals are pod-scoped with a shared PID namespace and container-scoped otherwise. Target cgroup CPU and memory remain container-scoped. Duplicate agents observing the same target share a single logical series. One healthy producer owns each series; an unavailable, disabled or stale producer can be replaced without changing the series ID. Rates restart on producer changes instead of taking a delta between exporters.

Filesystem capacity/free/used values from older agents have no filesystem identity. They remain separate target observations and are never summed into a pod total. Equal numbers alone do not prove that two filesystems are identical. Graphs overlay series; they do not stack or sum these observations. Unrelated Prometheus endpoints retain their original identity.

Quick Live Metrics and detached charts include a subtle time/value grid.
Container requests use thin green dotted lines; limits use thin red dotted lines,
with human-readable labels. Bounds appear only when monitoring one container or
a pod with a single application container (including restartable init sidecars).
Selecting multiple containers or pods hides all bounds, even if some sources fail. Kubernetes budgets come directly from the Pod
manifest, independently of metrics-server; verified PodToast CPU/memory cgroup
limits provide a fallback. Unknown or unlimited values are omitted. Bounds are
scoped to each selected container, never summed, and included in the chart scale.
Detached charts hide absolute bounds when normalization is enabled. Filesystem
capacity is not treated as an ephemeral-storage limit.

Quick charts use short Request/Limit labels without repeating the selected
object's name. History load failures are retried; recovered history is merged
before live points and counter rates are recomputed. Quick Live Metrics reports
when an agent cannot provide retained history. Autonomous pre-open history
requires a PodToast image containing the background history collector; older
images cannot reconstruct samples they never collected.

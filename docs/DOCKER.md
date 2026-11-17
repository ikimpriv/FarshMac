# Docker integration

FarshMac invokes the installed Docker CLI with argument arrays, preserving its
context and authentication behavior. It does not mount the Docker socket inside
containers or invoke a shell to interpret names or image arguments.

## Panels

- **Compose** groups containers by the Compose project label.
- **Containers** lists standalone containers. FarshMac helpers are hidden.
- **Volumes** lists named volumes. F11 can explicitly attach a volume toolbox
  with a read/write mount; Enter then opens that volume.
- **Networks / Images** provide structured object reports.

F3 and Quick view show container, volume, network and image information grouped
into identity, state, storage, network and configuration sections. Compose projects
show their services, images and current state. Inspect reports are read-only: they
are not Docker update manifests. F4 edits actual files inside containers and
attached volumes through the existing save/conflict handling. Quick view reads at
most 16 KiB of file contents; object previews are capped at 64 KiB. Requests run in
the background and are cancelled when the selection changes.

Container files use the common transfer/document interfaces. Same-container copy
uses the backend copy operation; rename stays in the container. Cross-backend
transfers use the existing streaming, collision and metadata handling. Filesystem
catalogs cannot be destinations for file operations.

## Shells and toolboxes

Native shells run through Docker exec with a PTY. A PodToast toolbox shares the
target PID and network namespaces and requests SYS_PTRACE, allowing target-root
access even when the target has no shell. Its file API uses HTTP/2 over exec
stdin/stdout, with no listener port. Commands use toolbox binaries with cwd below
the target root and identify PodToast in the prompt.

Toolboxes are never created just by browsing. F11 attachment is explicit. Existing
running helpers are discovered by labels and reused. Helpers remain available
across application sessions; the current image's idle lifetime is 24 hours.
They may also be removed using Docker. Volume toolboxes have networking disabled.

Absolute paths typed inside a toolbox shell refer to that toolbox's root. Relative
paths start in the target folder; TARGET_ROOT points to the target filesystem.
The panel never interprets an outside-target shell cwd as a target directory.

## Diagnostics

F11 / Option+Enter provides object-specific operations without duplicating F3/F4
or file copy/rename/delete shortcuts. Container controls depend on state: start for
stopped containers, stop/restart/pause for running containers, and unpause for paused
containers. Menus also offer logs, processes, filesystem changes, port mappings,
signals, CPU/memory limits and restart policy. Mutations require confirmation.

Images offer layer history, tagging, streamed TAR export and a configurable detached
run dialog. Networks offer connected-container lists and connect/disconnect pickers
(the built-in host/none networks do not offer these mutations). Volumes offer
consumer lists, toolbox attachment and streamed TAR.GZ backup through a temporary
network-isolated, read-only-mounted toolbox. Backups do not freeze writers; stop
applications first when an application-consistent backup is required. Archive output
must be a new local path; errors/cancellation never publish partial output.

Compose projects offer per-service logs and state-filtered lifecycle actions on
existing containers. Recreation validates the original configuration and shows its
paths before confirmation; it uses `up --detach --force-recreate --no-build`. This
requires a local Docker context and accessible Compose files. Missing environment
files or variables may prevent validation. No global prune or implicit deletion is
provided.

 Metrics reuse FarshMac's compact panel and independent graphical window,
including selection of multiple containers. CPU is measured in cores; memory is
in bytes and rendered with human-readable units. Network/block-I/O counters are
rates in charts. Docker CLI display rounding also applies to sampled counters.
Writable-layer storage usage is not inferred from block I/O.

PodToast diagnostics expose target processes, resources, storage and network.
The detached traffic monitor uses a separate explicitly attached helper with
NET_ADMIN and NET_RAW. Attaching it enables observation; traffic changes require
the existing explicit fault-control actions. Network namespace sharing means
controls affect that namespace, not an artificial per-container network.

## Verification

The normal suite uses synthetic routing/authentication fixtures. Set
FARSHMAC_DOCKER_TEST_IMAGE to a locally available PodToast image to run the opt-in
Docker tests. They create and clean up disposable containers, a scratch distroless
image and a named volume; they do not operate on existing user containers.

# Docker connections over SSH

Open **Alt+F1 / Alt+F2 → Docker… → Create Context…** (also available through
the Left/Right menus). Choose one of these modes:

- **SSH host**: enter a context name, hostname/IP address, optional user and port,
  and optionally a private-key path. F4 in the key field opens the file picker.
  A blank key uses normal OpenSSH configuration and agent identities.
- **SSH alias**: enter a name already configured in OpenSSH. F4 lists literal
  aliases found in `~/.ssh/config` and included files. You can also type an alias
  manually. OpenSSH resolves its user, port, identity, ProxyJump and other options.

Creation saves a standard Docker CLI context and returns to the context list,
with the new context selected. Enter connects. It does not switch the CLI's default
context or require the server to be online during creation.

An explicit private key in host mode creates a uniquely named Host stanza at the
beginning of `~/.ssh/config`; the Docker context points to that alias. Existing
configuration is retained, and failed creation rolls back the stanza unless a
concurrent configuration edit prevents safe rollback. Symlinked/nonregular configs
are not rewritten: use an existing SSH alias instead. The private key itself is
never copied into Docker metadata or FarshMac settings. Context names must be unique.

Docker CLI and OpenSSH must be available locally, and the remote SSH user needs
access to Docker. Encrypted keys should be unlocked in ssh-agent. Existing host-key
verification remains enabled; a new/untrusted host must be verified using SSH.

## PodToast on Linux Docker hosts

PodToast joins the selected container's PID and network namespaces, runs as root
with SYS_PTRACE, and uses the target's AppArmor profile when it differs from the
Docker default. This allows `/proc/<pid>/root` access across the matching security
domain. Target and daemon security settings are not modified; the helper does not
use `--privileged` or host PID/network namespaces.

Attachment verifies a PodToast handshake before reporting success. **Reconnect /
repair PodToast toolbox…** reuses a working agent or creates a verified replacement.
Failed new helpers are removed; replacements are preferred over older helpers
when panels refresh or reconnect. Older helpers are retained because another
window may still own a session. Host policies such as user namespaces or custom
AppArmor rules can still prevent access; those failures are reported immediately.

## Panel badges

Docker catalog rows use the same PodToast diamond as Kubernetes: **◆** marks a
container or volume with a running management toolbox, or a Compose project with
all its containers covered. **◇** marks a partially covered Compose project. The
footer shows the covered/total container count for the current project. Selection
checkmarks take precedence in the row; the footer still shows PodToast coverage.

Badges use the Docker catalog snapshot, without extra network requests during
rendering. They refresh after attachment/repair or a panel reread. Stopped targets
and stopped toolboxes do not count as covered; network-only helpers do not imply
filesystem management. Toolbox containers themselves remain hidden.

## Container status and age

Container catalogs (including Compose service containers) show **Status** and
**Age**. Age is elapsed time since container creation, not uptime; it does not
reset on a restart. Docker uptime and full status remain visible in the footer.
Running healthchecks and exit codes are included in the status column. Inactive
containers are dimmed, paused containers are yellow, and unhealthy, restarting,
dead or unsuccessfully exited containers are red. File views inside containers
keep their normal storage, size and modification columns.

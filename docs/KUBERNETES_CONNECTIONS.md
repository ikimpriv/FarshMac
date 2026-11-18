# Kubernetes connections

Open **Kubernetes cluster…** from Left/Right, Alt+F1/Alt+F2, or Network connections.
There are three configuration sources:

1. **Local kubeconfig files**: direct API access. Discovery starts immediately
   and finds configurations in `~/.kube`, one subdirectory level, and the files
   named by `KUBECONFIG`. Cache directories are skipped. An explicit path works
   for configurations elsewhere.
2. **SSH server**: enter only a server address/alias and optional username.
   FarshMac first tries existing SSH configuration, keys and agent automatically.
   It asks for authentication only if that fails, then discovers readable configs
   in the remote `~/.kube`, the remote command's `KUBECONFIG`, and the standard
   k3s and kubeadm configuration paths. Select a context to connect. An explicit
   remote path is available through Other file / F4. No remote kubectl installation is needed.
3. **Custom kubeconfig files**: choose **Paste new kubeconfig**, give it a name,
   and paste YAML with Cmd+V. Choose direct API access or SSH, then a context.
   Reopen the named config to use another context; each context can have its own
   saved connection profile. F4 renames/replaces the config, and F8 deletes a
   config after its connection profiles have been removed.

The selection dialog has two lists: configuration files on the left and contexts
of the selected file on the right. Tab/Left/Right switch lists; arrows, Page Up/Down
and Home/End navigate. Enter in the file list moves to its contexts; Enter in the
context list connects. Mouse selection, double-click and wheel navigation work.
F4 selects a config outside the discovered locations; Escape cancels.

A kubeconfig does not describe an SSH route. FarshMac never guesses a route from
`127.0.0.1`: the user selects the SSH host. Through SSH, `127.0.0.1:6443` and kind's
other published API ports refer to the SSH server, not the Mac. Private IP and DNS
API endpoints are also reached from that server.

## SSH and TLS

The system OpenSSH client uses `~/.ssh/config` aliases, ProxyJump, keys and the
agent. A server address may include a port, for example `host:2222` or
`ssh://user@host:2222`. The initial dialog has no key, port, config-file or plugin
settings fields.

If automatic authentication fails, choose a private key (optional passphrase),
a password, or an SSH settings file. The successful method and key/config paths
are saved per server/user. Passwords and passphrases remain in memory for the
app session and are passed to OpenSSH through a private, short-lived askpass
socket, never process arguments, environment values or saved profiles. After an
app restart a password may be requested again.

Unknown host keys require explicit fingerprint confirmation. Changed keys are
refused, not silently replaced. First-time trust through an SSH jump/proxy must
be established in Terminal before retrying. Explicitly confirmed host keys are
stored in FarshMac's `ssh_known_hosts`; standard known_hosts checks remain enabled.

API requests use SSH TCP forwarding while retaining the original Kubernetes
server URL and TLS certificate verification. The Mac's HTTP proxy environment is
not used to bypass the SSH route. Exec/shell WebSocket and SPDY streams use a
short-lived loopback bridge because client-go's streaming constructors do not
use the REST dialer. Cancellation closes the bridge and its connections.

Remote config certificate/key/token file references are read on the server,
including paths relative to that config. Pasted configs should preferably embed
these data. For a pasted config used over SSH, FarshMac asks for the original remote config
path only when relative credential references need it; absolute paths refer to
the SSH server. For direct pasted configs, referenced files must be available locally
(absolute paths are recommended).

Remote/SSH configs that require an exec credential plugin or an external-command
auth provider are rejected with an explanation instead of executing a remote
command on the Mac. Certificate and token authentication used by kind/k3s is
supported. Local/direct configurations keep normal client-go plugin behavior;
OIDC is supported without copying renewed credentials into connection profiles.

## Storage and limits

`kubernetes.json` stores connection metadata only: configuration path, context,
SSH options and display name. Remote configs are read into memory, not copied
into the app's saved config library.

Explicitly pasted configs are stored under the app's `kubeconfigs/` directory as
atomic mode-0600 files, inside a mode-0700 directory. `kubeconfigs.json` contains
only their IDs and user-defined names. The paste control preserves line breaks
and displays a size summary, not credentials. Configs are not encrypted at rest;
normal macOS account and disk protections apply.

Discovery scans at most 128 candidate files, with a 4 MiB per-file limit. It does
not recursively search a server, use sudo, change its current context, or modify
its files. `KUBECONFIG` entries are offered individually, not implicitly merged.
Escape cancels discovery and connection attempts. Successful connections are
available through the existing saved/recent connection and bookmark mechanisms.

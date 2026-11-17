# PodToast toolbox and execution context

PodToast 0.3.1 includes an Alpine Linux toolbox: bash/sh, coreutils, findutils,
grep/sed/awk, tar and compression/archive tools, curl/wget, DNS tools, ip/ss,
ping, netcat, ps/top, util-linux, file, less, jq, OpenSSL and CA certificates.
The Go agent and its filesystem protocol remain usable without a target shell.

## FarshMac integration

The panel displays the target container's filesystem. Typed commands and the
interactive shell run in the **ephemeral container**, with cwd set to
`/proc/<target-pid>/root/<panel-folder>`. Both the prompt and terminal identify
the target and the ephemeral execution container. No tool is installed into the
application container, and command execution does not chroot into its filesystem.

- Relative paths start in the target folder.
- `/` and absolute command paths refer to the ephemeral root.
- `$TARGET_ROOT` refers to the target root; use `cat "$TARGET_ROOT/etc/os-release"`.
- Absolute symlinks followed by debug tools use the ephemeral root. PodToast's
  file API still uses the target root, including target-relative absolute symlinks.
- Commands use the ephemeral container's user, privileges, environment and tools;
  they are not application processes. Files created by a root shell can be root-owned.
- An interactive `cd` is local to that shell. Each command launched from the panel
  starts at the panel folder. Commands never silently fall back to the host shell.
- Target identity and root access are checked before starting a command. Runtime
  support and permissions are required; ambiguous shared PID targets are refused.

Other ephemeral images work through their own shell and filesystem tools. Keep
such containers running (for example `/bin/sh -c 'sleep 86400'`). The **Files and
shell** access profile adds SYS_PTRACE, DAC_OVERRIDE, FOWNER, CHOWN and FSETID;
UID 0 is typically needed for target-root access. A container that exits, lacks a
shell/tools, or cannot access the target root cannot provide this integration.

Choose targets with checkboxes. Isolated PID namespaces need an ephemeral
container per selected target. PodToast reuses one agent for selected targets in
a shared PID namespace; their file sessions remain distinct. Arbitrary images
are started separately for each selected target. Already created ephemeral
containers remain in the pod after cancellation, as required by Kubernetes.

## Component licensing and sources

The PodToast Go code is MIT-licensed. The image also contains Alpine Linux and
third-party packages under their own licenses, including GPL/LGPL components;
the MIT image label describes the agent, not a relicensing of bundled utilities.
The Go and Go-module license texts are in `/licenses`. Exact installed APK
versions, origins and license identifiers are recorded in
`/licenses/alpine-packages.txt` and `/licenses/apk-installed.txt`.

Alpine package descriptions and source build recipes are available at
https://pkgs.alpinelinux.org/packages?branch=v3.22 and
https://gitlab.alpinelinux.org/alpine/aports/-/tree/3.22-stable . The recipes name
upstream source archives and patches; Alpine also mirrors distribution sources
at https://distfiles.alpinelinux.org/distfiles/v3.22/ . Preserve the corresponding
component licenses and source availability when redistributing this image.

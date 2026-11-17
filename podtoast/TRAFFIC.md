# Interactive pod traffic monitor

PodToast 0.7.0 extends `podtoast traffic`, an HTTP/2 control service over Kubernetes
exec stdin/stdout. It does not expose an HTTP control port in the pod. The new
`traffic` hello capability is separate from filesystem access. Traffic runs in
the ephemeral root without chroot or a target PID requirement.

TLS decryption is not implemented. No CA is generated, trust store changed, or
TLS certificate validation bypassed. Encrypted connections remain opaque TCP.

## Passive observation

`GET /v1/traffic/overview` returns `overview` (socket inventory and non-loopback
interface totals) and `capture` (per-flow and port-group counters). POST also
renews an active fault lease. Opening this endpoint never installs iptables rules.
`POST /v1/traffic/stop` stops fault injection while capture continues; closing the
exec transport stops both. The hello response advertises `trafficOverview`.

The connection table includes pre-existing TCP/UDP sockets over IPv4/IPv6.
Counters start when monitoring opens, not when the application started. A 4 MiB
TPACKET_V2 mmap ring captures 256-byte header snapshots with NET_RAW. Only header
metadata and byte counts survive parsing; no payload or TLS decoding is performed.
Bytes are IP bytes, including headers and retransmissions, not application payload
bytes. Interface totals exclude loopback; flow counters include local conversations
once. These scopes differ and need not sum to the same total.

Incoming groups use the local server port; outgoing groups use the remote port.
For pre-existing TCP flows, listener inventory helps infer direction; for UDP,
the first observed packet determines direction. This is heuristic for connections
whose handshake predates capture. UDP flows expire after 60 seconds of inactivity.
Port-group byte totals retain short flows even when detailed history is evicted.
Connection counts describe observed flow records, not guaranteed handshake counts;
active record eviction can increase the count on later traffic.

Limits: 8,192 recent flow records, 2,048 port groups, 4,096 inventory sockets,
300 chart samples, 64 selected rows. Kernel drops, unparsed/non-TCP/UDP packets,
record evictions and the group limit are explicit diagnostics. Non-first IP
fragments and unsupported extension chains cannot currently be attributed to a
flow, so counts are incomplete when these diagnostics increase. Capture is not a
lossless packet recorder. Existing service-mesh/CNI behavior may affect visibility.

FarshMac separates Connections, Port groups, Sockets & listeners and Fault injection.
Select several rows for an overlay, optionally with a sum; filter sent, received
or combined traffic. Group charts keep five minutes of history; per-connection
history starts when selected. TCP/UDP observation does not depend on the proxy's
selected port. Fault controls apply only to the explicitly enabled IPv4 TCP
experiment; an observed pre-existing flow cannot be reset through this proxy API.

## Fault injection: scope and effects

Each experiment selects one **IPv4 TCP destination port**, a peer CIDR and an
inbound/outbound/both direction. Inbound uses PREROUTING and the source CIDR;
outbound uses OUTPUT and the destination CIDR. IPv6 and UDP fault injection are unsupported (passive observation supports both).
Only new connections are redirected. The proxy changes the client source
address seen by the upstream application. This is an active proxy, not passive
packet capture; TLS still passes through without termination.

- Initial connection delay: 0–30,000 ms, once before upstream connect.
- Bandwidth: 0 (unchanged) or up to 1,048,576 KiB/s per connection and direction.
- Packet loss: 0–100%, real packet drops in mangle before NAT, in the selected
  direction. TCP retransmission and connection setup are affected.
- Reset: optional abort after 1–600,000 ms of an established proxy connection.
- Fixed experiments retain a 5–600 second deadline.
- Interactive sessions use a renewable 5–600 second lease (FarshMac uses 30 seconds).
  They run until the user closes the window; renewal stops on disconnect.
- Interactive actions address one connection ID: reset immediately, set or clear
  per-chunk delay, per-direction bandwidth, and real client-to-destination packet loss.
  Delay is per forwarded chunk (up to 16 KiB), not a precise packet-latency simulator.

A monitor shows accepted/rejected connections, sent/received bytes, resets and
recent connections. Only the first HTTP/1 request and response header in each
connection is inspected. Bodies, authorization/cookie headers and query strings
are not retained. HTTP/2, gRPC and subsequent HTTP keep-alive messages are not
parsed. There is no PCAP export in this version.

## Ownership and cleanup

A pod-wide abstract Unix socket prevents multiple PodToast traffic experiments,
including agents in separate PID namespaces. Proxy sockets carry a mark to
exclude them from the redirect/drop rules and avoid loops. Up to 128 concurrent
connections and 256 total recent/active records are retained; stream buffers are bounded.

An independent watchdog owns **all** rule installation and removal. It journals
exact inverse operations in a private temporary directory, retains the pod-wide
lock, and removes only its own rules/chains after stop, control-stream EOF,
controller crash or expiry. Partial installation rolls back through the same
owner, so a late installation cannot race cleanup. It never flushes a shared
chain or restores an old snapshot over another controller's changes. Cleanup
errors are surfaced and the journal is retained for diagnosis.

The watchdog must remain running; simultaneous termination of it and the proxy
cannot guarantee cleanup in a still-running network namespace. Kernel/iptables
failures may also prevent cleanup. Existing service-mesh/CNI rules can change
which traffic reaches the hooks; support is not claimed for every mesh or eBPF
routing mode. Test selected flows before relying on an experiment.

## FarshMac

Select a pod/container, then **F11 / Alt+Enter → Traffic monitoring / faults…**.
The Start form specifies the image and explicit attachment permission. Opening the window starts passive monitoring; direction, port and peer CIDR are selected separately through Test network conditions. Only Start may
attach a network agent, and only when its explicit attachment checkbox is set.
An existing running PodToast with NET_ADMIN and the selected image is reused. Older images without the
traffic API return an actionable compatibility error.

The network agent uses the Network profile (NET_ADMIN/NET_RAW), not privileged
mode. RBAC/admission must allow this profile. FarshMac rejects hostNetwork and
hostPID pods. The monitor opens in an independent graphical macOS window with the same palette
as the metrics dashboard. It continues when the parent file-manager window closes.
The graphs show sent/received rates and concurrent connections, with a 1/3/5-minute
view and human-readable units. History is bounded to 300 samples. A selected
connection can have its own rate graph; collection begins when it is selected.

Search filters addresses and the first HTTP exchange. Open-only filtering is
available. Every connection row has Conditions, Block/Unblock, Clear and Reset buttons.
The port toolbar controls all matching traffic in the selected port/direction/CIDR.
Port delay and per-connection rate limits apply to current and future proxied
connections. Port loss also affects matching packets before proxy redirection,
including connection establishment. Reset open connections resets only current
monitored flows; it does not prevent new connections.

Port conditions are a baseline. Connection delay adds to port delay; rate uses
the lower nonzero per-connection/per-direction limit (not an aggregate port rate).
Independent packet-loss rules compound: effective loss is
`1 - (1 - port_loss) * (1 - connection_loss)`.
Clearing one scope preserves the other. The table displays both scopes. Clearing
or changing a queued delay/rate limit immediately re-evaluates waiting chunks.
A conditions dialog retains its original target even when the table updates.

Each monitor still selects one destination port. Select a connection to use the keyboard:

- F2: delay, bandwidth and packet loss for the selected connection.
- F4: block/unblock the selected connection's client packets (100% loss).
- F6: clear all faults on that connection.
- F8: immediately reset that connection.
- Esc / Cmd+W: stop, clean up and close (Esc first dismisses a conditions dialog).

Closing the application also closes the exec stream and triggers watchdog cleanup.
Only new matching TCP connections are proxied; existing connections are not
retroactively discovered or controlled. This is not a complete pod packet inventory.

## API

- GET `/v1/hello`: `traffic: true`, `interactiveTraffic: true`, `portTraffic: true`, `networkScope: "pod"`.
- POST `/v1/traffic/start`: validated TrafficConfig; returns TrafficStatus.
- GET `/v1/traffic`: bounded statistics and current lifecycle state.
- POST `/v1/traffic/renew`: extend an interactive lease and return current status.
- POST `/v1/traffic/action`: `{ "id": 1, "action": "faults", "faults": { "delayMS": 100, "rateKiB": 512, "lossPercent": 2 } }`.
  Actions `clear` and `reset` also require an active connection ID.
  Port actions use `{ "scope": "port", "port": 8080, "action": "faults", "faults": { "lossPercent": 10 } }`;
  the port must match the session and ID must be zero/omitted. Empty scope retains
  connection-level compatibility. Status includes `portFaults`.
- POST `/v1/traffic/stop`: remove rules, stop proxy connections and return status.

## Tests

Unit tests cover scope validation, inverse rules and HTTP privacy. An opt-in
Docker test in its own network namespace with NET_ADMIN verifies redirection,
initial delay, bandwidth limiting, 100% loss, reset, deadline, process-kill
cleanup, exclusion of overlapping experiments and preservation of unrelated
chains, selected-flow isolation, live fault changes, renewable leases and lease expiry. Both amd64 and arm64 CI runners execute it. FarshMac also exercises the
API through an ephemeral container in a disposable kind cluster.

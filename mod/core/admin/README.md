# Admin

Package `admin` is the administrative-socket adapter used by `mod/core`.
The current implementation is intentionally a thin pass-through to
`github.com/yggdrasil-network/yggdrasil-go/src/admin`; it is not a security
boundary and does not attempt to correct upstream lifecycle or concurrency
behavior.

`mod/core` uses this package through a small API for starting and stopping the
socket and attaching the multicast diagnostic handler. Construction parameters
are grouped in `ConfigObj` so future adapter settings can be added without
changing the `New` signature. Keeping the adapter in a separate package allows a
future implementation to replace the upstream socket without spreading
admin-specific code through `mod/core`.

Direct use of this package is possible because it is a normal Go package, but it
is not an intended standalone integration surface. A caller that imports it
directly accepts the same risks and lifecycle constraints as a caller that
enables it through `core.EnableAdmin`.

## Security and stability warning

The current adapter inherits the upstream implementation's unsafe behavior:

- There is no authentication, authorization, or transport encryption.
- Bind failures, an occupied Unix socket, and Unix-socket cleanup failures can
  terminate the host process through `os.Exit(1)`.
- The listener starts before all handlers are registered. An early request can
  race with writes to the upstream handler map and crash the process.
- Attaching handlers after the socket starts, including the multicast diagnostic
  handler, can race with request processing.
- Upstream ignores duplicate multicast-handler registration. After multicast is
  restarted, its admin command can remain attached to the previous instance and
  report stale state until the admin socket is restarted.
- Requests have no read or write deadline and no JSON size limit. A client can
  retain goroutines indefinitely or cause excessive memory use.
- Persistent `Accept` errors have no retry backoff and can cause a CPU spin.
- Stopping the socket closes the listener but does not close already accepted
  keepalive connections. `Stop` and `core.DisableAdmin` are therefore not access
  revocation barriers for existing clients.
- Administrative handlers expose privileged diagnostics and peer-management
  operations. `SetAdmin` also exposes upstream construction-time handlers and is
  unsafe with untrusted implementations or concurrent registration.

Use the admin socket only as trusted operational tooling. Prefer a protected
Unix socket. If TCP is unavoidable, bind only to a protected loopback endpoint.
Do not expose it to an untrusted network or client, and do not assume that a
successful `Stop` has terminated existing sessions.

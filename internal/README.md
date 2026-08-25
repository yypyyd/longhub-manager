# Internal packages

`internal` contains LongHub Manager implementation packages that are not a
public Go API. The executable in `cmd/longhub-manager` composes them into the
loopback-only Windows desktop control plane.

- `runtime`: native OpenClaw discovery, bounded CLI inventory and typed local management.
- `configbackup`: opaque, integrity-checked config snapshots and atomic restore.
- `httpapi`: local authentication, structured APIs and the embedded WebView UI.
- `manageragent`: Gateway-independent Agent, model client, credentials and approval state.
- `managerupdate`: signed update verification, state and recovery.

Package-specific design and usage details live in each active package's
README/DESIGN files and the repository-level `README.md`, `DESIGN.md` and
`PRIVACY.md`. Empty legacy directories are retained only where older build or
migration tooling still expects the path; they must not regain Cloud code or
credentials in the Manager repository.

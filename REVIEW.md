
## Dependency Vulnerability Remediation
- Upgraded core container tooling dependencies to patched builds:
  - `github.com/docker/docker` → v28.0.0+incompatible (fixes GO-2025-3829, GO-2024-3110, GO-2022-0985, etc.).
  - `github.com/containerd/containerd` → v1.6.38 (fixes GO-2025-3528).
  - `github.com/opencontainers/runc` → v1.1.14 (fixes GO-2024-3110).
- Added supporting module bumps introduced by the upgrades (`github.com/moby/patternmatcher`, `github.com/moby/sys/*`, updated Microsoft runtimes, etc.).
- Confirmed `task go:sec:vuln` now reports zero reachable vulnerabilities.
- Re-ran `task go:test --force` to validate runtime compatibility with the new versions.


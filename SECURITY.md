# Security Policy

## Supported Versions

Only the latest release of Rack Gateway gets security fixes. Upgrade to it before reporting, and check whether the
problem is still there.

## Reporting a Vulnerability

Please don't report security vulnerabilities in public GitHub issues, pull requests or discussions.

Report them privately, either way:

- **GitHub:** [Report a vulnerability](https://github.com/DocSpring/rack-gateway/security/advisories/new) on the
  repository's Security tab
- **Email:** [security@docspring.com](mailto:security@docspring.com)

Include:

- The version (or commit) you tested
- What an attacker can do, and what they need first (an account, a role, network access)
- Steps or a proof of concept that reproduce it
- Any configuration it depends on

We'll acknowledge your report, keep you updated while we fix it, and agree a disclosure date with you. We credit
reporters in the release notes and the advisory unless you'd rather we didn't.

## Scope

In scope: the gateway server, the `rack-gateway` CLI, the web UI and the release artifacts (Docker image, CLI
archives) published from this repository.

Out of scope: Convox itself (report those to [Convox](https://github.com/convox/convox)), and problems that need a
misconfiguration the documentation warns against, such as exposing the gateway without TLS or running a test build
(`DEV_MODE`, `E2E_TEST_MODE`) in production.

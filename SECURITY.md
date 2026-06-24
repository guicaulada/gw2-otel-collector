# Security Policy

## Supported versions

This project is pre-1.0 and ships from `main`. Security fixes are applied to the
latest commit on `main`; there are no maintained release branches.

## Reporting a vulnerability

**Please do not open a public issue for security problems.**

Report privately through GitHub's
[private vulnerability reporting](https://github.com/guicaulada/gw2-otel-collector/security/advisories/new)
(Security → Advisories → "Report a vulnerability"). That opens a private channel
where details can be shared and a fix coordinated before any public disclosure.

Please include enough to reproduce: affected version/commit, configuration, and
the impact you observed.

## Scope notes

- The collector reads a **read-only** GW2 account API key from the environment
  (`GW2_API_KEY`) and never logs or exports it. Keys, OTLP headers, and Grafana
  Cloud credentials are passed via environment variables and are never committed —
  please report any case where a secret could leak into metrics, logs, traces, or
  the repository.
- This is personal-infrastructure software with no warranty; see [LICENSE](LICENSE).

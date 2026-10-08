# Security policy

## Reporting a vulnerability

Please **do not open a public issue** for a security problem. Report it
privately through GitHub instead:
[**Report a vulnerability**](https://github.com/drjzlyan/dhi/security/advisories/new).

Include what you found, how to reproduce it, and the impact you see. You
will get an acknowledgement within a few days. Fixes are released as soon
as they are ready, and reporters are credited unless they prefer otherwise.

## Supported versions

Security fixes go into the latest release. Upgrade with the install command
in the [README](README.md#install).

## What is in scope

DHI runs coding agents on your machine, so these matter most:

- an agent escaping its workspace, scopes or approvals (the sandbox, the
  path jail, the tool allowlist);
- credential handling (`internal/credstore`, MCP credentials);
- the installer and the toolchain downloads: checksums, cosign signatures
  and the pinned manifest.

Agents run on a host coding CLI that keeps its own native tools. DHI states
that this containment is best-effort (see ADR-0024). Reports that improve it
are welcome.

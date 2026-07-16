# Security Policy

## Contents

- [Supported versions](#supported-versions)
- [Reporting a vulnerability](#reporting-a-vulnerability)
- [Useful report details](#useful-report-details)
- [Response and disclosure](#response-and-disclosure)
- [Security boundaries](#security-boundaries)
- [Out of scope](#out-of-scope)
- [Safe harbor](#safe-harbor)

## Supported versions

Security fixes target the latest released minor version and the default branch. Reproduce a suspected issue on the
newest release before reporting it when doing so is safe.

## Reporting a vulnerability

Email **git@sunsung.fun** with the subject `SECURITY: <short summary>`. Do not open a public issue, discussion, or pull
request and do not publish a proof of concept before coordinated disclosure.

If email itself is part of the issue, send only enough information to establish contact and request another private
channel.

## Useful report details

Include what is available and relevant:

- affected Ratatoskr and Go versions;
- operating system, architecture, and Yggdrasil topology;
- exposed listener type and whether the caller is trusted;
- configuration values that affect limits, timeouts, peers, NodeInfo, forwarding, SOCKS, multicast, or admin;
- expected and observed behavior;
- minimum reproduction steps or a small proof of concept;
- impact on confidentiality, integrity, availability, process control, or network boundaries;
- logs, stack traces, race reports, profiles, or packet captures with private keys and credentials removed;
- known mitigations and whether the issue is already public.

Never send Yggdrasil private keys, production credentials, or unrelated personal data.

## Response and disclosure

The project will:

- acknowledge a report within 72 hours;
- provide an initial assessment or mitigation plan within 14 days;
- aim to release a fix or documented mitigation within 90 days, subject to complexity and upstream coordination;
- coordinate public disclosure and credit the reporter unless anonymity is requested.

Keep the report private until a fix or agreed disclosure date. Security changes are published in release notes and, when
appropriate, a public advisory.

## Security boundaries

Ratatoskr is an embeddable networking library. The embedding application remains responsible for authentication,
authorization, listener exposure, peer trust, identity storage, rate limits, and host-level isolation.

The following contracts are deliberate and documented:

- `mod/core/admin` is an unsafe pass-through to the upstream Yggdrasil admin implementation. It is unauthenticated and
  can race, retain accepted connections, or terminate the process on selected errors. Do not expose it to untrusted
  users.
- `mod/forward` uses unlimited TCP connections and UDP sessions when its maximum fields are zero. Public mappings must
  set explicit limits appropriate to the deployment.
- Negative values disable selected SOCKS, resolver, and timeout limits as documented by each field and module README.
- NodeInfo is public network metadata. Do not put secrets in base NodeInfo, sigils, or custom fields.
- A Yggdrasil private key is the node identity. Its disclosure permits identity impersonation.

A report is in scope when Ratatoskr violates one of its documented boundaries, bypasses configured validation or
limits, leaks data across callers, corrupts ownership, or permits unintended process or network control.

## Out of scope

- the documented admin limitations without a new Ratatoskr-specific boundary violation;
- denial of service that depends only on deliberately unlimited configuration and has no limit bypass;
- vulnerabilities solely in an upstream dependency without a Ratatoskr-specific exploit path or mitigation;
- unsupported releases;
- social engineering, physical attacks, or attacks on infrastructure outside this project;
- automated scanner output without a reproducible impact;
- reports that require access already equivalent to full control of the embedding process.

Upstream issues should be reported to the responsible project. A report remains useful here when Ratatoskr exposes the
upstream issue in an unexpected way or can reasonably contain it at its own boundary.

## Safe harbor

Good-faith research that follows this policy, minimizes access and disruption, avoids persistence, protects user data,
and allows time for coordinated disclosure will not prompt legal action from this project.

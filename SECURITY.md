# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| latest  | Yes       |

Only the latest release receives security patches.

## Reporting a Vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

To report a vulnerability, use [GitHub's private vulnerability reporting](https://github.com/laplacef/bsky-comments-proxy/security/advisories/new). You can expect an initial response within 72 hours.

Please include:
- A description of the vulnerability
- Steps to reproduce the issue
- Potential impact assessment

## Scope

The following are considered security issues:
- Server-side request forgery through unvalidated upstream paths or parameters
- Cache poisoning, cache key collisions, or serving one reader's response to another
- CORS or header handling that exposes origin internals or reader IP addresses
- Dependency vulnerabilities in direct dependencies or the container base images

## Out of Scope

The following are **not** security issues:
- Availability or rate limiting of Bluesky's upstream endpoints
- Vulnerabilities in Bluesky itself
- Resource exhaustion from traffic volumes the deployment was not sized for

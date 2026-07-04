# Changelog

All notable changes to **evo-ai-core-service-community** will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v1.0.0-rc6] - 2026-07-04

Feature release — server-side advanced filtering across the list endpoints, a rebuilt Custom Tool test endpoint, a standalone community image build, and enterprise multi-tenancy extension points that remain no-ops in the community build.

### Added

- **Advanced filtering (server-side)** on the **Agents** list endpoint (EVO-1952) and on the **Custom Tools** and **Custom MCP Servers** list endpoints (EVO-1953). The filter payload mirrors the CRM's advanced filtering: whitelisted filter keys, fully parameterized SQL clauses, array-aware tag matching, and the same filters applied to both List and Count so pagination totals stay consistent.
- **Enterprise multi-tenancy extension points** (EVO-1621, EVO-1623, EVO-1624, EVO-1625) and a **license-guardian boot hook** (EVO-1989), all behind build tags — no-ops with zero behavior change in the community build.

### Changed

- **CI** now builds and publishes `:pr-N` / `:sha-*` images for internal pull requests, feeding the review environment (EVO-1998). PR builds are gated to internal PRs (forks have no secrets).

### Fixed

- **Custom Tool Test endpoint** is now content-type-agnostic (EVO-1790): it returns the real upstream status code, response time, headers and raw body for any content type, supports 7 HTTP methods, and includes an SSRF guard.
- **Community image build** no longer breaks on `go mod download`: the enterprise `replace` directive in `go.mod` is bypassed via a dedicated `go.community.mod`, making the community image build standalone (EVO-1998).

### Notes

- **No new migrations** — the sequence still ends at `000015` (a `000016` migration was added and removed within the same PR) — and **no new environment variables** for the community edition.
- **Build note**: outside the official Dockerfile, build with `go build -modfile=go.community.mod ./cmd/api`.

## [v1.0.0-rc5] - 2026-05-27

No code changes — version bump to keep the CRM Community family aligned at v1.0.0-rc5. Source tree is identical to v1.0.0-rc4.

## [v1.0.0-rc4] - 2026-05-25

No functional changes. Tag issued to keep the CRM Community family aligned on a single release-candidate version.

## [v1.0.0-rc3] - 2026-05-17

Integration release — adds the `pkg/evoextensions` contract (no-op extension points for the future Enterprise edition), exposes a proxy to list Knowledge Spaces from Nexus from within the agent builder, and standardizes docs/branding for Evolution Foundation 2026.

### Added

- **`pkg/evoextensions`** — three no-op interfaces published as an extension point (EVO-1285). The contract is versioned in `EXTENSION_POINTS.md` and allows the Enterprise edition to inject implementations without forking.
- **Knowledge Nexus — spaces listing proxy** in `/agent-integrations`. Backend endpoint that queries the Nexus API and returns the list of available spaces, consumed by the Knowledge Nexus selector in the frontend Agent Builder.

### Changed

- **Docs** standardized for Evolution Foundation 2026 (README, LICENSE, NOTICE, TRADEMARKS).
- **Docs (org)** — GitHub URLs updated from `EvolutionAPI` to `evolution-foundation`.

### Fixed

- N/A

## [v1.0.0-rc2] - 2026-05-05

Release with no functional changes in this service — only pipeline / staging adjustments.

### Changed

- **CI**: workflow now also publishes `develop` images to staging. (#2)

## [v1.0.0-rc1] - 2026-04-24

### Added

- First public release candidate of `evo-ai-core-service-community`.
- Agent management API (`/agents`, `/apikeys`, `/folders`).
- Integration with `evo-auth-service` for Bearer token validation.

---

[Unreleased]: https://github.com/evolution-foundation/evo-ai-core-service-community/compare/v1.0.0-rc6...HEAD
[v1.0.0-rc6]: https://github.com/evolution-foundation/evo-ai-core-service-community/compare/v1.0.0-rc5...v1.0.0-rc6
[v1.0.0-rc5]: https://github.com/evolution-foundation/evo-ai-core-service-community/compare/v1.0.0-rc4...v1.0.0-rc5
[v1.0.0-rc4]: https://github.com/evolution-foundation/evo-ai-core-service-community/compare/v1.0.0-rc3...v1.0.0-rc4
[v1.0.0-rc3]: https://github.com/evolution-foundation/evo-ai-core-service-community/compare/v1.0.0-rc2...v1.0.0-rc3
[v1.0.0-rc2]: https://github.com/evolution-foundation/evo-ai-core-service-community/compare/v1.0.0-rc1...v1.0.0-rc2
[v1.0.0-rc1]: https://github.com/EvolutionAPI/evo-ai-core-service-community/releases/tag/v1.0.0-rc1

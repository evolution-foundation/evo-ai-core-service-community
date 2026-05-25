# Changelog

All notable changes to **evo-ai-core-service-community** will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- N/A

### Changed

- N/A

### Fixed

- N/A

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

[Unreleased]: https://github.com/evolution-foundation/evo-ai-core-service-community/compare/v1.0.0-rc4...HEAD
[v1.0.0-rc4]: https://github.com/evolution-foundation/evo-ai-core-service-community/compare/v1.0.0-rc3...v1.0.0-rc4
[v1.0.0-rc3]: https://github.com/evolution-foundation/evo-ai-core-service-community/compare/v1.0.0-rc2...v1.0.0-rc3
[v1.0.0-rc2]: https://github.com/evolution-foundation/evo-ai-core-service-community/compare/v1.0.0-rc1...v1.0.0-rc2
[v1.0.0-rc1]: https://github.com/EvolutionAPI/evo-ai-core-service-community/releases/tag/v1.0.0-rc1

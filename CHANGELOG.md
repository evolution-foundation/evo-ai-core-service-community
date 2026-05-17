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

## [v1.0.0-rc3] - 2026-05-17

Release de integração — adiciona o contrato `pkg/evoextensions` (extension points no-op para a futura Enterprise edition), expõe um proxy para listar Knowledge Spaces do Nexus a partir do builder de agentes, e padroniza docs/branding para Evolution Foundation 2026.

### Added

- **`pkg/evoextensions`** — três interfaces no-op publicadas como ponto de extensão (EVO-1285). O contrato fica versionado em `EXTENSION_POINTS.md` e permite que a Enterprise edition injete implementações sem fork.
- **Knowledge Nexus — proxy de listagem de spaces** em `/agent-integrations`. Endpoint backend que consulta a API do Nexus e devolve a lista de spaces disponíveis, consumido pelo seletor de Knowledge Nexus no Agent Builder do frontend.

### Changed

- **Docs** padronizados para Evolution Foundation 2026 (README, LICENSE, NOTICE, TRADEMARKS).
- **Docs (org)** — URLs do GitHub atualizadas de `EvolutionAPI` para `evolution-foundation`.

### Fixed

- N/A

## [v1.0.0-rc2] - 2026-05-05

Release sem mudanças funcionais neste serviço — apenas ajustes de pipeline / staging.

### Changed

- **CI**: workflow agora também publica imagens `develop` para staging. (#2)

## [v1.0.0-rc1] - 2026-04-24

### Added

- Primeiro release candidate público do `evo-ai-core-service-community`.
- API de gerenciamento de agentes (`/agents`, `/apikeys`, `/folders`).
- Integração com `evo-auth-service` para validação de Bearer tokens.

---

[Unreleased]: https://github.com/evolution-foundation/evo-ai-core-service-community/compare/v1.0.0-rc3...HEAD
[v1.0.0-rc3]: https://github.com/evolution-foundation/evo-ai-core-service-community/compare/v1.0.0-rc2...v1.0.0-rc3
[v1.0.0-rc2]: https://github.com/evolution-foundation/evo-ai-core-service-community/compare/v1.0.0-rc1...v1.0.0-rc2
[v1.0.0-rc1]: https://github.com/EvolutionAPI/evo-ai-core-service-community/releases/tag/v1.0.0-rc1

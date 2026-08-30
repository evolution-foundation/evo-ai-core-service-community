# Changelog

All notable changes to **evo-ai-core-service-community** will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **`PUT /agents/:id` now merges the config instead of replacing it** (CRM-305).
  The update used to rebuild the agent config from the request alone — only
  `api_key` survived — so a one-toggle edit wiped `custom_tool_ids`,
  `mcp_servers`, `tools`, `message_wait_time`, the segmentation settings,
  `inactivity_actions` and every other stored key, answering **200** with no
  warning. A request with no `config` at all left the agent holding just its
  `api_key`.
  - **Contract:** a key the request sends wins, a key it omits is preserved,
    and clearing is explicit — send `null` for a scalar or `[]` for a list.
    Absence means "keep". This is a behavior change for any client that
    relied on the old replace semantics to drop keys by omission.
  - **Merged as stored, not revalidated:** stored values were already
    validated when they were written, and re-resolving stored `mcp_servers`
    would fail an unrelated toggle update whenever a referenced server had
    since left the catalog.
  - **Effective-view validation:** `preload_memory` checks the `load_memory`
    the agent already carries, and an `external` agent that does not resend
    `provider` keeps (and revalidates) the stored one — otherwise a valid
    partial update would be rejected.
  - **Read-only expansions stay out of the write.** `custom_tools` and
    `custom_mcp_servers` are hydrated in memory on every read (EVO-2126); the
    merge drops them from the stored config it merges from, so an update never
    persists a frozen tool copy. Persisting one would also disable the
    hydration guard for good, pinning the agent to a stale tool definition.
- **`DELETE /integration-credentials/:id` now removes the row** (CRM-191). It
  used to set `is_active = false` like the deactivate toggle, so the encrypted
  value never left the database and its `(scope, name)` / `(owner_store,
  owner_ref)` stayed taken. The delete is a hard delete (the model has no
  `deleted_at`).
  - **Consumer guard:** unlike `api_key`, no FK protects this table — a
    credential's id can sit inside a jsonb column on custom tools, MCP
    servers, agents, agent integrations, or bots. Deleting one still in use
    now answers **409**, naming the consumers in `error.details.consumers`,
    instead of leaving a dangling reference that only fails the next time the
    tool runs. The guard **fails closed**: a store that cannot be read refuses
    the delete rather than reporting "unused". Only a store genuinely absent
    from this database (older CRM half) is skipped, and the catalog decides
    that, not a failed query.
  - **OAuth guard:** a `kind='oauth'` row points at its connection by
    `(owner_store, owner_ref)`, never through the jsonb refs, so no consumer
    ever held it. Deleting one while the connection is live now answers
    **409** — before, the row was removed and the listing sync recreated it
    on the next page load under a NEW id, orphaning every reference that
    named the old one. Disconnect the integration first.
  - **Contract change:** deleting a credential that does not exist, or is
    already gone, answers **404** — never the previous 200/204, and no longer
    the 500 a plain error produced.
  - **Known limit:** a consumer that starts referencing the credential between
    the guard and the delete still ends up dangling. Closing that window means
    rewriting the consumers' jsonb in the same transaction, which CRM-191
    deferred.
- **`DELETE /agents/apikeys/:id` now removes the row** (CRM-186). It used to set
  `is_active = false` — indistinguishable from the deactivate toggle — so the
  encrypted key never left the database and the name stayed taken by the
  `(name, tenant_id)` unique. The delete is a hard delete (the model has no
  `deleted_at`); agents pointing at the key keep existing with
  `api_key_id = NULL` (`ON DELETE SET NULL`).
  - **Contract change:** deleting a key that does not exist, or is already
    gone, answers **404** — never the previous 200/204, and no longer the 500
    a plain error produced. An account-level caller still meets the
    installation-scope gate's fail-closed **403** when the target cannot be
    read to decide its scope.
  - Rows already `is_active = false` are a mix of "deleted" and "deactivated"
    that no migration can tell apart; clean them up through the credentials
    screen.
- **`PUT /agents/apikeys/:id` answers 404 for an unknown key** instead of 500.
  It replaced the mapped lookup error with a plain one, which the error handler
  could only read as an internal error.

### Fixed

- **`POST` / `PUT` / `DELETE /api/v1/mcp-servers` now reach their handlers.**
  They answered **401** `{"error":"User is not an admin"}` for every caller,
  `super_admin` included, so the whole write surface of the MCP Servers
  feature was unusable. The admin gate read `is_admin` from gin's key map
  with `c.GetBool`, and nothing in the service ever wrote that key: the sole
  assignment lives in `internal/middleware/jwt.go`, which puts it on the
  request context instead, and that middleware has no call sites. Two
  different stores plus no writer means the boolean was unconditionally
  false — a permanently closed door rather than an unenforced policy.
  - **The gate is deleted, not repaired.**
    `internal/middleware/user_admin.go` is removed and
    `RequirePermission("ai_mcp_servers", <action>)` remains the enforcement,
    matching the other write routes in this service and the auth service's
    RBAC model, whose seed grants all five `ai_mcp_servers` actions to
    `super_admin` and to `account_owner` — the two keys withheld from
    `account_owner` are `accounts.stats` and `installation_configs.manage`,
    withheld from the catalog as a whole, neither of them an MCP key.
    Repairing the gate would have meant inventing a local role authority in
    Go, which would also refuse custom roles that legitimately hold the
    permission.
  - **Contract:** this is a security-relevant behavior change — the routes
    become reachable for the first time. They are not open: every one of
    them still requires the auth service to grant the matching
    `ai_mcp_servers` permission. An operator who was relying on the hard
    401 as a block should revoke that permission instead.

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

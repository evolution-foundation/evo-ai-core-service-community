# Extension Points

**Contract version:** `1.0.0` (SemVer)

This document is the public contract between `evo-ai-core-service-community`
and any external consumer that wants to plug into the Go service without
forking or patching community source. The authoritative architectural
decision behind this contract is **ADR13 — Extension Points Versioning
Strategy**; the rules below are self-contained.

The community release is fully usable on its own. Every extension point
ships with a working no-op default implementation under
`pkg/evoextensions/...`; a consumer may replace the default at process
start by providing its own implementation of the interface.

If you are about to change any of the three interfaces below, read the
[Compatibility Promise](#compatibility-promise) first.

---

## Compatibility Promise

Each extension point is versioned independently and treated as a public
Go API, with the same backward-compatibility rules as the REST `/v1/*`
endpoints exposed by the service:

- **Backward compatibility is forever.** Once shipped at `v1.0.0`, the
  name, method set, parameter types and return types of an extension
  point do not change silently.
- **Breaking changes require a major bump** of the affected extension
  point and of the community release that ships them.
- **Deprecation window is at least one minor release.** The old shape
  keeps working alongside the new one. Deprecations are signalled by
  the standard Go `// Deprecated:` doc comment on the affected
  declaration so `go vet` and IDEs surface them.
- **Additive changes are minor bumps.** Adding a new interface, a new
  optional helper, or a new accepted input value.
- **Bug fixes that preserve the contract are patch bumps.**

Bumping one extension point does not bump the others.

---

## Extension points

The three interfaces below live under the `pkg/evoextensions/` import
path of the Go module. Sub-packages outside the three listed here are
private and may change without notice.

### 1. `feature.Gate`

**Version:** `1.0.0`
**Import path:** `evo-ai-core-service/pkg/evoextensions/feature`

```go
type Gate interface {
    Enabled(name string) bool
}

func Default() Gate // no-op: always returns true
```

**Default behaviour.** `Default()` returns a gate whose `Enabled`
always reports `true`. The community release ships with no feature
gating; every flag is considered enabled.

Override (consumer wires its own gate at process start):

```go
import "evo-ai-core-service/pkg/evoextensions/feature"

var gate feature.Gate = myConsumerGate{} // implements feature.Gate
```

**Breaking-change policy.** Renaming `Gate`, `Enabled`, `Default`, or
changing the parameter / return type of `Enabled` is a major bump.
Adding new interface methods to `Gate` is a major bump (it would force
every existing implementation to update). Adding new sibling
constructors (for example a `New(...)` helper) is a minor bump.

### 2. `tenant.Context`

**Version:** `1.0.0`
**Import path:** `evo-ai-core-service/pkg/evoextensions/tenant`

```go
type Context interface {
    CurrentID(ctx context.Context) string
}

func Default() Context // no-op: always returns ""
```

**Default behaviour.** `Default()` returns a context whose `CurrentID`
always reports the empty string. The community release runs in
single-tenant mode by default.

Override:

```go
import "evo-ai-core-service/pkg/evoextensions/tenant"

var tc tenant.Context = myConsumerTenant{} // implements tenant.Context
```

The returned string is opaque to the community release; the empty
string means "no tenant bound".

**Breaking-change policy.** Renaming `Context`, `CurrentID`, `Default`,
or changing the parameter / return type of `CurrentID` is a major bump.
Adding new interface methods is a major bump. Adding new sibling
helpers is a minor bump.

### 3. `plugin.Registry`

**Version:** `1.0.0`
**Import path:** `evo-ai-core-service/pkg/evoextensions/plugin`

```go
type Registry interface {
    Discover() []string
}

func Default() Registry // no-op: always returns nil
```

**Default behaviour.** `Default()` returns a registry whose `Discover`
always reports `nil` (an empty set of plugins). The community release
itself registers nothing; the registry is intentionally **read-only**
in this contract — a public mutation API is not part of `v1.0.0`.

Override:

```go
import "evo-ai-core-service/pkg/evoextensions/plugin"

var reg plugin.Registry = myConsumerRegistry{} // implements plugin.Registry
```

**Breaking-change policy.** Renaming `Registry`, `Discover`, `Default`,
or changing the return type of `Discover` (for example, from
`[]string` to a richer struct slice) is a major bump. Adding read-only
methods to the interface is a major bump. Exposing a public write API
on the contract is a minor bump (additive).

---

## How to use as a consumer

Each extension point is independently overridable; a consumer picks
only what it needs. The three mini-examples below are intentionally
isolated.

Feature gate:

```go
import "evo-ai-core-service/pkg/evoextensions/feature"

type myGate struct{}

func (myGate) Enabled(name string) bool { return true }

var _ feature.Gate = myGate{}
```

Tenant context:

```go
import (
    "context"

    "evo-ai-core-service/pkg/evoextensions/tenant"
)

type myTenant struct{}

func (myTenant) CurrentID(context.Context) string { return "" }

var _ tenant.Context = myTenant{}
```

Plugin registry:

```go
import "evo-ai-core-service/pkg/evoextensions/plugin"

type myRegistry struct{}

func (myRegistry) Discover() []string { return nil }

var _ plugin.Registry = myRegistry{}
```

Wiring a custom implementation into the running service is owned by
the consumer's bootstrap code and intentionally out of scope of this
contract; the contract is the **interface set**, not the wiring.

A consumer is expected to declare the community module version range
it supports in its own `go.mod` via standard Go module versioning.

---

## Cross-references

- Backend extension points (Ruby on Rails): see
  [`EXTENSION_POINTS.md` in `evo-ai-crm-community`](https://github.com/evolution-foundation/evo-ai-crm-community/blob/develop/EXTENSION_POINTS.md).
- Frontend extension points (React): see
  [`EXTENSION_POINTS.md` in `evo-ai-frontend-community`](https://github.com/evolution-foundation/evo-ai-frontend-community/blob/develop/EXTENSION_POINTS.md).
- The architectural decision behind the SemVer-per-extension-point
  strategy is **ADR13 — Extension Points Versioning Strategy**. The ADR
  is maintained in an internal planning workspace and is not checked
  into this repository; the relevant rules from it are restated in the
  [Compatibility Promise](#compatibility-promise) above so this
  document can be read on its own.

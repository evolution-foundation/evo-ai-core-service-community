//go:build !enterprise

// Package tenantfield exposes a build-tagged TenantField struct embedded
// by the 8 evo_core_* GORM models. In the community build the struct is
// empty — the models compile and INSERT works without a tenant_id column.
// In the enterprise build (field_enterprise.go) the struct carries the
// TenantID field with the gorm column tag; the gem-ruby migrations add
// the physical column to the shared Postgres, and the tenantstamp plugin
// stamps it on INSERT.
//
// This split lets community deployments stay single-tenant with zero
// schema changes while enterprise keeps its multi-tenant isolation, and
// avoids duplicating each evo_core_* model struct twice (once per build).
package tenantfield

// TenantField is the zero-cost community variant. Embedding it adds no
// columns and no fields to the host struct.
type TenantField struct{}

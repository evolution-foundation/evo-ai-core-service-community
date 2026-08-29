//go:build !enterprise

// Package tenantfield exposes a build-tagged TenantField struct embedded
// by the 8 evo_core_* models. In the community build the struct is empty:
// the models compile and INSERT works with no tenant_id column. The
// enterprise build (field_enterprise.go) carries the field, and the
// enterprise migrations add the physical column.
//
// The split keeps community single-tenant with zero schema changes without
// duplicating each model struct per build.
package tenantfield

// TenantField is the zero-cost community variant. Embedding it adds no
// columns and no fields to the host struct.
type TenantField struct{}

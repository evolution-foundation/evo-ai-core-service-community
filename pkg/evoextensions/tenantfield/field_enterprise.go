//go:build enterprise

package tenantfield

import "github.com/google/uuid"

// TenantField is the enterprise variant embedded by the 8 evo_core_*
// models. The gorm column tag matches the column the gem-ruby
// migrations add to each table; the tenantstamp plugin populates it
// on every INSERT via Schema.LookUpField("tenant_id").
type TenantField struct {
	TenantID uuid.UUID `json:"-" gorm:"column:tenant_id;type:uuid;not null;default:'00000000-0000-0000-0000-000000000000'"`
}

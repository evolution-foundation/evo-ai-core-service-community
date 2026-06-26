package repository

import (
	"testing"

	"evo-ai-core-service/pkg/custom_tool/model"
)

func TestCustomToolFilterFragment(t *testing.T) {
	cases := []struct {
		name     string
		filter   model.CustomToolListFilter
		wantSQL  string
		wantArgs []interface{}
		wantOK   bool
	}{
		{
			name:   "unknown attribute is rejected (whitelist)",
			filter: model.CustomToolListFilter{AttributeKey: "headers", FilterOperator: "contains", Values: []string{"x"}},
			wantOK: false,
		},
		{
			name:     "name contains",
			filter:   model.CustomToolListFilter{AttributeKey: "name", FilterOperator: "contains", Values: []string{"weather"}},
			wantSQL:  "name ILIKE ?",
			wantArgs: []interface{}{"%weather%"},
			wantOK:   true,
		},
		{
			name:     "method equal_to is case-insensitive",
			filter:   model.CustomToolListFilter{AttributeKey: "method", FilterOperator: "equal_to", Values: []string{"post"}},
			wantSQL:  "LOWER(method) = LOWER(?)",
			wantArgs: []interface{}{"post"},
			wantOK:   true,
		},
		{
			name:     "tags equal_to is case-insensitive array membership",
			filter:   model.CustomToolListFilter{AttributeKey: "tags", FilterOperator: "equal_to", Values: []string{"weather"}},
			wantSQL:  "LOWER(?) = ANY(SELECT LOWER(t) FROM unnest(tags) AS t)",
			wantArgs: []interface{}{"weather"},
			wantOK:   true,
		},
		{
			name:     "tags contains uses unnest + ILIKE (array substring)",
			filter:   model.CustomToolListFilter{AttributeKey: "tags", FilterOperator: "contains", Values: []string{"weath"}},
			wantSQL:  "EXISTS (SELECT 1 FROM unnest(tags) AS tag WHERE tag ILIKE ?)",
			wantArgs: []interface{}{"%weath%"},
			wantOK:   true,
		},
		{
			name:    "tags is_present uses cardinality",
			filter:  model.CustomToolListFilter{AttributeKey: "tags", FilterOperator: "is_present"},
			wantSQL: "cardinality(tags) > 0",
			wantOK:  true,
		},
		{
			name:     "created_at equal_to matches by date",
			filter:   model.CustomToolListFilter{AttributeKey: "created_at", FilterOperator: "equal_to", Values: []string{"2026-01-01"}},
			wantSQL:  "DATE(created_at) = ?",
			wantArgs: []interface{}{"2026-01-01"},
			wantOK:   true,
		},
		{
			name:   "created_at rejects substring operators (no ILIKE on a timestamp)",
			filter: model.CustomToolListFilter{AttributeKey: "created_at", FilterOperator: "contains", Values: []string{"2026"}},
			wantOK: false,
		},
		{
			name:   "created_at rejects a malformed date value (no Postgres cast error)",
			filter: model.CustomToolListFilter{AttributeKey: "created_at", FilterOperator: "equal_to", Values: []string{"notadate"}},
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, args, ok := customToolFilterFragment(tc.filter)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if sql != tc.wantSQL {
				t.Errorf("sql = %q, want %q", sql, tc.wantSQL)
			}
			if len(args) != len(tc.wantArgs) {
				t.Fatalf("args len = %d, want %d", len(args), len(tc.wantArgs))
			}
			for i := range args {
				if args[i] != tc.wantArgs[i] {
					t.Errorf("arg[%d] = %v, want %v", i, args[i], tc.wantArgs[i])
				}
			}
		})
	}
}

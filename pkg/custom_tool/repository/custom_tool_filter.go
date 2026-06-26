package repository

import (
	"fmt"
	"strings"
	"time"

	"evo-ai-core-service/pkg/custom_tool/model"

	"gorm.io/gorm"
)

// customToolFilterColumns whitelists which attribute keys the Custom Tools list
// screen may filter on, mapped to their physical column. Only keys present here
// reach SQL; every value is bound as a parameter, so user input never enters the
// query string — only the (whitelisted) column name and operator shape do.
var customToolFilterColumns = map[string]string{
	"name":        "name",
	"description": "description",
	"method":      "method",
	"endpoint":    "endpoint",
	"tags":        "tags",
	"created_at":  "created_at",
}

// applyCustomToolFilters adds the advanced-filter clauses to the query, combined
// via each clause's query_operator (and/or). It composes with the existing
// search/tags query params already applied by the caller.
func applyCustomToolFilters(query *gorm.DB, filters []model.CustomToolListFilter) *gorm.DB {
	conditions := make([]string, 0, len(filters))
	args := make([]interface{}, 0)

	for _, filter := range filters {
		fragment, fragmentArgs, ok := customToolFilterFragment(filter)
		if !ok {
			continue
		}

		glue := ""
		if len(conditions) > 0 {
			glue = filterQueryGlue(filter.QueryOperator) + " "
		}
		conditions = append(conditions, fmt.Sprintf("%s(%s)", glue, fragment))
		args = append(args, fragmentArgs...)
	}

	if len(conditions) > 0 {
		query = query.Where(strings.Join(conditions, " "), args...)
	}

	return query
}

func filterQueryGlue(operator string) string {
	if strings.EqualFold(operator, "or") {
		return "OR"
	}
	return "AND"
}

func customToolFilterFragment(filter model.CustomToolListFilter) (string, []interface{}, bool) {
	column, ok := customToolFilterColumns[filter.AttributeKey]
	if !ok {
		return "", nil, false
	}

	// tags is a Postgres array column; text operators must use array-aware SQL.
	if column == "tags" {
		return tagsArrayFragment(filter)
	}

	switch filter.FilterOperator {
	case "is_present":
		return fmt.Sprintf("%s IS NOT NULL", column), nil, true
	case "is_not_present":
		return fmt.Sprintf("%s IS NULL", column), nil, true
	}

	value := filterFirstValue(filter.Values)
	if value == "" {
		return "", nil, false
	}

	// created_at is a date column: a non-date value would raise a Postgres cast
	// error (and 500); skip the clause instead, mirroring timeoutFragment.
	if column == "created_at" {
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return "", nil, false
		}
	}

	switch filter.FilterOperator {
	case "equal_to":
		if column == "created_at" {
			return "DATE(created_at) = ?", []interface{}{value}, true
		}
		return fmt.Sprintf("LOWER(%s) = LOWER(?)", column), []interface{}{value}, true
	case "not_equal_to":
		if column == "created_at" {
			return "DATE(created_at) <> ?", []interface{}{value}, true
		}
		return fmt.Sprintf("%s IS NULL OR LOWER(%s) <> LOWER(?)", column, column), []interface{}{value}, true
	case "contains":
		// Substring matching is text-only; ILIKE on the timestamp column errors in Postgres.
		if column == "created_at" {
			return "", nil, false
		}
		return fmt.Sprintf("%s ILIKE ?", column), []interface{}{"%" + value + "%"}, true
	case "does_not_contain":
		if column == "created_at" {
			return "", nil, false
		}
		return fmt.Sprintf("%s IS NULL OR %s NOT ILIKE ?", column, column), []interface{}{"%" + value + "%"}, true
	}

	return "", nil, false
}

// tagsArrayFragment matches against the varchar(255)[] tags column with
// array-aware SQL (ILIKE/LOWER would error on an array).
func tagsArrayFragment(filter model.CustomToolListFilter) (string, []interface{}, bool) {
	switch filter.FilterOperator {
	case "is_present":
		return "cardinality(tags) > 0", nil, true
	case "is_not_present":
		return "tags IS NULL OR cardinality(tags) = 0", nil, true
	}

	value := filterFirstValue(filter.Values)
	if value == "" {
		return "", nil, false
	}

	switch filter.FilterOperator {
	case "equal_to":
		return "LOWER(?) = ANY(SELECT LOWER(t) FROM unnest(tags) AS t)", []interface{}{value}, true
	case "not_equal_to":
		return "NOT (LOWER(?) = ANY(SELECT LOWER(t) FROM unnest(tags) AS t))", []interface{}{value}, true
	case "contains":
		return "EXISTS (SELECT 1 FROM unnest(tags) AS tag WHERE tag ILIKE ?)", []interface{}{"%" + value + "%"}, true
	case "does_not_contain":
		return "NOT EXISTS (SELECT 1 FROM unnest(tags) AS tag WHERE tag ILIKE ?)", []interface{}{"%" + value + "%"}, true
	}

	return "", nil, false
}

func filterFirstValue(values []string) string {
	if len(values) > 0 {
		return strings.TrimSpace(values[0])
	}
	return ""
}

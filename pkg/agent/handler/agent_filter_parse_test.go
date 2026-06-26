package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParseAgentFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	url := "/agents?" +
		"filters[0][attribute_key]=name&filters[0][filter_operator]=contains&filters[0][values]=bot&" +
		"filters[1][attribute_key]=type&filters[1][filter_operator]=equal_to&filters[1][values]=llm&filters[1][query_operator]=and"

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, url, nil)

	filters := parseAgentFilters(c)

	if len(filters) != 2 {
		t.Fatalf("got %d filters, want 2", len(filters))
	}
	if filters[0].AttributeKey != "name" || filters[0].FilterOperator != "contains" {
		t.Errorf("filter[0] = %+v", filters[0])
	}
	if len(filters[0].Values) != 1 || filters[0].Values[0] != "bot" {
		t.Errorf("filter[0].Values = %v", filters[0].Values)
	}
	if filters[1].AttributeKey != "type" || filters[1].QueryOperator != "and" {
		t.Errorf("filter[1] = %+v", filters[1])
	}
}

func TestParseAgentFiltersEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/agents?page=1&pageSize=20", nil)

	if filters := parseAgentFilters(c); len(filters) != 0 {
		t.Fatalf("got %d filters, want 0", len(filters))
	}
}

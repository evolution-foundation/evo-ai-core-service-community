package handler

import (
	"context"
	"encoding/json"
	"evo-ai-core-service/pkg/mcp_server/model"
	"evo-ai-core-service/pkg/mcp_server/service"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// stubService implements service.McpServerService; only List is exercised.
type stubService struct {
	service.McpServerService
	gotPage     int
	gotPageSize int
	gotSearch   string
	result      *model.McpServerListResponse
}

func (s *stubService) List(_ context.Context, page int, pageSize int, search string) (*model.McpServerListResponse, error) {
	s.gotPage, s.gotPageSize, s.gotSearch = page, pageSize, search
	return s.result, nil
}

func getList(t *testing.T, svc service.McpServerService, query string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/mcp-servers?"+query, nil)

	NewMcpServerHandler(svc).List(c)
	return rec
}

func TestListForwardsSearchAndAnswersTheFilteredTotal(t *testing.T) {
	svc := &stubService{result: &model.McpServerListResponse{
		Items:      []model.McpServerResponse{{Name: "github"}},
		Page:       2,
		PageSize:   1,
		TotalItems: 3,
	}}

	rec := getList(t, svc, "skip=1&limit=1&search=%20git%20")

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if svc.gotSearch != "git" {
		t.Fatalf("search must reach the service trimmed, got %q", svc.gotSearch)
	}
	if svc.gotPage != 2 || svc.gotPageSize != 1 {
		t.Fatalf("pagination not kept alongside the search: page=%d pageSize=%d", svc.gotPage, svc.gotPageSize)
	}

	var envelope struct {
		Data []model.McpServerResponse `json:"data"`
		Meta struct {
			Pagination struct {
				Total int `json:"total"`
			} `json:"pagination"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if envelope.Meta.Pagination.Total != 3 {
		t.Fatalf("total must be the filtered count, got %d", envelope.Meta.Pagination.Total)
	}
	if len(envelope.Data) != 1 || envelope.Data[0].Name != "github" {
		t.Fatalf("unexpected data: %#v", envelope.Data)
	}
}

func TestListWithoutSearchSendsEmptySearch(t *testing.T) {
	svc := &stubService{result: &model.McpServerListResponse{PageSize: 20}}

	rec := getList(t, svc, "page=1&pageSize=20")

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if svc.gotSearch != "" {
		t.Fatalf("no search param must mean no filter, got %q", svc.gotSearch)
	}
}

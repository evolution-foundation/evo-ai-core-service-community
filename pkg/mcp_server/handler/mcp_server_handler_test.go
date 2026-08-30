package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"evo-ai-core-service/internal/middleware"
	"evo-ai-core-service/pkg/mcp_server/model"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// The write routes must be reachable by a caller the auth service approves.
// These drive the registered route chain, not the handler method, because the
// gate that broke them lived on the route: it read a gin key nobody ever wrote,
// so every POST/PUT/DELETE answered 401 regardless of the caller's role.

const adminGateMessage = "User is not an admin"

// mcpStubService records the writes that reach the service layer.
type mcpStubService struct {
	createCalls int
	updateCalls int
	deleteCalls int
}

func (s *mcpStubService) Create(_ context.Context, request model.McpServer) (*model.McpServer, error) {
	s.createCalls++
	request.ID = uuid.New()
	return &request, nil
}

func (s *mcpStubService) GetByID(_ context.Context, id uuid.UUID) (*model.McpServer, error) {
	return &model.McpServer{ID: id}, nil
}

func (s *mcpStubService) List(_ context.Context, page int, pageSize int) (*model.McpServerListResponse, error) {
	return &model.McpServerListResponse{Page: page, PageSize: pageSize}, nil
}

func (s *mcpStubService) Update(_ context.Context, request *model.McpServer, id uuid.UUID) (*model.McpServer, error) {
	s.updateCalls++
	request.ID = id
	return request, nil
}

func (s *mcpStubService) Delete(_ context.Context, _ uuid.UUID) (bool, error) {
	s.deleteCalls++
	return true, nil
}

// routePermissionStub is the auth service's verdict, stubbed at the route level
// and recording every permission the routes demanded.
type routePermissionStub struct {
	granted bool
	asked   []string
}

func (p *routePermissionStub) RequirePermission(resource, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		p.asked = append(p.asked, resource+"."+action)
		if !p.granted {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "permission denied"})
			return
		}
		c.Next()
	}
}

func (p *routePermissionStub) CheckPermission(_, _ string) (bool, error) { return p.granted, nil }

func (p *routePermissionStub) CheckPermissionWithType(_, _, _ string) (bool, error) {
	return p.granted, nil
}

func (p *routePermissionStub) HasPermission(_ *gin.Context, _, _ string) (bool, error) {
	return p.granted, nil
}

// newMcpRouter mounts the real route registration against a stubbed verdict.
func newMcpRouter(t *testing.T, granted bool) (*gin.Engine, *mcpStubService, *routePermissionStub) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	permissions := &routePermissionStub{granted: granted}
	t.Cleanup(middleware.SetGlobalPermissionMiddleware(permissions))

	stub := &mcpStubService{}
	engine := gin.New()
	NewMcpServerHandler(stub).RegisterRoutesMiddleware(engine)

	return engine, stub, permissions
}

func mcpRequest(engine *gin.Engine, method, target, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

const validMcpServerBody = `{"name":"Servidor oficial","description":"catalogo",` +
	`"config_type":"studio","config_json":{"command":"npx"},"environments":{"API_KEY":"x"},` +
	`"tools":[{"id":"t1","name":"busca"}],"type":"official"}`

func assertNoAdminGate(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Code == http.StatusUnauthorized && strings.Contains(recorder.Body.String(), adminGateMessage) {
		t.Fatalf("the admin gate is back: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestCreateReachesTheHandlerWhenThePermissionIsGranted(t *testing.T) {
	engine, stub, permissions := newMcpRouter(t, true)

	recorder := mcpRequest(engine, http.MethodPost, "/mcp-servers", validMcpServerBody)

	assertNoAdminGate(t, recorder)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", recorder.Code, recorder.Body.String())
	}
	if stub.createCalls != 1 {
		t.Errorf("create calls = %d, want 1", stub.createCalls)
	}
	if len(permissions.asked) != 1 || permissions.asked[0] != "ai_mcp_servers.create" {
		t.Errorf("route demanded %v, want [ai_mcp_servers.create]", permissions.asked)
	}
}

func TestUpdateReachesTheHandlerWhenThePermissionIsGranted(t *testing.T) {
	engine, stub, permissions := newMcpRouter(t, true)

	recorder := mcpRequest(engine, http.MethodPut, "/mcp-servers/"+uuid.NewString(), validMcpServerBody)

	assertNoAdminGate(t, recorder)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if stub.updateCalls != 1 {
		t.Errorf("update calls = %d, want 1", stub.updateCalls)
	}
	if len(permissions.asked) != 1 || permissions.asked[0] != "ai_mcp_servers.update" {
		t.Errorf("route demanded %v, want [ai_mcp_servers.update]", permissions.asked)
	}
}

func TestDeleteReachesTheHandlerWhenThePermissionIsGranted(t *testing.T) {
	engine, stub, permissions := newMcpRouter(t, true)

	recorder := mcpRequest(engine, http.MethodDelete, "/mcp-servers/"+uuid.NewString(), "")

	assertNoAdminGate(t, recorder)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", recorder.Code, recorder.Body.String())
	}
	if stub.deleteCalls != 1 {
		t.Errorf("delete calls = %d, want 1", stub.deleteCalls)
	}
	if len(permissions.asked) != 1 || permissions.asked[0] != "ai_mcp_servers.delete" {
		t.Errorf("route demanded %v, want [ai_mcp_servers.delete]", permissions.asked)
	}
}

// Removing the unreachable gate must not open the routes: the auth service's
// denial is the enforcement, and it still stops the write.
func TestWritesAreRefusedWhenThePermissionIsDenied(t *testing.T) {
	cases := []struct {
		name   string
		method string
		target string
		body   string
	}{
		{"create", http.MethodPost, "/mcp-servers", validMcpServerBody},
		{"update", http.MethodPut, "/mcp-servers/" + uuid.NewString(), validMcpServerBody},
		{"delete", http.MethodDelete, "/mcp-servers/" + uuid.NewString(), ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine, stub, _ := newMcpRouter(t, false)

			recorder := mcpRequest(engine, tc.method, tc.target, tc.body)

			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403: %s", recorder.Code, recorder.Body.String())
			}
			if stub.createCalls+stub.updateCalls+stub.deleteCalls != 0 {
				t.Errorf("the write reached the service without the permission")
			}
		})
	}
}

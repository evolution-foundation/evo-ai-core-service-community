package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"evo-ai-core-service/pkg/api_key/model"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// The settings screen toggles a credential by sending {name, provider,
// is_active}. GORM skips zero values on struct Updates, so `false` only lands
// if the field is a pointer — otherwise the toggle is a silent no-op.
func TestUpdateAppliesIsActive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &stubService{}
	handler := NewApiKeyHandler(stub, fernetTestKey)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	c.Request = httptest.NewRequest(http.MethodPut, "/agents/apikeys/x",
		bytes.NewBufferString(`{"name":"Producao","provider":"openai","is_active":false}`))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.Update(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if stub.updatedIsActive == nil {
		t.Fatal("is_active never reached the service: the deactivation toggle is a no-op")
	}
	if *stub.updatedIsActive {
		t.Errorf("is_active = true, want false")
	}
}

// Omitting the field must keep the stored value: a rename must not silently
// reactivate a credential the admin had disabled.
func TestUpdateWithoutIsActiveKeepsTheStoredValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &stubService{}
	handler := NewApiKeyHandler(stub, fernetTestKey)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: uuid.New().String()}}
	c.Request = httptest.NewRequest(http.MethodPut, "/agents/apikeys/x",
		bytes.NewBufferString(`{"name":"Renomeada","provider":"openai"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.Update(c)

	if stub.updatedIsActive != nil {
		t.Errorf("is_active = %v, want nil (untouched)", *stub.updatedIsActive)
	}
}

var _ = model.ApiKey{}

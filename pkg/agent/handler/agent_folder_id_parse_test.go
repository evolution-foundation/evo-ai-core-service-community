package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestFolderRoutes_MalformedIDIs400(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name string
		run  func(c *gin.Context)
		body string
	}{
		{name: "assign folder", run: (&agentHandler{}).AssignFolder, body: `{"folder_id":null}`},
		{name: "list agents by folder", run: (&agentHandler{}).ListAgentsByFolderID},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPut, "/", strings.NewReader(tc.body))
			c.Params = gin.Params{{Key: "id", Value: "not-a-uuid"}}

			tc.run(c)

			if recorder.Code != http.StatusBadRequest {
				t.Errorf("status = %d (%s), want 400", recorder.Code, recorder.Body.String())
			}
		})
	}
}

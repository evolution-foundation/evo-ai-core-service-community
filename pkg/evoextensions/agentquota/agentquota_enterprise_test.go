//go:build enterprise

package agentquota

import (
	stderrors "errors"
	"net/http"
	"testing"

	apierrors "evo-ai-core-service/internal/httpclient/errors"
)

func TestEvaluate(t *testing.T) {
	cases := []struct {
		name       string
		count      int
		limit      int
		wantReject bool
	}{
		{"under limit allows", 4, 5, false},
		{"zero agents under limit allows", 0, 5, false},
		{"at limit rejects", 5, 5, true},
		{"over limit rejects", 6, 5, true},
		{"limit zero blocks everything", 0, 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := evaluate(tc.count, tc.limit)

			if !tc.wantReject {
				if err != nil {
					t.Fatalf("evaluate(%d,%d) = %v, want nil (allow)", tc.count, tc.limit, err)
				}
				return
			}

			if err == nil {
				t.Fatalf("evaluate(%d,%d) = nil, want QUOTA_EXCEEDED", tc.count, tc.limit)
			}
			var apiErr *apierrors.ApiError
			if !stderrors.As(err, &apiErr) {
				t.Fatalf("evaluate(%d,%d) error type = %T, want *apierrors.ApiError", tc.count, tc.limit, err)
			}
			if apiErr.Code != quotaExceededCode {
				t.Errorf("code = %q, want %q", apiErr.Code, quotaExceededCode)
			}
			if apiErr.HTTPCode != http.StatusUnprocessableEntity {
				t.Errorf("http code = %d, want %d (422)", apiErr.HTTPCode, http.StatusUnprocessableEntity)
			}
		})
	}
}

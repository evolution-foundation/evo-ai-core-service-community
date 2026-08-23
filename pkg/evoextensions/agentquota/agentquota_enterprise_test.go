//go:build enterprise

package agentquota

import (
	"bytes"
	"context"
	stderrors "errors"
	"log"
	"net/http"
	"testing"

	apierrors "evo-ai-core-service/internal/httpclient/errors"
)

// assertRejected checks the shape the CRM frontend depends on: the licensing
// gem's QUOTA_EXCEEDED code (which drives the localized toast) and a 422.
func assertRejected(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s = nil, want QUOTA_EXCEEDED", what)
	}
	var apiErr *apierrors.ApiError
	if !stderrors.As(err, &apiErr) {
		t.Fatalf("%s error type = %T, want *apierrors.ApiError", what, err)
	}
	if apiErr.Code != quotaExceededCode {
		t.Errorf("code = %q, want %q", apiErr.Code, quotaExceededCode)
	}
	if apiErr.HTTPCode != http.StatusUnprocessableEntity {
		t.Errorf("http code = %d, want %d (422)", apiErr.HTTPCode, http.StatusUnprocessableEntity)
	}
}

// The single-agent path must behave EXACTLY as before the signature changed:
// `count + 1 > limit` is the same boundary as the old `count >= limit`.
func TestEvaluate_SingleAgent(t *testing.T) {
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
			err := evaluate(tc.count, 1, tc.limit)

			if !tc.wantReject {
				if err != nil {
					t.Fatalf("evaluate(%d,1,%d) = %v, want nil (allow)", tc.count, tc.limit, err)
				}
				return
			}
			assertRejected(t, err, "evaluate")
		})
	}
}

// The finding this card came back for: POST /agents/import creates N agents in
// one request, and a gate that can only answer for one at a time let a tenant
// capped at 2 blow past it with a 500-agent JSON.
func TestEvaluate_BulkImport(t *testing.T) {
	cases := []struct {
		name       string
		count      int
		additional int
		limit      int
		wantReject bool
	}{
		// The reported exploit, in miniature: 2-agent cap, bulk upload.
		{"the exploit: cap 2, import 500", 0, 500, 2, true},
		{"cap 2, import 3", 0, 3, 2, true},

		// The boundary: filling the plan exactly is allowed, one past it is not.
		{"import fills the plan exactly", 0, 5, 5, false},
		{"import exceeds by one", 0, 6, 5, true},
		{"partial room, import fits", 3, 2, 5, false},
		{"partial room, import one too many", 3, 3, 5, true},

		// A tenant already at the limit cannot import anything.
		{"already at limit, import one", 5, 1, 5, true},

		{"limit zero blocks any import", 0, 1, 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := evaluate(tc.count, tc.additional, tc.limit)

			if !tc.wantReject {
				if err != nil {
					t.Fatalf("evaluate(%d,%d,%d) = %v, want nil (allow)",
						tc.count, tc.additional, tc.limit, err)
				}
				return
			}
			assertRejected(t, err, "evaluate")
		})
	}
}

// A caller that cannot say how many it is creating must not get a free pass.
// Guards against a future caller passing 0 (or a negative from a bad cast) and
// silently reopening the hole this card exists to close.
func TestEvaluate_NonPositiveAdditionalCountsAsOne(t *testing.T) {
	for _, additional := range []int{0, -1, -500} {
		if err := evaluate(5, additional, 5); err == nil {
			t.Errorf("evaluate(5,%d,5) = nil, want rejection — a tenant at its limit "+
				"must not slip through by asking for %d", additional, additional)
		}
		if err := evaluate(0, additional, 5); err != nil {
			t.Errorf("evaluate(0,%d,5) = %v, want nil — treating it as 1 must not "+
				"reject a tenant with room", additional, err)
		}
	}
}

// The message is what an operator reads when the import is refused, so it has to
// carry the three numbers that explain the refusal.
func TestEvaluate_MessageExplainsTheRefusal(t *testing.T) {
	err := evaluate(1, 500, 2)

	var apiErr *apierrors.ApiError
	if !stderrors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *apierrors.ApiError", err)
	}
	for _, want := range []string{"1", "2", "500"} {
		if !contains(apiErr.Message, want) {
			t.Errorf("message %q does not mention %q (count/limit/requested)", apiErr.Message, want)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// --- F3: a non-enforcing exit must leave a trace -----------------------------
//
// The card that opened this work is titled "nada falha, nada loga — o limite
// simplesmente não existe na prática". A fix that skips enforcement in silence
// puts the operator back exactly where they started: a limit that is
// configured, believed, and quietly absent.
//
// These capture the standard logger and assert BOTH that a trace exists and
// that it says which tenant and why — a bare "skipped" would not survive a
// 3am incident.

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buffer bytes.Buffer
	previousOut := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(&buffer)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousOut)
		log.SetFlags(previousFlags)
	})
	return &buffer
}

func TestLogSkip_NamesTheTenantAndTheReason(t *testing.T) {
	logged := captureLog(t)

	logSkip("tenant-abc", "counting agents failed, allowing the create: boom")

	line := logged.String()
	for _, want := range []string{"enterprise agent quota", "NOT enforced", "tenant-abc", "boom"} {
		if !contains(line, want) {
			t.Errorf("log line %q is missing %q", line, want)
		}
	}
}

// The prefix is load-bearing: wire_enterprise.go uses "enterprise ..." so the
// whole enterprise wiring is greppable under one term. A skip that does not
// match that grep is a skip nobody finds.
func TestLogSkip_SharesTheEnterpriseWiringPrefix(t *testing.T) {
	logged := captureLog(t)

	logSkip("t1", "whatever")

	if !contains(logged.String(), "enterprise ") {
		t.Errorf("log line %q does not share the enterprise wiring prefix", logged.String())
	}
}

// An unbound tenant is the community/standalone case — normal, and logging it on
// every request would bury the abnormal ones.
func TestCheck_UnboundTenantIsSilent(t *testing.T) {
	logged := captureLog(t)

	if err := Check(context.Background(), 1); err != nil {
		t.Fatalf("Check with no tenant = %v, want nil", err)
	}

	if logged.Len() != 0 {
		t.Errorf("unbound tenant logged %q; the normal path must stay quiet", logged.String())
	}
}

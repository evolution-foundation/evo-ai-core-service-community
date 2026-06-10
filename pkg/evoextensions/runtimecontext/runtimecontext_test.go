package runtimecontext

import (
	"context"
	"testing"
)

func TestDefault_CurrentID_AlwaysEmpty(t *testing.T) {
	t.Parallel()

	c := Default()
	if got := c.CurrentID(context.Background()); got != "" {
		t.Fatalf("Default().CurrentID(Background) = %q, want empty", got)
	}
	if got := c.CurrentID(context.TODO()); got != "" {
		t.Fatalf("Default().CurrentID(TODO) = %q, want empty", got)
	}
}

func TestIDFromContext_Unbound_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	if got := IDFromContext(context.Background()); got != "" {
		t.Fatalf("IDFromContext(Background) = %q, want empty", got)
	}
	//nolint:staticcheck,SA1012 // explicit nil-ctx safety
	if got := IDFromContext(nil); got != "" {
		t.Fatalf("IDFromContext(nil) = %q, want empty", got)
	}
}

func TestWithID_RoundTrip(t *testing.T) {
	t.Parallel()

	ctx := WithID(context.Background(), "tenant-abc")
	if got := IDFromContext(ctx); got != "tenant-abc" {
		t.Fatalf("IDFromContext after WithID: got %q want %q", got, "tenant-abc")
	}
}

// WithID on a nil ctx must not panic — callers may be bridging from a
// helper that returns a nil ctx in edge cases. We return the same nil
// rather than fabricating a Background so that the caller's nil-check
// behaviour is unchanged.
func TestWithID_NilCtx_NoPanic(t *testing.T) {
	t.Parallel()

	//nolint:staticcheck,SA1012 // explicit nil-ctx safety
	got := WithID(nil, "x")
	if got != nil {
		t.Fatalf("WithID(nil, _) = %v, want nil", got)
	}
}

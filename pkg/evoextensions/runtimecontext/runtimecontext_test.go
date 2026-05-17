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

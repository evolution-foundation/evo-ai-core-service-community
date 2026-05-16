package plugin

import "testing"

func TestDefault_Discover_AlwaysEmpty(t *testing.T) {
	t.Parallel()

	r := Default()
	got := r.Discover()
	if len(got) != 0 {
		t.Fatalf("Default().Discover() returned %d entries (%v), want 0", len(got), got)
	}
}

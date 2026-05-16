package feature

import "testing"

func TestDefault_Enabled_AlwaysTrue(t *testing.T) {
	t.Parallel()

	g := Default()
	for _, name := range []string{"", "anything", "evo.feature.example"} {
		if !g.Enabled(name) {
			t.Fatalf("Default().Enabled(%q) = false, want true", name)
		}
	}
}

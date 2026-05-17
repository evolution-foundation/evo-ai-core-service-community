package capability

import "testing"

func TestDefault_Enabled_AlwaysTrue(t *testing.T) {
	t.Parallel()

	g := Default()
	for _, name := range []string{"", "anything", "evo.capability.example"} {
		if !g.Enabled(name) {
			t.Fatalf("Default().Enabled(%q) = false, want true", name)
		}
	}
}

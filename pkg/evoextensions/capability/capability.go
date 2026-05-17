// Package capability is the public capability-gating extension point of
// the community release. See EXTENSION_POINTS.md at the repository root.
package capability

// Gate decides whether a named capability is enabled for the current
// request or process. Implementations must be safe for concurrent use
// and side-effect-free for the same input within a single request.
type Gate interface {
	Enabled(name string) bool
}

type noop struct{}

func (noop) Enabled(string) bool { return true }

// Default returns the no-op gate used when no extension is installed.
// It always reports every capability as enabled, preserving the
// community release's standalone behaviour.
func Default() Gate { return noop{} }

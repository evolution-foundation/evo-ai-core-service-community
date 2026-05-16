// Package plugin is the public plugin-discovery extension point of the
// community release. See EXTENSION_POINTS.md at the repository root.
package plugin

// Registry exposes the set of plugins that have registered themselves
// with the running process. The contract is intentionally read-only:
// the community release does not provide a public API to mutate the
// registry. Plugins are expected to register themselves at process
// start through a mechanism owned by the consumer.
//
// Discover returns the identifiers of registered plugins. The returned
// slice is owned by the caller; implementations must not retain it.
type Registry interface {
	Discover() []string
}

type noop struct{}

func (noop) Discover() []string { return nil }

// Default returns the no-op registry used when no extension is
// installed. It always reports an empty set of plugins.
func Default() Registry { return noop{} }

// Package tenant is the public tenant-context extension point of the
// community release. See EXTENSION_POINTS.md at the repository root.
package tenant

import "context"

// Context resolves the tenant identifier bound to a given request or
// background job. Implementations must be safe for concurrent use.
//
// The returned string is opaque to the community release; an empty
// string means "no tenant bound", which is the standalone case.
type Context interface {
	CurrentID(ctx context.Context) string
}

type noop struct{}

func (noop) CurrentID(context.Context) string { return "" }

// Default returns the no-op context used when no extension is
// installed. It always reports the empty string, preserving the
// community release's single-tenant behaviour.
func Default() Context { return noop{} }

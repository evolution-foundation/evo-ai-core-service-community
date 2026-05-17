// Package runtimecontext is the public runtime-scope extension point of
// the community release. See EXTENSION_POINTS.md at the repository root.
package runtimecontext

import "context"

// Scope resolves the runtime scope identifier bound to a given request
// or background job. Implementations must be safe for concurrent use.
//
// The returned string is opaque to the community release; an empty
// string means "no scope bound", which is the standalone case.
type Scope interface {
	CurrentID(ctx context.Context) string
}

type noop struct{}

func (noop) CurrentID(context.Context) string { return "" }

// Default returns the no-op scope used when no extension is installed.
// It always reports the empty string, preserving the community
// release's single-scope behaviour.
func Default() Scope { return noop{} }

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

type ctxKey int

const idKey ctxKey = 0

// WithID returns a copy of ctx carrying the given runtime-scope id. The
// community release does not call this itself; the helper exists so the
// enterprise build can bridge its tenant binding into a context value
// that downstream community code can read without importing the
// enterprise SDK.
func WithID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, idKey, id)
}

// IDFromContext returns the runtime-scope id bound to ctx via WithID,
// or "" when no id is bound. Use this in community code paths that
// need to propagate the active scope (eg. server-to-server header
// propagation) without coupling to any specific Scope implementation.
func IDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(idKey).(string); ok {
		return v
	}
	return ""
}

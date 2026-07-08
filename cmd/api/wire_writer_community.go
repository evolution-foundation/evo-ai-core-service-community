//go:build !enterprise

package main

import "context"

// installConfigWriter is the community-build no-op. The enterprise build
// provides the real implementation via cmd/api/wire_writer_enterprise.go behind
// the `enterprise` build tag.
func installConfigWriter(_ context.Context) {}

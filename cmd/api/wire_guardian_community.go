//go:build !guardian

package main

import (
	"context"

	"gorm.io/gorm"
)

// installGuardian is the no-op for builds without the `guardian` tag. The
// license guardian ships only in the self-hosted enterprise box image (built
// with `-tags='enterprise guardian'`); the community build never runs it.
func installGuardian(_ context.Context, _ *gorm.DB) {}

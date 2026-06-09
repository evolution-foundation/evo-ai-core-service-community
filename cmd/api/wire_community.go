//go:build !enterprise

package main

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// installRuntimeScope is the community-build no-op. The community
// release runs in single-scope mode (runtimecontext.Default()) and
// does not enforce tenant headers. The enterprise build replaces
// this function via wire_enterprise.go behind the `enterprise`
// build tag.
func installRuntimeScope(_ *gin.RouterGroup, _ *gorm.DB) {}

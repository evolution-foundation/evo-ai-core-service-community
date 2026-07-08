//go:build enterprise

package main

import (
	"context"
	"log"

	"github.com/evolution-foundation/evo-enterprise-licensing-go/writer"
)

// installConfigWriter is the enterprise-build override of the community no-op
// hook. It starts the licensing SDK's config writer on its own connection pool;
// details live in the private evo-enterprise-licensing-go module. When no writer
// DSN is configured, the writer is disabled and boot continues normally.
func installConfigWriter(ctx context.Context) {
	dsn := writer.SealedDSN()
	if dsn == "" {
		log.Println("enterprise wiring: config writer disabled (not configured)")
		return
	}
	if _, err := writer.Start(ctx, writer.StartConfig{DSN: dsn}); err != nil {
		// Non-fatal: log and keep serving.
		log.Printf("enterprise wiring: config writer failed to start: %v", err)
		return
	}
	log.Println("enterprise wiring: config writer started")
}

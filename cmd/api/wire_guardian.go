//go:build guardian

package main

import (
	"context"
	"log"
	"os"

	"github.com/evolution-foundation/evo-enterprise-licensing-go/guardian"
	"gorm.io/gorm"
)

// installGuardian boots the license guardian (enterprise lib) behind the
// dedicated `guardian` build tag. It is a thin hook — all logic lives in the
// enterprise lib. The guardian verifies the Ed25519-signed license and becomes
// the single producer of the Redis cache the Ruby FeatureGate reads.
//
// Fail-mode: a misconfigured/unreachable guardian must NOT crash evo-core
// (which also serves the AI plane). It simply doesn't produce the cache → the
// Ruby gate denies by cache-miss (fail-closed). So a start error is logged, not
// fatal.
func installGuardian(ctx context.Context, db *gorm.DB) {
	sqlDB, err := db.DB()
	if err != nil {
		log.Printf("guardian: cannot reach underlying *sql.DB (%v); license cache will not be produced", err)
		return
	}
	version := os.Getenv("EVO_ENTERPRISE_VERSION")
	if version == "" {
		version = "selfhosted"
	}
	if err := guardian.Start(ctx, guardian.Config{
		LicensingURL: os.Getenv("EVO_ENTERPRISE_LICENSING_URL"),
		APIKey:       os.Getenv("EVO_ENTERPRISE_API_KEY"),
		RedisURL:     os.Getenv("EVO_ENTERPRISE_REDIS_URL"),
		DB:           sqlDB,
		Version:      version,
	}); err != nil {
		log.Printf("guardian: failed to start (%v); features will deny until it runs", err)
	}
}

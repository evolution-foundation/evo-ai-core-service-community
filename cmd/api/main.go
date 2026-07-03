package main

import (
	"context"
	"evo-ai-core-service/internal/config"
	"evo-ai-core-service/internal/infra/postgres"
	"evo-ai-core-service/internal/middleware"
	"evo-ai-core-service/internal/telemetry"
	agentModule "evo-ai-core-service/pkg/agent"
	agentIntegrationModule "evo-ai-core-service/pkg/agent_integration"
	apiKeyModule "evo-ai-core-service/pkg/api_key"
	coreModule "evo-ai-core-service/pkg/core"
	systemHandler "evo-ai-core-service/pkg/core/handler"
	customMcpServerModule "evo-ai-core-service/pkg/custom_mcp_server"
	customToolModule "evo-ai-core-service/pkg/custom_tool"
	folderModule "evo-ai-core-service/pkg/folder"
	folderShareModule "evo-ai-core-service/pkg/folder_share"
	mcpServerModule "evo-ai-core-service/pkg/mcp_server"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

)

func main() {
	var devMode = flag.Bool("dev", false, "Run in development mode")
	flag.Parse()

	if *devMode {
		err := godotenv.Load(".env")
		if err != nil {
			log.Printf("Warning: Could not load .env file: %v", err)
		}
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize OpenTelemetry tracing
	ctx := context.Background()
	shutdownTracer, err := telemetry.InitTracer(ctx)
	if err != nil {
		log.Printf("Warning: Failed to initialize OpenTelemetry tracing: %v", err)
	} else {
		// Register shutdown handler for graceful tracer shutdown
		go func() {
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
			<-sigChan
			if shutdownTracer != nil {
				if err := shutdownTracer(ctx); err != nil {
					log.Printf("Error shutting down tracer: %v", err)
				}
			}
			os.Exit(0)
		}()
	}

	// Initialize database
	db := postgres.ConnectGorm(&cfg.DB)

	// Initialize EvoAuth middleware
	evoAuthMiddleware := middleware.NewEvoAuthMiddleware(cfg.EvoAuth.BaseURL)

	// Initialize global Permission middleware
	middleware.InitializePermissionMiddleware(cfg.EvoAuth.BaseURL)

	// Initialize modules
	coreModule := coreModule.New(db, &cfg.HealthCheck)
	mcpServerModule := mcpServerModule.New(db)
	customToolModule := customToolModule.New(db)
	customMcpServerModule := customMcpServerModule.New(db, &cfg.AIProcessorService)
	apiKeyModule := apiKeyModule.New(db, cfg.Core.EncryptionKey)
	folderModule := folderModule.New(db)
	folderShareModule := folderShareModule.New(db, folderModule.Service)
	agentModule := agentModule.New(
		db,
		&cfg.AIProcessorService,
		folderModule.Service,
		apiKeyModule.Service,
		mcpServerModule.Service,
		customToolModule.Service,
		customMcpServerModule.Service,
		folderShareModule.Service,
	)

	// Initialize handlers
	systemHandler := systemHandler.NewSystemHandler(coreModule.HealthService)

	// Setup routes with custom logging configuration
	router := gin.New()
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false

	// Add recovery middleware
	router.Use(gin.Recovery())

	// Add OpenTelemetry tracing middleware (if enabled)
	if os.Getenv("OTEL_TRACES_ENABLED") == "true" {
		router.Use(otelgin.Middleware("evo-ai-core-service"))
	}

	// Add custom logger that skips health check and metrics endpoints
	router.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		SkipPaths: []string{"/health", "/ready", "/metrics"},
	}))

	// Add rate limiting middleware with configuration
	router.Use(middleware.GlobalRateLimitMiddleware(cfg.RateLimit.GlobalRPS, cfg.RateLimit.GlobalBurst))
	router.Use(middleware.RateLimitMiddleware(cfg.RateLimit.ClientRPS, cfg.RateLimit.ClientBurst))

	// Add CORS middleware
	corsConfig := middleware.CORSConfig(cfg.Evolution.BaseURL)
	router.Use(middleware.CORS(corsConfig))

	// System routes (health and ready checks)
	systemHandler.RegisterRoutes(router)

	// API v1 routes with EvoAuth authentication
	v1 := router.Group("/api/v1")
	v1.Use(evoAuthMiddleware.GetEvoAuthMiddleware())
	// installRuntimeScope is a build-tagged hook: no-op in the community
	// build (cmd/api/wire_community.go), wires the enterprise tenant
	// middleware after EvoAuth when the binary is built with
	// `-tags=enterprise` (cmd/api/wire_enterprise.go).
	installRuntimeScope(v1, db)
	// installGuardian is a build-tagged hook: no-op in the community build
	// (cmd/api/wire_guardian_community.go), boots the license guardian when the
	// binary is built with `-tags=guardian` (cmd/api/wire_guardian.go). The
	// guardian runs for the life of the process on context.Background().
	installGuardian(context.Background(), db)
	{
		customToolModule.Handler.RegisterRoutesMiddleware(v1)
		customMcpServerModule.Handler.RegisterRoutesMiddleware(v1)
		mcpServerModule.Handler.RegisterRoutesMiddleware(v1)
		folderModule.Handler.RegisterRoutesMiddleware(v1)
		// Register API keys before agents to ensure /agents/apikeys is captured first
		apiKeyModule.Handler.RegisterRoutesMiddleware(v1)
		agentModule.Handler.RegisterRoutesMiddleware(v1)
		// Register agent integrations routes
		agentIntegrationModule.InitModule(db, v1)
		folderShareModule.Handler.RegisterRoutesMiddleware(v1)
	}

	// Start server with custom logging configuration
	log.Printf("Starting server on %s", fmt.Sprintf(":%s", cfg.Core.Port))
	if err := router.Run(fmt.Sprintf(":%s", cfg.Core.Port)); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

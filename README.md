<p align="center">
  <a href="https://evolutionfoundation.com.br">
    <img src="./public/hover-evolution.png" alt="Evolution Foundation" />
  </a>
</p>

<h1 align="center">Evo CRM Core Service</h1>

<p align="center">
  Agent management, custom tools, API keys and folder organization service for the Evo CRM Community.
</p>

<p align="center">
  <a href="https://github.com/evolution-foundation/evo-ai-core-service-community/releases/latest"><img src="https://img.shields.io/github/v/release/evolution-foundation/evo-ai-core-service-community?include_prereleases&label=version&color=00ffa7" alt="Latest version" /></a>
  <a href="https://opensource.org/licenses/Apache-2.0"><img src="https://img.shields.io/badge/License-Apache%202.0-blue.svg" alt="License: Apache 2.0" /></a>
  <a href="https://docs.evolutionfoundation.com.br"><img src="https://img.shields.io/badge/Docs-evolutionfoundation.com.br-00ffa7" alt="Documentation" /></a>
  <a href="https://evolutionfoundation.com.br/community"><img src="https://img.shields.io/badge/Community-Join%20us-white" alt="Community" /></a>
</p>

<p align="center">
  <a href="https://evolutionfoundation.com.br">Website</a> &middot;
  <a href="https://docs.evolutionfoundation.com.br">Documentation</a> &middot;
  <a href="https://evolutionfoundation.com.br/community">Community</a> &middot;
  <a href="mailto:suporte@evofoundation.com.br">Support</a>
</p>

---

## About

**Evo CRM Core Service** is the agent management microservice of the Evo CRM Community. Built in Go with the Gin framework, it provides agent CRUD, custom HTTP tools for agents, MCP server registration, API key management, and workspace organization through folders and shares.

The service shares its PostgreSQL database with the CRM backend (`evo-ai-crm-community`), using the `evo_core_` table prefix to avoid conflicts.

## Part of the Evo CRM Community

Evo CRM Core Service is part of the [Evo CRM Community](https://github.com/evolution-foundation/evo-crm-community) ecosystem maintained by Evolution Foundation. To use the full stack, clone the umbrella repository with submodules:

```bash
git clone --recurse-submodules git@github.com:evolution-foundation/evo-crm-community.git
```

The Community Edition is **single-tenant** by design — one account, no multi-tenancy overhead, no super-admin, no billing or plans. All limits are removed and features are unlocked by default.

---

## Quick Start

### Prerequisites

- **Go** 1.24+
- **PostgreSQL** 12+
- **`evo-ai-crm-community`** running (shares the database)
- **golang-migrate** (installed automatically via `make install`)
- **swag** (installed automatically via `make install`)

### Installation

```bash
git clone git@github.com:evolution-foundation/evo-ai-core-service-community.git
cd evo-ai-core-service-community

# Configure environment
cp .env.template .env
# Edit .env with your settings

# Install dependencies and start
make start
```

The service will be available at `http://localhost:5555`.

---

## Configuration

### Database

The service uses the same PostgreSQL database as the CRM backend for seamless integration:

```bash
# .env
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=root
DB_NAME=evo_chat   # Same database as the CRM backend
DB_SSLMODE=disable
```

### CRM Backend Integration

```bash
# Base URL of the CRM backend (no trailing slash)
EVOLUTION_BASE_URL=http://localhost:3000

# JWT secret must match the CRM backend's SECRET_KEY_BASE
JWT_SECRET_KEY=your_crm_secret_key_base
```

### Service port

```bash
PORT=5555
```

---

## Development Commands

### Quick start
```bash
make start          # Install everything and start in dev mode
make help           # Show all available commands
```

### Development
```bash
make dev            # Dev mode with race detection
make run            # Production mode
make build          # Compile the application
```

### Database
```bash
make db-setup       # Create database and run migrations
make db-reset       # Full reset (with confirmation)
make migrate-up     # Run pending migrations
make migrate-down   # Roll back last migration
make migrate-create NAME=migration_name
```

### Utilities
```bash
make install        # Install dependencies and tooling
make swag           # Generate Swagger documentation
make tidy           # Tidy Go modules
make clean          # Clean generated files
```

---

## Architecture

```
evo-ai-core-service-community/
├── cmd/api/                    # Entry point and Swagger annotations
├── internal/                   # Application-private code
│   ├── config/                # Configuration management
│   ├── middleware/            # HTTP middleware (auth, CORS, etc.)
│   ├── utils/                 # Utility packages (context, JWT, string)
│   └── infra/postgres/        # Database connection and infrastructure
├── pkg/                       # Public modules (domain-driven design)
│   ├── agent/                # AI agent management with A2A integration
│   ├── api_key/              # API key management for AI services
│   ├── custom_tool/          # HTTP tool definitions for agents
│   ├── custom_mcp_server/    # Model Context Protocol server registration
│   ├── folder/               # Workspace and organization
│   ├── folder_share/         # Folder sharing and permissions
│   └── mcp_server/           # MCP server registry
├── migrations/               # Database schema migrations
└── docs/                     # Generated Swagger documentation
```

Each domain module follows a consistent structure: **Handler** (HTTP) → **Service** (business logic) → **Repository** (GORM data access) → **Model** (DTOs and entities).

---

## Authentication

The service uses **Bearer Token** authentication via tokens issued by `evo-auth-service-community`:

```bash
curl -H "Authorization: Bearer YOUR_ACCESS_TOKEN" \
     http://localhost:5555/api/v1/agents
```

The middleware validates tokens against the auth service's `/api/v1/me` endpoint and injects user/account context into each request.

---

## API Endpoints

### Agents
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/agents` | List agents (skip/limit or page/pageSize) |
| `POST` | `/api/v1/agents` | Create agent |
| `PUT` | `/api/v1/agents/:id` | Update agent |
| `DELETE` | `/api/v1/agents/:id` | Delete agent |
| `GET` | `/api/v1/agents/:id` | Get agent by ID |

### Custom Tools
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/custom-tools` | List custom tools |
| `POST` | `/api/v1/custom-tools` | Create custom tool |
| `PUT` | `/api/v1/custom-tools/:id` | Update custom tool |
| `DELETE` | `/api/v1/custom-tools/:id` | Delete custom tool |
| `GET` | `/api/v1/custom-tools/:id/test` | Test custom tool |

### API Keys
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/apikeys` | List API keys |
| `POST` | `/api/v1/apikeys` | Create API key |
| `PUT` | `/api/v1/apikeys/:id` | Update API key |
| `DELETE` | `/api/v1/apikeys/:id` | Delete API key |

### Folders
| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/folders` | List folders |
| `GET` | `/api/v1/folders/accessible-folders` | List accessible folders |
| `POST` | `/api/v1/folders` | Create folder |
| `PUT` | `/api/v1/folders/:id` | Update folder |
| `DELETE` | `/api/v1/folders/:id` | Delete folder |

### System
- `GET /health` — Health check
- `GET /ready` — Readiness check
- `GET /swagger/*` — Swagger UI

---

## Database

- **Shared database**: uses the same PostgreSQL instance as the CRM backend
- **Table prefix**: all tables prefixed with `evo_core_` to avoid conflicts
- **Migration safety**: backs up the Rails `schema_migrations` table before running Go migrations

### Main tables
- `evo_core_agents` — AI agents of various types
- `evo_core_custom_tools` — HTTP tool definitions
- `evo_core_api_keys` — Encrypted credential storage
- `evo_core_folders` — Workspace organization
- `evo_core_folder_shares` — Folder sharing
- `evo_core_mcp_servers` — Model Context Protocol servers

---

## Documentation

| Resource | Link |
|---|---|
| Website | [evolutionfoundation.com.br](https://evolutionfoundation.com.br) |
| Documentation | [docs.evolutionfoundation.com.br](https://docs.evolutionfoundation.com.br) |
| Community | [evolutionfoundation.com.br/community](https://evolutionfoundation.com.br/community) |
| Swagger UI | `http://localhost:5555/swagger/index.html` |
| Changelog | [CHANGELOG.md](./CHANGELOG.md) |

---

## Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](./CONTRIBUTING.md) for guidelines on how to submit issues, propose features, and open pull requests.

Join our [community](https://evolutionfoundation.com.br/community) to discuss ideas and collaborate.

---

## Security

For security issues, **do not open a public issue**. Email **suporte@evofoundation.com.br** or use GitHub's private vulnerability reporting. See [SECURITY.md](./SECURITY.md) for details.

---

## License

Evo CRM Core Service is licensed under the Apache License 2.0. See [LICENSE](./LICENSE) for details.

## Trademarks

"Evolution Foundation", "Evolution" and "Evo CRM Core Service" are trademarks of Evolution Foundation. See [TRADEMARKS.md](./TRADEMARKS.md) for the brand assets policy.

Third-party attributions are documented in [NOTICE](./NOTICE).

---

<p align="center">
  Made by <a href="https://evolutionfoundation.com.br">Evolution Foundation</a> · © 2026
</p>

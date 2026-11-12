# 🎵 Waveyard Studio Backend

[![Go Version](https://img.shields.io/badge/Go-1.27.4-00ADD8?style=flat&logo=go)](https://golang.org)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-15+-336791?style=flat&logo=postgresql&logoColor=white)](https://www.postgresql.org)
[![Redis](https://img.shields.io/badge/Redis-7+-DC382D?style=flat&logo=redis&logoColor=white)](https://redis.io)
[![Cloudflare R2](https://img.shields.io/badge/Storage-Cloudflare%20R2-F38020?style=flat&logo=cloudflare&logoColor=white)](https://www.cloudflare.com/products/r2/)
[![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?style=flat&logo=docker&logoColor=white)](https://www.docker.com)
[![OpenAPI](https://img.shields.io/badge/API%20Docs-Swagger%20UI-85EA2D?style=flat&logo=swagger&logoColor=black)](http://localhost:8080/docs/)

> Production-ready, high-performance modular monolith backend API and durable asynchronous media processing worker for the **Waveyard Studio** (`waveyard.studio`) beat licensing and music marketplace platform.

---

## 📑 Table of Contents

- [Overview & Architecture](#-overview--architecture)
  - [Architecture Design](#architecture-design)
  - [Dual-Process Model](#dual-process-model)
  - [Core Domain Modules](#core-domain-modules)
  - [System Architecture Diagram](#system-architecture-diagram)
- [Tech Stack](#-tech-stack)
- [Prerequisites](#-prerequisites)
- [Local Development Quick Start](#-local-development-quick-start)
- [Make Commands Reference](#-make-commands-reference)
- [Database Migrations Guide](#-database-migrations-guide)
  - [How Migrations Work](#how-migrations-work)
  - [Creating a New Migration](#creating-a-new-migration)
  - [Running & Rolling Back Migrations](#running--rolling-back-migrations)
  - [Fixing Dirty Migration States](#fixing-dirty-migration-states)
- [Direct Media Upload & Worker Pipeline](#-direct-media-upload--worker-pipeline)
- [Environment Variables](#-environment-variables)
- [Production Deployment Guide](#-production-deployment-guide)
  - [Architecture Overview](#deployment-architecture)
  - [Step 1: Provision Neon PostgreSQL](#step-1-provision-neon-postgresql)
  - [Step 2: Setup Cloudflare R2 Storage](#step-2-setup-cloudflare-r2-storage)
  - [Step 3: Deploy Backend API on Render](#step-3-deploy-backend-api-on-render)
  - [Step 4: Deploy Media Worker on Render](#step-4-deploy-media-worker-on-render)
  - [Step 5: Frontend Connection & CORS Configuration](#step-5-frontend-connection--cors-configuration)
  - [What Happens Under the Hood During Deployment](#what-happens-under-the-hood-during-deployment)
  - [Post-Deployment Smoke Test](#post-deployment-smoke-test)
- [API Routes Reference](#-api-routes-reference)
- [Testing & Coverage Workflow](#-testing--coverage-workflow)

---

## 🏛 Overview & Architecture

### Architecture Design
The backend is structured as a **Modular Monolith** applying **Domain-Driven Design (DDD)** and **Clean / Hexagonal Architecture** principles:
- **API Gateway Layer (`internal/gateway`)**: Centralized HTTP routing via Go's standard library `net/http.ServeMux`, middleware orchestration (CORS, Prometheus metrics, structured request logging, rate limiting, and JWT authentication / RBAC), and OpenAPI documentation serving.
- **Domain Modules (`internal/modules/*`)**: Autonomous business domains with explicit boundaries (Domain Entities, Application Services, Repositories/Persistence, and HTTP Handlers).
- **Shared Infrastructure (`internal/shared/infrastructure/*`)**: Reusable database connection pooling, Redis caching, configuration loading, Resend email dispatching, and utility helpers.

### Dual-Process Model
The repository builds and runs **two distinct binaries**:
1. **API Server (`cmd/server`)**: Serves user-facing HTTP and WebSocket endpoints, handles authentication, catalog browsing, checkout sessions, and dispatches upload tokens.
2. **Media Worker (`cmd/worker`)**: A durable background processor that claims async audio processing jobs from PostgreSQL using lease-based locking (`FOR UPDATE SKIP LOCKED`), validates uploaded files directly from Cloudflare R2, extracts MP3 preview snippets, calculates waveform peaks, updates database records, and triggers real-time WebSocket notifications.

### Core Domain Modules

| Module | Responsibility |
| :--- | :--- |
| **`auth`** | User registration, login, Google OAuth 2.0, refresh token rotation (HTTP-only secure cookies), email verification, password reset, and session management. |
| **`catalog`** | Beats/samples/loops catalog, genre taxonomy, licensing tiers (Basic, Premium, Trackout, Unlimited), tags, search filters, dual-currency pricing (INR/USD), and upload sessions. |
| **`filestorage`** | Presigned S3/R2 direct upload/download URL generation, image optimization (avatars/banners/artwork), and secure audio stream handling. |
| **`payment`** | Order management, dual payment gateway routing (**Razorpay** for INR, **Dodo Payments** for USD/international), signature/webhook verification, and license generation. |
| **`user`** | User profiles, producer store settings, avatar and banner asset uploads, and public producer storefronts. |
| **`notification`** | Real-time WebSocket subscriptions (`/ws`), unread count tracking, and persistent in-app notifications. |
| **`analytics`** | Audio play tracking, likes/favorites, producer revenue analytics, top-performing specs, and system-wide overview metrics. |
| **`admin`** | Super Admin RBAC, platform moderation (users, specs, orders, licenses), system role assignment, and immutable audit logs. |

### System Architecture Diagram

```
                              ┌──────────────────────────────────────────────────┐
                              │           Client (Web Browser / SPA)             │
                              └──────┬─────────────────────────────┬─────────────┘
                                     │ HTTP / Presigned Direct PUT │ WebSocket (/ws)
                                     ▼                             ▼
┌────────────────────────────────────┼─────────────────────────────┼──────────────────────────────────┐
│ API GATEWAY LAYER                  │                             │                                  │
│   ├── CORS Middleware              │                             │                                  │
│   ├── Request Logger & Prometheus  │                             │                                  │
│   ├── Rate Limiting Middleware     │                             │                                  │
│   └── JWT & RBAC Auth Middleware   │                             │                                  │
└────────────────────────────────────┼─────────────────────────────┼──────────────────────────────────┘
                                     ▼                             ▼
┌────────────────────────────────────┼─────────────────────────────┼──────────────────────────────────┐
│ DOMAIN MODULES                     │                             │                                  │
│   ├── Auth Module                  │                             │                                  │
│   ├── Catalog Module               │                             │                                  │
│   ├── Payment Module (Razorpay/Dodo)                             │                                  │
│   ├── User & Profile Module        │                             │                                  │
│   ├── Notification Module ─────────┼─────────────────────────────┘                                  │
│   ├── Analytics Module             │                                                                │
│   ├── Admin RBAC Module            │                                                                │
│   └── FileStorage Module (R2/S3)   │                                                                │
└────────────────────────────────────┼────────────────────────────────────────────────────────────────┘
                                     ▼
┌─────────────────────────────────────────────────────────────────────────────────────────────────────┐
│ SHARED INFRASTRUCTURE                                                                              │
│   ┌───────────────────────────┐    ┌───────────────────────────┐    ┌───────────────────────────┐   │
│   │   PostgreSQL 15 (Neon)    │    │       Redis 7 Cache       │    │     Cloudflare R2 (S3)    │   │
│   │   - Business Entities     │    │   (Optional / Swappable)  │    │   - Master Audio (WAV)    │   │
│   │   - Durable Upload Jobs   │    │   - Session Caching       │    │   - Preview Audio (MP3)   │   │
│   │   - Audit Logs            │    │   - Rate Limit Buckets    │    │   - Stems (ZIP) & Artwork │   │
│   └─────────────▲─────────────┘    └───────────────────────────┘    └─────────────▲─────────────┘   │
└─────────────────┼─────────────────────────────────────────────────────────────────┼─────────────────┘
                  │ Polls with Lease Locking (`FOR UPDATE SKIP LOCKED`)             │ Stream audio
                  ▼                                                                 ▼
┌─────────────────────────────────────────────────────────────────────────────────────────────────────┐
│ DURABLE MEDIA WORKER (`cmd/worker`)                                                                 │
│   - Claims pending upload jobs from PostgreSQL                                                      │
│   - Validates direct browser uploads from R2                                                        │
│   - Extracts MP3 preview audio, analyzes duration & BPM                                             │
│   - Computes waveform peak data arrays                                                              │
│   - Publishes completion notification via WebSocket & database                                      │
└─────────────────────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 🛠 Tech Stack

- **Core Runtime**: Go `1.27.4` (Standard Library HTTP routing, `sqlx`, `lib/pq`)
- **Primary Database**: PostgreSQL `15+` (Neon Serverless in production, Docker container locally)
- **Caching & Ephemeral Store**: Redis `7+` (Supports running in no-cache mode via `REDIS_ENABLED=false`)
- **Object Storage**: Cloudflare R2 (S3-compatible API via AWS SDK for Go v2)
- **Payment Processing**: Razorpay (INR domestic) + Dodo Payments (USD international checkout)
- **Email Delivery**: Resend API
- **Observability**: Prometheus (`/metrics`) + Grafana + Structured Logging (`log/slog`)
- **API Documentation**: OpenAPI 3.0 + Swagger UI (`/docs/`)

---

## 📋 Prerequisites

Ensure the following tools are installed on your workstation:

1. **Go**: Version `1.27.4+` ([Download Go](https://go.dev/dl/))
2. **Docker & Docker Compose**: ([Get Docker](https://docs.docker.com/get-docker/))
3. **GNU Make**:
   - **Linux / macOS**: Pre-installed or via `brew install make` / `sudo apt install make`
   - **Windows**: Via Chocolatey (`choco install make`) or Scoop (`scoop install make`), or run the equivalent commands directly in PowerShell.
4. **golang-migrate CLI**: Used for manual schema migrations.
   ```bash
   # Install via Go (All Platforms)
   go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

   # Verify installation
   migrate -version
   ```
5. **Cloudflare R2 Bucket**: A Cloudflare account with an R2 bucket and API token credentials.

---

## 🚀 Local Development Quick Start

Follow these step-by-step instructions to set up and run the entire backend stack locally.

### Step 1: Clone the Repository & Install Dependencies
```bash
git clone https://github.com/saransh1220/blueprint-audio.git waveyard-backend
cd waveyard-backend

# Download Go module dependencies
go mod download
```

### Step 2: Configure Environment Variables
Copy the template configuration file to `.env`:

```bash
# Linux / macOS
cp .env.example .env

# Windows (PowerShell)
Copy-Item .env.example .env
```

Open `.env` in your editor and configure the necessary values (see [Environment Variables](#-environment-variables) for full details):
- Ensure `DB_HOST=localhost` and `DB_PORT=5432` (or your mapped port).
- Set your `JWT_SECRET` (generate using `openssl rand -base64 32`).
- Fill in your Cloudflare R2 credentials (`S3_ENDPOINT`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_BUCKET`).
- Set Razorpay test keys (`RAZORPAY_KEY_ID`, `RAZORPAY_KEY_SECRET`).

### Step 3: Start Infrastructure Containers
Create a local scrape credential once before starting Docker Compose. The API and Prometheus read the same ignored file.

```bash
openssl rand -hex 32 > .metrics-token
```

On Windows PowerShell:

```powershell
$bytes = New-Object byte[] 32
$rng = [Security.Cryptography.RandomNumberGenerator]::Create()
$rng.GetBytes($bytes)
$rng.Dispose()
[IO.File]::WriteAllText((Join-Path (Get-Location) '.metrics-token'), [Convert]::ToBase64String($bytes))
```

Launch PostgreSQL, Redis, Prometheus, and Grafana in the background using Docker Compose:

```bash
make docker-up
```

> **Note**: This starts:
> - PostgreSQL on `localhost:5432`
> - Redis on `localhost:6379`
> - Prometheus on `http://localhost:9090`
> - Grafana on `http://localhost:3000`

### Step 4: Run Database Migrations
Apply all 36+ database migrations to instantiate tables, indexes, enums, and initial schema state:

```bash
make migrate-up
```

Verify that the database version matches the latest migration:
```bash
make migrate-version
```

### Step 5: Start the API Server
In your first terminal, run the HTTP API server:

```bash
make run
```
*The server will start listening on `http://localhost:8080`.*

### Step 6: Start the Durable Media Worker
In a second terminal, launch the media worker:

```bash
make run-worker
```
*The worker starts polling PostgreSQL for pending beat upload sessions and audio processing jobs.*

### Step 7: Verify Health & API Documentation
- **Health Check**: `curl http://localhost:8080/health` (Returns `OK` with status `200`)
- **Interactive Swagger UI**: Open `http://localhost:8080/docs/` in your browser
- **OpenAPI Specification**: `http://localhost:8080/openapi.yaml` or `http://localhost:8080/openapi.json`
- **Prometheus Metrics**: `http://localhost:8080/metrics`

---

## 🧰 Make Commands Reference

The project includes a comprehensive `Makefile` to streamline development, testing, database management, and container workflows.

```bash
make help
```

### Development & Execution

| Command | Description | What It Runs Under the Hood |
| :--- | :--- | :--- |
| `make run` | Starts the HTTP API server locally | `go run ./cmd/server/main.go` |
| `make run-worker` | Starts the media processor worker locally | `go run ./cmd/worker/main.go` |
| `make build` | Compiles API and Worker binaries into `bin/` | `go build -o bin/waveyard ./cmd/server`<br>`go build -o bin/waveyard-worker ./cmd/worker` |
| `make clean` | Removes compiled binaries and coverage artifacts | `rm -rf bin/ coverage/ coverage.out coverage.html` |
| `make fmt` | Formats all Go source files according to standard Go conventions | `go fmt ./...` |
| `make lint` | Runs `golangci-lint` static analysis across packages | `golangci-lint run ./...` |

### Docker & Infrastructure

| Command | Description | What It Runs Under the Hood |
| :--- | :--- | :--- |
| `make docker-up` | Starts PostgreSQL, Redis, Prometheus, and Grafana in background | `docker-compose up -d` |
| `make docker-down` | Stops and tears down all running Docker containers | `docker-compose down` |
| `make docker-build` | Builds the multi-stage Docker container image | `docker-compose build` |
| `make dev` | Convenience command to start full dev environment | `docker-compose up -d` |
| `make logs` | Follows logs from all Docker containers | `docker-compose logs -f` |
| `make logs-api` | Follows logs specifically for the API container | `docker-compose logs -f api` |

### Database Migrations

| Command | Description | What It Runs Under the Hood |
| :--- | :--- | :--- |
| `make migrate-up` | Applies all pending migrations | `migrate -path db/migrations -database "$(DB_URL)" up` |
| `make migrate-down` | Rolls back the single most recent migration | `migrate -path db/migrations -database "$(DB_URL)" down 1` |
| `make migrate-create name=<name>` | Generates a new sequential pair of `.up.sql` and `.down.sql` files | `migrate create -ext sql -dir db/migrations -seq $(name)` |
| `make migrate-version` | Displays current active migration version and dirty status | `migrate -path db/migrations -database "$(DB_URL)" version` |
| `make migrate-force version=<N>` | Forces database version to `N` (used to recover from dirty states) | `migrate -path db/migrations -database "$(DB_URL)" force $(version)` |
| `make migrate-drop` | **DANGEROUS**: Drops all tables and migration records | `migrate -path db/migrations -database "$(DB_URL)" drop` |

### Testing & Quality Gates

| Command | Description | What It Runs Under the Hood |
| :--- | :--- | :--- |
| `make test` | Tidies modules and executes all tests | `go mod tidy && go test -v ./...` |
| `make test-unit` | Runs all unit tests | `go mod tidy && go test -v ./...` |
| `make test-integration` | Runs integration tests with build tag | `go test -v -tags=integration ./...` |
| `make test-coverage` / `make coverage` | Runs tests, generates JSON reports, line-by-line HTML dashboards, and enforces thresholds | Runs `tools/coverage-runner` and `tools/coverage-report` |
| `make coverage-report` | Generates coverage dashboards in non-blocking mode | Runs coverage reporter without failing on test errors |
| `make coverage-check COVERAGE_THRESHOLD=90` | CI gate that checks coverage against a minimum percentage threshold | Fails build if coverage falls below `COVERAGE_THRESHOLD` |

---

## 🗄 Database Migrations Guide

### How Migrations Work
All database schema changes are managed via version-controlled SQL files located in `db/migrations/`.
- Every migration consists of two paired files:
  - `0000XX_<description>.up.sql`: Applies the change (e.g., creates tables, adds columns, creates indexes).
  - `0000XX_<description>.down.sql`: Reverts the change cleanly (e.g., drops tables, removes columns).
- The `schema_migrations` table in PostgreSQL automatically tracks the current version integer and a boolean `dirty` flag.

### Creating a New Migration
To create a new migration, execute `make migrate-create` with a descriptive name:

```bash
make migrate-create name=add_promotional_banners_table
```

This creates two files in `db/migrations/`:
- `db/migrations/000037_add_promotional_banners_table.up.sql`
- `db/migrations/000037_add_promotional_banners_table.down.sql`

Edit the `.up.sql` file:
```sql
CREATE TABLE IF NOT EXISTS promotional_banners (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title VARCHAR(255) NOT NULL,
    image_url TEXT NOT NULL,
    link_url TEXT,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_promotional_banners_active ON promotional_banners(is_active);
```

Edit the `.down.sql` file:
```sql
DROP TABLE IF EXISTS promotional_banners;
```

Apply the migration:
```bash
make migrate-up
```

### Running & Rolling Back Migrations
- **Apply all unapplied migrations**:
  ```bash
  make migrate-up
  ```
- **Roll back the last migration step**:
  ```bash
  make migrate-down
  ```
- **Check current schema version**:
  ```bash
  make migrate-version
  ```

### Fixing Dirty Migration States
If a migration fails midway (e.g., due to a syntax error or constraint conflict), `golang-migrate` marks the database state as `dirty = true` to protect against data corruption.

When this happens:
1. Identify the failing migration and inspect the database error.
2. Check the current status:
   ```bash
   make migrate-version
   # Output: 36 (dirty: true)
   ```
3. Fix the SQL error in the migration file or manually rectify the table state in PostgreSQL.
4. Force the version back to the last known good version (or the target version once resolved):
   ```bash
   make migrate-force version=35
   ```
5. Re-run migrations:
   ```bash
   make migrate-up
   ```

---

## 🎧 Direct Media Upload & Worker Pipeline

Waveyard Studio uses a **Direct-to-Storage Presigned Upload Architecture** to avoid streaming multi-gigabyte audio files and stems through the API server.

```
1. Client (Producer)                2. API Server (`cmd/server`)            3. Cloudflare R2
       │                                     │                                      │
       │── POST /spec-uploads ──────────────>│ (Creates upload session in DB)       │
       │<── Returns upload_id ───────────────│                                      │
       │                                     │                                      │
       │── POST /spec-uploads/{id}/files ───>│ (Generates presigned PUT URL)        │
       │<── Returns Presigned S3 PUT URL ────│                                      │
       │                                     │                                      │
       │──────────────────────── DIRECT PRESIGNED PUT ─────────────────────────────>│ (Stores WAV/ZIP)
       │                                     │                                      │
       │── POST /spec-uploads/{id}/complete ─>│ (Marks session as "queued")         │
       │<── Status: Queued ──────────────────│                                      │
       │                                     │                                      │
       │                                     ▼                                      │
       │                            [ PostgreSQL DB ]                               │
       │                                     ▲                                      │
       │                                     │ Claims job with lease lock           │
       │                               4. Media Worker (`cmd/worker`)               │
       │                                     │                                      │
       │                                     │── Streams WAV directly from R2 ─────>│
       │                                     │── Extracts MP3 preview snippet       │
       │                                     │── Computes waveform peaks JSON array │
       │                                     │── Uploads preview & peaks to R2 ────>│
       │                                     │                                      │
       │<── WebSocket Notification: Ready ───│ (Broadcasts to user `/ws`)           │
```

1. **Initiate Session**: Producer client calls `POST /spec-uploads` to create an upload record.
2. **Presigned URL Generation**: Client calls `POST /spec-uploads/{id}/files` to request presigned PUT URLs for Master WAV, Stems ZIP, and Artwork image.
3. **Direct Upload**: Browser uploads binary files directly to Cloudflare R2.
4. **Completion Trigger**: Client confirms upload completion via `POST /spec-uploads/{id}/complete`.
5. **Worker Pickup**: `cmd/worker` polls PostgreSQL, locks the job with a 30-minute lease, streams the audio from R2, validates headers, generates an MP3 preview snippet, extracts waveform peak metadata, uploads generated assets to R2, updates the catalog record status to `completed`, and pushes a real-time notification to the user over WebSockets.

---

## 🔐 Environment Variables

The application reads configuration from environment variables (or automatically from a local `.env` file via `internal/shared/infrastructure/config`).

| Variable | Required | Default | Description & Recommended Values |
| :--- | :---: | :---: | :--- |
| **`PORT`** | No | `8080` | Port on which the HTTP server listens. |
| **`ENV`** | No | `development` | Runtime environment (`development`, `staging`, `production`). In production, cookies are enforced with `Secure; SameSite=Strict`. |
| **`API_DOCS_ENABLED`** | No | `true` | When `true`, exposes `/docs/`, `/openapi.yaml`, and `/openapi.json`. Set to `false` if docs should be hidden in public production. |
| **`ALLOWED_ORIGINS`** | **Yes** | `http://localhost:4200` | Comma-separated list of allowed browser CORS origins (e.g., `https://waveyard.studio,https://qa.waveyard.studio`). No trailing slashes. |
| **`DB_HOST`** | **Yes** | `localhost` | PostgreSQL database hostname (e.g., `ep-cool-sample.us-east-2.aws.neon.tech`). |
| **`DB_PORT`** | **Yes** | `5432` | PostgreSQL port (usually `5432`). |
| **`DB_USER`** | **Yes** | `postgres` | Database username. |
| **`DB_PASSWORD`** | **Yes** | `postgres` | Database user password. |
| **`DB_NAME`** | **Yes** | `waveyard` | Database name. |
| **`DB_SSLMODE`** | No | `disable` | PostgreSQL SSL mode (`disable` for local Docker, `require` for Neon/production). |
| **`AUTO_MIGRATE`** | No | `false` | When `true`, automatically executes all pending database migrations on server startup. Recommended for Render deploys. |
| **`MIGRATIONS_PATH`** | No | `db/migrations` | Filesystem path containing `.sql` migration files. |
| **`SUPER_ADMIN_EMAIL`** | No | *empty* | Email address to automatically bootstrap with `super_admin` role on server startup. |
| **`JWT_SECRET`** | **Yes** | *empty* | 256-bit cryptographically secure secret used for signing JWT access tokens. |
| **`JWT_EXPIRATION`** | No | `24h` | Expiration lifetime of access tokens (e.g., `15m`, `24h`). |
| **`JWT_REFRESH_EXPIRATION`** | No | `720h` | Expiration lifetime of refresh tokens (e.g., `720h` for 30 days). |
| **`GOOGLE_CLIENT_ID`** | No | *empty* | Google OAuth 2.0 Client ID for Google Social Login. |
| **`USE_S3`** | No | `true` | Enables S3/R2 storage integration (`true` or `false`). |
| **`S3_ENDPOINT`** | **Yes** | *empty* | S3 API endpoint URL (e.g., `https://<ACCOUNT_ID>.r2.cloudflarestorage.com`). |
| **`S3_PRESIGN_ENDPOINT`**| No | *`S3_ENDPOINT`* | Browser-reachable endpoint used for signing direct upload PUTs. Leave blank to match `S3_ENDPOINT`. |
| **`S3_PUBLIC_ENDPOINT`** | No | *empty* | Optional public CDN custom domain for stable asset delivery. |
| **`S3_ACCESS_KEY`** | **Yes** | *empty* | Cloudflare R2 Access Key ID. |
| **`S3_SECRET_KEY`** | **Yes** | *empty* | Cloudflare R2 Secret Access Key. |
| **`S3_BUCKET`** | **Yes** | *empty* | Cloudflare R2 bucket name (e.g., `waveyard-assets`). |
| **`S3_REGION`** | No | `auto` | Storage region (use `auto` for Cloudflare R2, `us-east-1` for AWS S3). |
| **`S3_USE_SSL`** | No | `true` | Enforces HTTPS on storage API requests (`true`). |
| **`RAZORPAY_KEY_ID`** | **Yes** | *empty* | Razorpay Key ID (`rzp_test_...` or `rzp_live_...`). |
| **`RAZORPAY_KEY_SECRET`**| **Yes**| *empty* | Razorpay Key Secret. |
| **`DODO_PAYMENTS_API_KEY`** | No | *empty* | Dodo Payments API key for global USD checkout. |
| **`DODO_PAYMENTS_PRODUCT_ID`**| No | *empty* | Dodo Payments product ID. |
| **`DODO_PAYMENTS_WEBHOOK_KEY`**| No | *empty* | Dodo Payments webhook signing secret. |
| **`FX_ENABLED`** | No | `false` | Enable automated daily FX rate refresh job. |
| **`FX_PROVIDER_URL`** | No | `https://api.frankfurter.dev/v1/latest` | Public or authenticated FX rates provider API URL. |
| **`FX_API_KEY`** | No | *empty* | Optional provider API key. |
| **`FX_REFRESH_INTERVAL`** | No | `24h` | Refresh frequency for exchange rates. |
| **`FX_STALE_AFTER`** | No | `36h` | Duration after fetch before rates are considered stale. |
| **`FX_MAX_AGE`** | No | `168h` | Maximum allowed rate age (7 days) before rate is unusable. |
| **`FX_MAX_DAILY_MOVE`** | No | `0.20` | Sanity band threshold (20%) beyond which rates are quarantined. |
| **`FX_REPORTING_CURRENCY`** | No | `INR` | Base currency used for financial reporting. |
| **`REDIS_ENABLED`** | No | `true` | Set `false` to run without Redis caching (zero-cost production setup). |
| **`REDIS_HOST`** | No | `localhost` | Redis server hostname. |
| **`REDIS_PORT`** | No | `6379` | Redis server port. |
| **`REDIS_PASSWORD`** | No | *empty* | Redis authentication password (if configured). |
| **`EMAIL_ENABLED`** | No | `true` | Set `false` to disable outbound transactional emails. |
| **`RESEND_API_KEY`** | Conditional | *empty* | Resend API key (`re_...`). Required if `EMAIL_ENABLED=true`. |
| **`EMAIL_FROM`** | Conditional | *empty* | From email address (e.g., `Waveyard Studio <noreply@waveyard.studio>`). Required if `EMAIL_ENABLED=true`. |
| **`EMAIL_REPLY_TO`** | No | *empty* | Reply-To email address. |
| **`APP_BASE_URL`** | No | `http://localhost:4200` | Frontend web application URL used for links generated in transactional emails. |
| **`WORKER_ID`** | No | `local-worker` | Identifier prefix for the worker process instance. |
| **`WORKER_POLL_INTERVAL`**| No | `2s` | Polling frequency for claiming background upload jobs. |
| **`WORKER_LEASE_DURATION`**| No | `30m` | Duration for which a worker claims an upload job lease. |

---

## 🚢 Production Deployment Guide

This section describes the deployment setup using **Render** (Compute), **Neon** (Serverless PostgreSQL), and **Cloudflare** (R2 Object Storage & Pages Frontend).

```
                      ┌───────────────────────────────────────────────┐
                      │    Cloudflare Pages (Angular Frontend)        │
                      │    https://qa.waveyard.studio                 │
                      └──────────────────────┬────────────────────────┘
                                             │ API Requests
                                             ▼
                      ┌───────────────────────────────────────────────┐
                      │    Render Web Service (Go API Server)         │
                      │    https://api-qa.waveyard.studio             │
                      │    Runs: ./server                             │
                      └──────┬───────────────────────────────┬────────┘
                             │                               │
             Direct Presign  │                               │ SQL Queries
             & Audio URLs    │                               │ & Migrations
                             ▼                               ▼
┌─────────────────────────────────────────┐     ┌─────────────────────────────────────────┐
│     Cloudflare R2 Object Storage        │     │         Neon Serverless Postgres        │
│     Bucket: waveyard-assets             │     │         Database: waveyard_prod         │
│     - Audio WAV, MP3 previews, Stems    │     │         `sslmode=require`               │
└────────────────────▲────────────────────┘     └────────────────────▲────────────────────┘
                     │ Stream audio                                  │ Claim jobs with
                     │ & upload previews                             │ `FOR UPDATE SKIP LOCKED`
                     └───────────────────────┬───────────────────────┘
                                             │
                      ┌──────────────────────┴────────────────────────┐
                      │    Render Background Worker                   │
                      │    waveyard-worker                            │
                      │    Runs: ./worker                             │
                      └───────────────────────────────────────────────┘
```

### Step 1: Provision Neon PostgreSQL
1. Sign in to [Neon](https://neon.tech) and click **New Project** (`waveyard-prod`).
2. Choose a region closest to your compute and target audience.
3. In the project dashboard, click **Connect** and retrieve your connection string:
   ```text
   postgresql://USER:PASSWORD@ep-sample-12345.us-east-2.aws.neon.tech/waveyard_prod?sslmode=require
   ```
4. Note down the individual components:
   - `DB_HOST`: `ep-sample-12345.us-east-2.aws.neon.tech`
   - `DB_PORT`: `5432`
   - `DB_USER`: `USER`
   - `DB_PASSWORD`: `PASSWORD`
   - `DB_NAME`: `waveyard_prod`
   - `DB_SSLMODE`: `require`

### Step 2: Setup Cloudflare R2 Storage
1. In the Cloudflare Dashboard, navigate to **R2 Object Storage** and create a bucket (e.g., `waveyard-assets`).
2. Generate an API Token with **Object Read & Write** permissions.
3. Configure **CORS Policy** on the R2 bucket to allow browser direct uploads:
   ```json
   [
     {
       "AllowedOrigins": [
         "https://waveyard.studio",
         "https://qa.waveyard.studio",
         "http://localhost:4200"
       ],
       "AllowedMethods": ["GET", "PUT", "HEAD"],
       "AllowedHeaders": ["*"],
       "ExposeHeaders": ["ETag"],
       "MaxAgeSeconds": 3600
     }
   ]
   ```

### Step 3: Deploy Backend API on Render
1. In the [Render Dashboard](https://dashboard.render.com), click **New +** → **Web Service**.
2. Connect your Git repository.
3. Configure the service settings:
   - **Name**: `waveyard-api`
   - **Region**: Same general geographic region as your Neon database.
   - **Branch**: `main`
   - **Runtime**: `Docker`
   - **Dockerfile Path**: `Dockerfile`
   - **Instance Type**: `Free` or `Starter`
   - **Auto-Deploy**: `Yes`
4. Add Environment Variables in the Render **Environment** tab:
   ```env
   PORT=8080
   ENV=production
   AUTO_MIGRATE=true
   MIGRATIONS_PATH=db/migrations
   REDIS_ENABLED=false

   DB_HOST=ep-sample-12345.us-east-2.aws.neon.tech
   DB_PORT=5432
   DB_USER=your_neon_user
   DB_PASSWORD=your_neon_password
   DB_NAME=waveyard_prod
   DB_SSLMODE=require

   JWT_SECRET=your_long_random_production_secret_32_bytes
   JWT_EXPIRATION=24h
   JWT_REFRESH_EXPIRATION=720h

   ALLOWED_ORIGINS=https://qa.waveyard.studio,https://waveyard.studio
   APP_BASE_URL=https://qa.waveyard.studio

   USE_S3=true
   S3_ENDPOINT=https://<ACCOUNT_ID>.r2.cloudflarestorage.com
   S3_ACCESS_KEY=your_r2_access_key
   S3_SECRET_KEY=your_r2_secret_key
   S3_BUCKET=waveyard-assets
   S3_USE_SSL=true
   S3_REGION=auto

   RAZORPAY_KEY_ID=rzp_live_...
   RAZORPAY_KEY_SECRET=...

   EMAIL_ENABLED=true
   RESEND_API_KEY=re_...
   EMAIL_FROM=Waveyard Studio <noreply@waveyard.studio>
   ```

### Step 4: Deploy Media Worker on Render
To process audio files asynchronously, create a second service on Render using the exact same repository and Docker image:
1. In Render Dashboard, click **New +** → **Background Worker**.
2. Connect the same repository.
3. Configure:
   - **Name**: `waveyard-worker`
   - **Runtime**: `Docker`
   - **Dockerfile Path**: `Dockerfile`
   - **Docker Command**: `./worker`
4. Copy the same `DB_*`, `S3_*`, and `ENV=production` variables from the Web Service.
5. Add worker-specific variables:
   ```env
   WORKER_ID=render-worker
   WORKER_POLL_INTERVAL=2s
   WORKER_LEASE_DURATION=30m
   ```
   *(Do NOT set `AUTO_MIGRATE=true` on the worker; the API Web Service handles migrations).*

### Step 5: Frontend Connection & CORS Configuration
1. For proper **HTTP-Only SameSite cookie transmission**, use a shared apex domain for API and Frontend:
   - Frontend: `https://qa.waveyard.studio` (or `https://waveyard.studio`)
   - Backend API: `https://api-qa.waveyard.studio` (or `https://api.waveyard.studio`)
2. In Render, navigate to **Custom Domains** and add `api-qa.waveyard.studio`.
3. Add the resulting CNAME record at your DNS provider (Cloudflare / Porkbun).
4. Update frontend environment (`environment.prod.ts`) with `apiUrl: 'https://api-qa.waveyard.studio'`.

---

### What Happens Under the Hood During Deployment

When a new commit is pushed to `main` (or you trigger a manual deploy in Render):

1. **Multi-Stage Docker Build**:
   ```dockerfile
   # Stage 1: Build binaries using Go 1.27.4 on Alpine
   go build -o /out/server ./cmd/server
   go build -o /out/worker ./cmd/worker

   # Stage 2: Final lean Alpine runtime container (~25MB)
   COPY --from=builder /out/server ./server
   COPY --from=builder /out/worker ./worker
   COPY --from=builder /app/db/migrations ./db/migrations
   CMD ["./server"]
   ```
2. **Container Boot & Automatic Migration Execution**:
   - The Web Service container boots and invokes `./server`.
   - `main.go` reads `AUTO_MIGRATE=true` and calls `pkg/migration.AutoMigrate()`.
   - `AutoMigrate()` connects to the Neon PostgreSQL database with `sslmode=require`, checks the `schema_migrations` table, and executes all pending `.up.sql` files sequentially.
   - Once migrations complete, the connection pool is established.
3. **Module Initialization & Route Registration**:
   - Domain modules (`auth`, `catalog`, `payment`, `user`, `notification`, `analytics`, `admin`, `filestorage`) initialize their repositories and services.
   - If `SUPER_ADMIN_EMAIL` is configured, it bootstraps the super admin user.
   - HTTP routes and middleware are registered on standard `net/http.ServeMux`.
4. **Binding & Readiness**:
   - The HTTP server binds to `:PORT` (`8080`).
   - Render's health check verifies the port and routes public traffic to the new revision.
5. **Background Worker Execution**:
   - The Background Worker container boots with `./worker`.
   - Connects to Neon and starts the polling loop, querying `spec_uploads` for jobs with status `queued` using PostgreSQL row-level locks (`FOR UPDATE SKIP LOCKED`).

---

### Post-Deployment Smoke Test

Verify the deployment with these automated checks:

```bash
# 1. Health Check
curl -I https://api-qa.waveyard.studio/health
# Expected: HTTP/1.1 200 OK

# 2. Check OpenAPI specification endpoint
curl -I https://api-qa.waveyard.studio/openapi.yaml
# Expected: HTTP/1.1 200 OK

# 3. Test user registration / login flow
curl -X POST https://api-qa.waveyard.studio/login \
  -H "Content-Type: application/json" \
  -d '{"email":"producer@example.com","password":"YourPassword123!"}'
```

---

## 📡 API Routes Reference

Below is a categorized summary of all HTTP endpoints exposed by the API Gateway:

### 🔑 Authentication (`/register`, `/login`, `/auth/*`)
- `POST /register` — Register a new producer/listener account
- `POST /login` — Authenticate with email/password; returns JWT and sets HTTP-only refresh cookie
- `POST /auth/google` — Authenticate using Google OAuth 2.0 ID token
- `POST /auth/refresh` — Exchange HTTP-only refresh token for a new access token
- `POST /auth/logout` — Invalidate user refresh token session and clear cookies
- `POST /auth/verify-email` — Verify email using token sent via Resend (Rate limited)
- `POST /auth/resend-verification` — Resend verification email token (Rate limited)
- `POST /auth/forgot-password` — Request password reset email (Rate limited)
- `POST /auth/reset-password` — Reset password using token (Rate limited)
- `GET  /me` — Retrieve currently authenticated user context (Protected)

### 🎵 Catalog & Specs (`/specs`, `/catalog/*`, `/spec-uploads/*`)
- `GET  /catalog/home` — Get featured beats, top trending specs, and curated genres
- `GET  /specs` — Search and filter specs (by category, genre, BPM, key, price, query)
- `GET  /specs/{id}` — Retrieve detailed spec metadata and audio preview information
- `POST /spec-uploads` — Initiate a direct presigned upload session (Producer only)
- `PUT  /spec-uploads/{id}/metadata` — Save draft metadata for an upload session
- `POST /spec-uploads/{id}/files` — Request presigned S3 PUT URL for asset upload
- `POST /spec-uploads/{id}/files/{assetID}/complete` — Confirm individual file upload
- `POST /spec-uploads/{id}/complete` — Mark upload session complete and queue for worker
- `GET  /spec-uploads/{id}` — Check status of an upload processing job
- `PATCH /specs/{id}` — Update spec metadata or pricing (Producer/Owner only)
- `DELETE /specs/{id}` — Soft-delete a spec (Producer/Owner only)
- `POST /specs/{id}/download-free` — Download tagged MP3 for free-tier specs

### 👤 Users & Profiles (`/users/*`)
- `PATCH /users/profile` — Update bio, social links, and display preferences
- `POST  /users/profile/avatar` — Upload and optimize profile avatar image
- `POST  /users/profile/banner` — Upload store banner background image
- `GET   /users/{id}/public` — Get public producer storefront profile and statistics
- `GET   /users/{id}/specs` — List published specs by producer ID

### 💳 Payments & Licenses (`/orders/*`, `/payments/*`, `/licenses/*`)
- `POST /orders` — Create a new purchase order and checkout session (Razorpay / Dodo)
- `GET  /orders` — List authenticated user's order history
- `GET  /orders/{id}` — Retrieve specific order invoice details
- `POST /payments/verify` — Verify Razorpay signature and generate licenses
- `POST /webhooks/dodo` — Handle asynchronous Dodo Payments webhook notifications
- `GET  /licenses` — List acquired user licenses
- `GET  /licenses/{id}/downloads` — Generate secure time-limited presigned download URLs for WAV/Stems
- `GET  /orders/producer` — List sales orders for producer dashboard

#### Payment reconciliation and rollout

- Subscribe Razorpay to `payment.captured`, `refund.created`, `refund.processed` and `refund.failed`. Subscribe Dodo to `payment.succeeded`, `refund.succeeded` and `refund.failed`. Configure each signing secret in the environment used by the API.
- A buyer can retry an unresolved order with its original `Idempotency-Key`. Operators with `admin:orders:refund` can call `POST /admin/orders/{id}/reconcile-checkout` to repeat provider lookups. HTTP 202 means provider evidence is still pending. Check the provider dashboard and pass its Razorpay order ID or Dodo payment ID as `provider_reference` for direct verification. Use webhook replay when an event needs redelivery.
- Refund reconciliation keeps access active while the provider is processing a refund. A confirmed full refund or confirmed partials that add up to the captured payment update the payment, order and license in one transaction. An external partial refund blocks a new full-refund request.
- Apply the producer-handle migration `000048` before `000049` if that feature ships in the same rollout. If the payment migrations ship first, give the producer-handle migration the next unused version before merging it. Before applying `000049` to an existing database, check `order_refunds` for multiple rows with `status <> 'failed'` for the same `payment_id` and reconcile any duplicates against the provider.


### 🔔 Notifications & Real-Time (`/notifications/*`, `/ws`)
- `GET   /ws` — Establish WebSocket connection for real-time push events (Protected)
- `GET   /notifications` — List user notifications (paginated)
- `GET   /notifications/unread-count` — Get total count of unread notifications
- `PATCH /notifications/{id}/read` — Mark single notification as read
- `PATCH /notifications/read-all` — Mark all notifications as read

### 📊 Analytics (`/analytics/*`, `/specs/{id}/analytics`, `/me/favorites`)
- `POST /specs/{id}/play` — Track playback event for audio ranking algorithms
- `POST /specs/{id}/favorite` — Toggle like/favorite on a spec
- `GET  /me/favorites` — List authenticated user's favorited beats (`limit`, `cursor` query parameters; returns `items`, `has_more`, `next_cursor`)
- `GET  /specs/{id}/analytics` — Producer analytics for a specific beat
- `GET  /analytics/overview` — Overall producer dashboard performance metrics
- `GET  /analytics/top-specs` — Producer's top performing specs

### 🛡 Super Admin (`/admin/*`)
- `GET    /admin/users` — List platform users with filter options
- `GET    /admin/users/{id}` — Get complete user details and session state
- `PATCH  /admin/users/{id}/system-role` — Assign or revoke `super_admin` role
- `PATCH  /admin/users/{id}/status` — Suspend or reactivate user accounts
- `GET    /admin/specs` — Moderate all catalog specs
- `PATCH  /admin/specs/{id}` — Edit or override any spec listing
- `DELETE /admin/specs/{id}` — Force delete a spec
- `GET    /admin/orders` — Platform-wide transaction audit log
- `GET    /admin/licenses` — Platform-wide license records
- `GET    /admin/analytics/overview` — Executive platform metrics
- `GET    /admin/audit-log` — Immutable administrative audit log

---

## 🧪 Testing & Coverage Workflow

Waveyard Studio includes a test suite with coverage enforcement tooling located in `tools/`.

### Run Unit & Integration Tests
```bash
# Run all tests
make test

# Run unit tests only
make test-unit

# Run integration tests (requires active database)
make test-integration
```

### Full Coverage Dashboard Generation
```bash
make coverage
```
This generates:
- `coverage/coverage.html` — Interactive visual dashboard
- `coverage/coverage-details.html` — Line-by-line syntax-highlighted code coverage view
- `coverage/summary.md` — Markdown summary report
- `coverage/summary.json` — Structured JSON coverage metrics
- `coverage/test-report.jsonl` — Detailed per-test execution log

### Enforce CI Coverage Threshold
```bash
# Fail if test coverage falls below 90%
make coverage-check COVERAGE_THRESHOLD=90
```

---

## 📄 License & Maintainers

- **Author**: Saransh Sharma
- **Product**: [Waveyard Studio](https://waveyard.studio)
- **License**: Proprietary / All Rights Reserved

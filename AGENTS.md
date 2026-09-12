# CPA USAGE KEEPER

> Parent: [../CLIProxyAPIPlus/AGENTS.md](../CLIProxyAPIPlus/AGENTS.md)

**Latest Tag:** v1.8.6-1
**Branch:** main

## OVERVIEW

Fork of [CPA Usage Keeper](https://github.com/Willxup/cpa-usage-keeper). Standalone usage persistence and visualization service that consumes CPA Redis usage events, persists to SQLite, exposes aggregation APIs, and serves a built-in web dashboard.

## STRUCTURE

```text
cpa-usage-keeper/
├── cmd/server/main.go        # entry point
├── internal/
│   ├── app/                  # application lifecycle, options, init/shutdown
│   ├── api/                  # Gin HTTP API handlers
│   ├── poller/               # Redis queue consumer, HTTP pull source, backoff, subscribe
│   ├── service/              # business logic: usage, pricing, sync, identities, API keys
│   ├── repository/           # SQLite persistence, migrations, DTOs
│   ├── quota/                # usage stats, quota types, errors
│   ├── entities/             # domain models
│   ├── cpa/                  # CPA client: API calls, DTOs, provider config, auth files
│   ├── config/               # env-based config
│   ├── auth/                 # login/password protection
│   ├── backup/               # SQLite backup
│   ├── logging/              # structured logging
│   ├── redact/               # sensitive data masking
│   ├── openrouter/           # OpenRouter model pricing sync
│   ├── updatecheck/          # version update checker
│   ├── version/              # build version info
│   ├── timeutil/             # time helpers
│   └── benchmark/            # performance benchmarks
├── web/                      # React dashboard (usage, credentials, analysis pages)
├── deploy/linux/             # systemd unit files
└── Makefile                  # verify targets (backend + frontend + docker)
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| App lifecycle | `internal/app/` | Init, options, graceful shutdown. |
| Redis consumption | `internal/poller/` | Pull/subscribe sources, backoff, inbox writer. |
| Usage business logic | `internal/service/usage_service.go`, `internal/service/redis_usage.go` | Aggregation and persistence. |
| Pricing | `internal/service/pricing_service.go` | Model cost estimation. |
| Sync from CPA | `internal/service/sync.go` | Metadata pull from CPA management API. |
| API keys | `internal/service/cpa_api_keys_service.go` | Per-key usage tracking. |
| SQLite schema | `internal/repository/migration/` | 76 migration files; schema evolution. |
| API endpoints | `internal/api/` | Gin routes for usage, pricing, credentials, system. |
| Web dashboard | `web/src/pages/`, `web/src/components/usage/` | React UI for usage, analysis, credentials. |

## CONVENTIONS

- Depends on CLIProxyAPIPlus with `usage-statistics-enabled: true`.
- Redis queue consumption uses backoff and reconnect logic.
- SQLite migrations are sequential numbered files in `repository/migration/`.
- API base URL and management key come from environment variables (`CPA_BASE_URL`, `CPA_MANAGEMENT_KEY`).
- Optional password protection via `AUTH_ENABLED` + `LOGIN_PASSWORD`.
- Docker Compose is the recommended deployment path alongside CPA.

## ANTI-PATTERNS

- Do not connect to CPA Redis without the management key.
- Do not skip migration files; they must run in order.
- Do not log or expose API keys or management secrets in responses.

## COMMANDS

```bash
go build ./cmd/server
```

Do not run tests for this subproject unless the user explicitly asks for them. This includes Go tests and web test commands.

## NOTES

- Requires CPA Redis usage queue endpoint (`REDIS_QUEUE_ADDR`).
- Web dashboard builds as a separate SPA (not single-file like Management Center).
- Known issue: CommandCode source may show as "Deleted" in credentials; cause under investigation.

## SUB-DOCUMENTS

```text
web/AGENTS.md
```

## Push / PR 규칙

- forked upstream에는 절대 Pull Request를 생성하지 않는다. 모든 push는 `origin`(jc01rho) 브랜치에만 수행한다. upstream remote는 fetch/merge 전용이다.

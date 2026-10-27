<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./assets/keeper-logo-dark.svg" />
    <source media="(prefers-color-scheme: light)" srcset="./assets/keeper-logo-light.svg" />
    <img src="./assets/keeper-logo-light.svg" alt="Keeper" width="560" />
  </picture>
</p>

<h1 align="center">CPA Usage Keeper</h1>

<p align="center"><em>Every flow leaves a trace.</em></p>

<p align="center">
  <a href="./README.md"><strong>English</strong></a> ｜ <a href="./README.zh.md">简体中文</a> ｜ <a href="./README.ko.md">한국어</a>
</p>

<p align="center">
  <a href="https://github.com/jc01rho/cpa-usage-keeper/releases/latest"><img src="https://img.shields.io/github/v/release/jc01rho/cpa-usage-keeper?style=flat-square" alt="Latest release" /></a>
  <a href="https://github.com/jc01rho/cpa-usage-keeper/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/jc01rho/cpa-usage-keeper/ci.yml?branch=main&amp;style=flat-square&amp;label=CI" alt="CI status" /></a>
  <a href="https://github.com/jc01rho/cpa-usage-keeper/releases/latest"><img src="https://img.shields.io/badge/Linux-FCC624?style=flat-square&amp;logo=linux&amp;logoColor=black" alt="Linux supported" /></a>
  <a href="https://github.com/jc01rho/cpa-usage-keeper/releases/latest"><img src="https://img.shields.io/badge/macOS-A2AAAD?style=flat-square&amp;logo=apple&amp;logoColor=black" alt="macOS supported" /></a>
  <a href="https://github.com/jc01rho/cpa-usage-keeper/releases/latest"><img src="https://img.shields.io/badge/Windows-0078D4?style=flat-square&amp;logo=data:image/svg%2Bxml;base64,PHN2ZyB4bWxucz0iaHR0cDovL3d3dy53My5vcmcvMjAwMC9zdmciIHZpZXdCb3g9IjAgMCAyNCAyNCI+PHBhdGggZmlsbD0iI2ZmZiIgZD0iTTIgMy41IDExIDJ2OUgyem0xMC0xLjdMMjIgLjNWMTFIMTJ6TTIgMTJoOXY5TDIgMTkuNXptMTAgMGgxMHYxMC43bC0xMC0xLjV6Ii8+PC9zdmc%2B" alt="Windows supported" /></a>
  <a href="./LICENSE"><img src="https://img.shields.io/github/license/jc01rho/cpa-usage-keeper?style=flat-square" alt="MIT License" /></a>
</p>

> **This is a fork** of [CPA Usage Keeper](https://github.com/Willxup/cpa-usage-keeper) maintained by [jc01rho](https://github.com/jc01rho), intended for use with the [CLIProxyAPIPlus](https://github.com/jc01rho/CLIProxyAPIPlus) fork.
>
> This fork tracks upstream closely and primarily differs in its integration target (CLIProxyAPIPlus instead of upstream CLIProxyAPI).

CPA Usage Keeper is a standalone persistence and analytics dashboard for [CLIProxyAPI (CPA)](https://github.com/router-for-me/CLIProxyAPI). It stores CPA usage in SQLite, pulls CPA configuration and credential data, and provides views for usage, cost, request health, quotas, and model/API statistics.

<p align="center">
  <strong><a href="#keeper-only">Deploy Keeper</a>&emsp;|&emsp;<a href="#cpa--keeper">Deploy CPA + Keeper</a>&emsp;|&emsp;<a href="#configuration">Configuration</a></strong>
</p>

## Screenshots

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./assets/screenshots/overview-dark.png" />
    <source media="(prefers-color-scheme: light)" srcset="./assets/screenshots/overview-light.png" />
    <img src="./assets/screenshots/overview-light.png" alt="CPA Usage Keeper Overview" width="49%" />
  </picture>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./assets/screenshots/analysis-dark.png" />
    <source media="(prefers-color-scheme: light)" srcset="./assets/screenshots/analysis-light.png" />
    <img src="./assets/screenshots/analysis-light.png" alt="CPA Usage Keeper Analysis" width="49%" />
  </picture>
</p>
<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./assets/screenshots/auth-files-dark.png" />
    <source media="(prefers-color-scheme: light)" srcset="./assets/screenshots/auth-files-light.png" />
    <img src="./assets/screenshots/auth-files-light.png" alt="CPA Usage Keeper Auth Files" width="49%" />
  </picture>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./assets/screenshots/ai-provider-dark.png" />
    <source media="(prefers-color-scheme: light)" srcset="./assets/screenshots/ai-provider-light.png" />
    <img src="./assets/screenshots/ai-provider-light.png" alt="CPA Usage Keeper AI Provider" width="49%" />
  </picture>
</p>
<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./assets/screenshots/login-dark.png" />
    <source media="(prefers-color-scheme: light)" srcset="./assets/screenshots/login-light.png" />
    <img src="./assets/screenshots/login-light.png" alt="CPA Usage Keeper Login" width="49%" />
  </picture>
</p>

## Why This Fork

Everything upstream ships, plus the pieces needed to run against [CLIProxyAPIPlus](https://github.com/jc01rho/CLIProxyAPIPlus):

| Addition | What it does |
| --- | --- |
| **Multi-instance support** | Register several CPA instances in one Keeper. Filter usage and request events per instance; a deterministic `Legacy` instance keeps pre-migration data intact. |
| **Keeper export protocol (`/api/v1/export/*`)** | Bearer-credential push API for usage batches and metadata (auth files, API keys, provider identities). Strict JSON validation, replay-safe ingestion, Argon2id-hashed credentials. See the [operator runbook](docs/keeper-export.md). |
| **tokscale bridge** | `scripts/tokscale_bridge.py` exports keeper-priced usage to gjc JSONL for [tokscale](https://github.com/junhoyeo/tokscale) analytics and leaderboard submission, with optional systemd timer. See [docs/tokscale-bridge.md](docs/tokscale-bridge.md). |
| **OpenRouter pricing sync** | "Auto-fill from OpenRouter" adds OpenRouter as a pricing sync source (review, then apply); optional `OPENROUTER_API_KEY`. |
| **Configurable storage retention** | `USAGE_EVENTS_HOT_RETENTION_DAYS`, `USAGE_RAW_RETENTION_DAYS`, `ERROR_EVENTS_RETENTION_DAYS`, and `USAGE_OVERVIEW_HOURLY_RETENTION_DAYS` bound how long each storage layer keeps data, so a long-running host can reclaim the SQLite file. |

## Features

- [Keeper export operator runbook](docs/keeper-export.md) — migration, credentials, ingestion, recovery, and release operations.
- **Keep your history**: persist CPA usage in SQLite with scheduled backups.
- **Understand costs**: analyze usage, caching, and estimated costs by model, API Key, and provider.
- **Investigate requests**: inspect and export request details, and explore success rates, time to first token (TTFT), and total latency.
- **Track quotas**: monitor credential health and remaining quotas, refresh quotas, edit priorities, and explore Codex quota history.
- **Share scoped access**: provide a read-only usage view for an individual CPA API Key.

CPA plugin embedding in CPAMC is also available. Deploy with Docker Compose, Homebrew, or binaries; login protection is enabled by default.

## Sponsors and Special Thanks

- Thanks to [CLIProxyAPI (CPA)](https://github.com/router-for-me/CLIProxyAPI) for providing the upstream CPA foundation and data source this project builds on.
- Thanks to [@YouShouldBetOnMe](https://github.com/YouShouldBetOnMe) for supporting CPA Usage Keeper.
- Thanks to the CPA discussion group for their discussions and feedback.

## Quick Start

> Keeper supports CPA v8.0.12+. Enable usage statistics by setting `observability.usage.usage-statistics-enabled` to `true`.<br>
> When multiple collectors share one CPA instance, all must use subscription mode to avoid interrupted collection or missing data.

Docker Compose is recommended; no Go, Node.js, or source build is needed. Choose a deployment below. For an existing CPA, have its address and management key ready, and choose a Keeper login password.

| Setup | Recommended path | Architectures |
| --- | --- | --- |
| New CPA + Keeper deployment | [Docker Compose: CPA + Keeper](#docker-compose-recommended) (builds the image locally) | `linux/amd64`, `linux/arm64` |
| Existing CPA deployment | [Docker Compose: Keeper only](#docker-compose-recommended) (builds the image locally) | `linux/amd64`, `linux/arm64` |
| Existing CPA, Docker CLI preferred | [Docker](#docker-cpa-already-runs-on-the-host) (builds the image locally) | `linux/amd64`, `linux/arm64` |
| macOS | [macOS binary](#macos-binary) | `amd64`, `arm64` |
| Linux without containers | [Linux binary](#linux-binary) | `amd64`, `arm64` |
| Windows | [Windows binary](#windows-binary) | `amd64`, `arm64` |

> This fork does not publish a Docker image or a Homebrew tap. Container deployments build the image from the included `Dockerfile`; macOS uses the release binaries.

Login protection is enabled by default. Configure `LOGIN_PASSWORD` before starting Keeper, or explicitly set `AUTH_ENABLED=false` only when access is reliably isolated by the deployment environment.

## Benchmark

Production-style `linux/amd64` capacity measurements for sustained ingestion, Dashboard latency, CPU utilization, and Keeper cgroup peak memory are available in the [Capacity Benchmark Report](./internal/benchmark/reports/capacity/README.md).

## Developer Reference

<details>
<summary>Project structure, local setup, and tests</summary>

### Project Structure

```text
cmd/server/              Application entry point
internal/api/            HTTP routes and handlers
internal/app/            Application wiring and startup
internal/auth/           Sessions and access control
internal/poller/         CPA usage and metadata synchronization
internal/repository/     SQLite persistence and aggregations
internal/service/        Usage, pricing, and identity services
internal/quota/          Provider quota refresh and inspection
internal/benchmark/      Capacity suite, reports, manifests, and legacy microbenchmarks
deploy/                  Deployment templates
web/                     React + TypeScript frontend
```

### Local Development

#### Prerequisites

- Go 1.26.10+
- Node.js 24.22.1+
- npm
- A running [CLIProxyAPI (CPA)](https://github.com/router-for-me/CLIProxyAPI) instance

If you use nvm, run `nvm install` and `nvm use` in the repository root to select the version in `.nvmrc`.

#### Run Locally

1. Copy `.env.example` to `.env`, then set at least `CPA_BASE_URL` and `CPA_MANAGEMENT_KEY`.

```bash
cp .env.example .env
vim .env
```

2. Start the backend.

```bash
go run ./cmd/server/main.go
```

3. In another terminal, install frontend dependencies and start the development server.

```bash
npm --prefix ./web ci
npm --prefix ./web run dev -- --host 127.0.0.1
```

Open `http://127.0.0.1:5173`. The frontend proxies `/api` to `http://127.0.0.1:8080`; override it with `VITE_API_PROXY_TARGET` when the backend uses another port.

#### Tests

Run the full verification baseline:

```bash
make verify
```

See the [development guide](./docs/development.md) for individual test commands and chart time-handling conventions.

</details>

## Deployment

### Docker Compose (Recommended)

#### CPA + Keeper

Save the following as `docker-compose.yml`, then replace the management key and login password:

```yaml
services:
  cli-proxy-api:
    image: eceasy/cli-proxy-api:latest
    container_name: cli-proxy-api
    restart: unless-stopped
    ports:
      - "8317:8317"
      - "1455:1455"
    volumes:
      - ./cpa/config.yaml:/CLIProxyAPI/config.yaml
      - ./cpa/auths:/root/.cli-proxy-api
      - ./cpa/logs:/CLIProxyAPI/logs
    networks:
      - cpa-network

  cpa-usage-keeper:
    build:
      context: https://github.com/jc01rho/cpa-usage-keeper.git
    container_name: cpa-usage-keeper
    restart: unless-stopped
    depends_on:
      - cli-proxy-api
    ports:
      - "8080:8080"
    environment:
      TZ: Asia/Shanghai # Sets the container timezone; log timestamps use this timezone.
      CPA_BASE_URL: http://cli-proxy-api:8317
      CPA_MANAGEMENT_KEY: replace-with-your-management-key
      REDIS_QUEUE_ADDR: cli-proxy-api:8317
      AUTH_ENABLED: true
      LOGIN_PASSWORD: ${KEEPER_LOGIN_PASSWORD:?set KEEPER_LOGIN_PASSWORD}
    volumes:
      - ./keeper:/data
    networks:
      - cpa-network

networks:
  cpa-network:
    driver: bridge
```

Set `KEEPER_LOGIN_PASSWORD` in the shell or the Compose `.env` file before starting.

This fork does not publish a Docker image, so the `build` above compiles Keeper from this repository. To use a local checkout instead, replace the `build` block with `build: .` inside a clone.

Use this download command for a new deployment only; edit an existing configuration directly to avoid overwriting it.

**2. Configure CPA and Keeper**

Edit the existing settings in `cpa/config.yaml` as listed below, preserving YAML nesting and indentation. Dots in the table denote nested paths, not literal YAML keys to add:

| Setting | What to configure |
| --- | --- |
| `management.allow-remote` | Set to `true` so Keeper can access CPA management features from another container. |
| `management.secret-key` | Set a private management key and use the same original key for `CPA_MANAGEMENT_KEY` in `docker-compose.yml`. |
| `observability.usage.usage-statistics-enabled` | Set to `true` to enable usage statistics. |
| `access.api-keys` | Replace the example keys with your own client API keys; these are separate from the CPA management key and Keeper login password. |

Keep `server.host` as an empty string, `server.port` as `8317`, and `oauth.auth-dir` as `"~/.cli-proxy-api"` to match the container network and volume mounts in the template.

Edit the downloaded `docker-compose.yml` and fill in two values under Keeper's `environment`:

- `CPA_MANAGEMENT_KEY`: use the same original management key configured in CPA.
- `LOGIN_PASSWORD`: replace the empty string `""` with your own Keeper login password.

For optional environment-file setup, see [advanced configuration](./docs/configuration.md#optional-compose-environment-file).

**3. Start and open Keeper**

```bash
docker compose up -d
```

Open `http://your-server-address:8080` and sign in with your Keeper login password.

For a new deployment, add model credentials in CPA and make a model request to generate usage records.

CPA data is stored under `./cpa`, and Keeper data is stored under `./keeper`.

#### Keeper Only

When CPA is already deployed, use the repository's Keeper-only Compose template. The template references the upstream image; this fork does not publish one, so point `build` at a local clone (or a remote `context`) instead of `image`:

```bash
cp deploy/docker-compose.example.yml docker-compose.yml
cp .env.example .env
vim .env
# in docker-compose.yml, replace `image: ghcr.io/willxup/cpa-usage-keeper:latest` with:
#   build: .
```

For CPA running on the Docker host, start with:

```env
CPA_BASE_URL=http://host.docker.internal:8317
CPA_MANAGEMENT_KEY=replace-with-your-management-key
AUTH_ENABLED=true
LOGIN_PASSWORD=
```

Set a private `LOGIN_PASSWORD` before starting the container.

Whether CPA runs on the Docker host or another machine, it must allow remote management and listen on an address reachable from the Keeper container; a listener bound only to `127.0.0.1` is not reachable. Set `management.allow-remote: true` and check `server.host`.

For other network layouts, set `CPA_BASE_URL` to a CPA address reachable from the container. Set `REDIS_QUEUE_ADDR` only when the Redis/RESP address differs from the automatically derived address.

**3. Start and open Keeper**

```bash
docker compose up -d
```

Run `docker compose up -d` to start Keeper and `docker compose down` to stop it.

Keeper data is stored under `./data` by the provided template.

#### Access And Routine Management

For either Compose setup, sign in at `http://your-server-address:8080` with your Keeper password and run `docker compose down` to stop it. View logs from the deployment directory:

```bash
docker compose logs --tail=100 -f cpa-usage-keeper
```

To update Keeper, keep your configuration and data directories and run:

```bash
docker compose pull cpa-usage-keeper
docker compose up -d cpa-usage-keeper
```

#### No Data After Startup?

- Check that CPA usage statistics are enabled: `observability.usage.usage-statistics-enabled` must be `true`.
- Ensure Keeper can reach `CPA_BASE_URL` and that `CPA_MANAGEMENT_KEY` matches the CPA management key.
- Check that CPA has received new model requests. If data is still missing, use the log command above to look for connection or authentication errors.

### Docker (CPA Already Runs On The Host)

Build the image from a local clone, then use the same `.env` values as the Keeper-only Compose setup:

```bash
docker build -t cpa-usage-keeper .
docker run -d \
  --name cpa-usage-keeper \
  --add-host=host.docker.internal:host-gateway \
  -p 8080:8080 \
  -v "$(pwd)/keeper:/data" \
  --env-file .env \
  cpa-usage-keeper
```

### macOS Binary

Download the `darwin_amd64` or `darwin_arm64` archive from [Releases](https://github.com/jc01rho/cpa-usage-keeper/releases/latest), then extract and run it:

```bash
mkdir -p cpa-usage-keeper
tar -xzf ./cpa-usage-keeper_*_darwin_*.tar.gz -C cpa-usage-keeper --strip-components=1
cd cpa-usage-keeper
cp .env.example .env
vim .env
./cpa-usage-keeper
```

Set `CPA_BASE_URL`, `CPA_MANAGEMENT_KEY`, and a private `LOGIN_PASSWORD` before starting. To run Keeper as a background service on macOS, create a `launchd` plist or use a process manager of your choice.

### Linux Binary

Download the `linux_amd64` or `linux_arm64` archive from [Releases](https://github.com/jc01rho/cpa-usage-keeper/releases/latest), then extract and run it:

```bash
mkdir -p cpa-usage-keeper
tar -xzf ./cpa-usage-keeper_*_linux_*.tar.gz -C cpa-usage-keeper --strip-components=1
cd cpa-usage-keeper
cp .env.example .env
vim .env
./cpa-usage-keeper
```

#### systemd

The Linux package includes a service template. Run these commands from the extracted package directory:

```bash
sudo cp cpa-usage-keeper.service /etc/systemd/system/cpa-usage-keeper.service
sudo sed -i "s|__CPA_USAGE_KEEPER_DIR__|$(pwd)|g" /etc/systemd/system/cpa-usage-keeper.service
sudo systemctl daemon-reload
sudo systemctl enable --now cpa-usage-keeper
```

```bash
sudo systemctl status cpa-usage-keeper
sudo journalctl -u cpa-usage-keeper -f
sudo systemctl restart cpa-usage-keeper
```

### Command-Line Options

The binary supports optional startup flags:

```bash
cpa-usage-keeper --host 127.0.0.1 # Override APP_HOST for this process.
cpa-usage-keeper -v               # Print the build version and exit; --version is also supported.
```

### Windows Binary

Download the `windows_amd64` or `windows_arm64` ZIP package from [Releases](https://github.com/jc01rho/cpa-usage-keeper/releases/latest) and extract it. In PowerShell, open the extracted package directory and run:

```powershell
Copy-Item .env.example .env
notepad .env
.\cpa-usage-keeper.exe
```

Set `CPA_BASE_URL`, `CPA_MANAGEMENT_KEY`, and a private `LOGIN_PASSWORD` before starting.

## Configuration

Edit your existing `.env` or Compose `environment`, depending on the deployment method; do not overwrite existing settings with a fresh template.

For first-time deployments, start with "Minimum required" and "Web access and reverse proxy". Most other settings can keep their defaults.

### Minimum Required

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `CPA_BASE_URL` | Yes | - | URL used by the Keeper server to call CPA. In Docker Compose this is usually `http://cli-proxy-api:8317`, and it can be a private address or container service name |
| `CPA_MANAGEMENT_KEY` | Yes | - | CPA management key used to read CPA management APIs |

### Web Access And Reverse Proxy

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `APP_HOST` | No | all interfaces | Keeper HTTP listen host; native deployments can set `127.0.0.1` for local-only access |
| `APP_PORT` | No | `8080` | Keeper HTTP listen port |
| `APP_BASE_PATH` | No | root path | Keeper subpath prefix, such as `/keeper`; empty means `/` |
| `CPA_PUBLIC_URL` | No | current browser origin root | Public CPA URL for the "Back to CPA" link and CPAMC frame trust |
| `TRUSTED_PROXY_CIDRS` | No | local loopback only | Additional reverse-proxy CIDRs allowed to provide `X-Forwarded-For`, separated by commas |

For Docker/Compose, keep `APP_HOST` empty; publish `127.0.0.1:8080:8080` for host-only access. Set a browser-accessible `CPA_PUBLIC_URL` when CPA and Keeper use different domains, ports, or paths. See [advanced configuration](./docs/configuration.md#web-access-and-reverse-proxy) for details and the reverse-proxy example below.

### Login Protection

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `AUTH_ENABLED` | No | `true` | Enable login protection |
| `LOGIN_PASSWORD` | When auth is enabled | - | Login password |
| `AUTH_SESSION_TTL` | No | `168h` | Login session lifetime |

### Timezone And Request Behavior

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `TZ` | No | `Asia/Shanghai` | Timezone used for statistics and display; Today, daily totals, page timestamps, log timestamps, and daily cleanup are calculated in this timezone |
| `REQUEST_TIMEOUT` | No | `30s` | Timeout for CPA HTTP requests and Redis queue operations |
| `TLS_SKIP_VERIFY` | No | `false` | Skip TLS certificate verification for CPA HTTPS and Redis queue TLS; enable only with self-signed certificates |

### Storage, Logs, And Backups

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `WORK_DIR` | No | `./data` | Application work directory; database, logs, and backups default to `app.db`, `logs/`, and `backups/` under it |
| `LOG_LEVEL` | No | `info` | Log level |
| `LOG_FILE_ENABLED` | No | `true` | Write persistent log files |
| `LOG_RETENTION_DAYS` | No | `7` | Combined-log history days, plus the current day; `0` disables cleanup. Error-only logs keep 30 history days plus the current day |
| `BACKUP_ENABLED` | No | `true` | Enable scheduled database backups |
| `BACKUP_INTERVAL` | No | `24h` | Database backup interval |
| `BACKUP_RETENTION_DAYS` | No | `7` | Backup retention days |
| `BACKUP_COMPRESS` | No | `true` | gzip completed backups (`.db.gz`), roughly one fifth the size |
| `USAGE_EVENTS_HOT_RETENTION_DAYS` | No | `90` | Local calendar days of raw `usage_events` kept in the hot table before archiving. Must be positive |
| `USAGE_RAW_RETENTION_DAYS` | No | `0` | Total raw-request retention in days; only archives are deleted. `0` retains archives forever. Values below `USAGE_EVENTS_HOT_RETENTION_DAYS` fall back to `0` with a warning without preventing startup |
| `ERROR_EVENTS_RETENTION_DAYS` | No | `30` | Local calendar days of credential `error_events` to keep; `0` keeps every row |
| `USAGE_OVERVIEW_HOURLY_RETENTION_DAYS` | No | `90` | Local calendar days of hourly Overview rollup rows to keep; `0` keeps every row. Daily rollups still serve long ranges |

Keeper archives raw request records older than `USAGE_EVENTS_HOT_RETENTION_DAYS` (default 90) local calendar days daily at 04:30 in the configured timezone, and retains archives permanently by default. Setting `USAGE_RAW_RETENTION_DAYS` to at least the hot window makes daily maintenance delete expired archived records in batches after archiving, using the request timestamp and local calendar days in the configured timezone. `ERROR_EVENTS_RETENTION_DAYS` and `USAGE_OVERVIEW_HOURLY_RETENTION_DAYS` bound the credential error history and the hourly rollup independently. Normal pages do not directly query archived records; historical aggregates and backups keep their own retention policies. Deleted raw records cannot support later historical recalculation, and hourly rollups older than their retention are only needed for the partial start/end day of a custom range longer than that retention (full days are served by the daily rollup). Deletion may not immediately shrink the SQLite file: freed space can be reused and is reclaimed by the conditional compaction policy, which now runs once the freelist holds at least 256 MiB and 10% of the database. Archived rows are stored in the `usage_events_archive` cold table, which is also used to rebuild incremental data for future schema migrations. See [storage and log details](./docs/configuration.md#storage-and-log-details).

When file logging is enabled, `cpa-usage-keeper-YYYY-MM-DD.log` contains all emitted levels. Error, fatal, and panic entries are also copied to `cpa-usage-keeper-error-YYYY-MM-DD.log`, which keeps the previous 30 local calendar dates plus the current date.

### Rolling Back A Failed v1-to-v2 Upgrade

Keeper backs up the database before migration. If it fails, follow the [failed migration rollback guide](./docs/upgrading.md). **Do not open a partially migrated database with v1.**

### Optional Features And Advanced Configuration

- [OIDC administrator login](./docs/oidc.md): disabled by default; keep password login configured. **Without subject restrictions, all users authenticated for the client become administrators; restrict application access at the provider.**
- [Quota refresh](./docs/configuration.md#credentials-quota-refresh): configure schedules from the gear button in the Credentials inspection dialog; the page need not stay open after saving. The guide covers concurrency and debugging settings.
- [Redis queue](./docs/configuration.md#redis-queue-advanced-settings): separate addresses, TLS, and polling settings; defaults usually suffice.
- [Built-in HTTPS](./docs/configuration.md#built-in-https): for Keeper to serve HTTPS directly; usually, use nginx, Caddy, or another reverse proxy instead.

### Security Notes

- Browser APIs redact key-like fields, but the SQLite database and its unencrypted backups contain original data.
- Authentication is enabled by default. If it is explicitly disabled, restrict Keeper access at the deployment boundary; terminate public HTTPS at a reverse proxy.

## Nginx Reverse Proxy

When serving under `/cpa`, set `APP_BASE_PATH=/cpa` and keep the prefix in your reverse proxy:

```nginx
location /cpa/ {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}
```

Local Nginx needs no extra proxy trust settings. For containers, remote proxies, or CDNs, configure only the actual proxy networks in `TRUSTED_PROXY_CIDRS`; never trust all sources.

## License

This project is open source under the [MIT License](./LICENSE).

## Star History

<a href="https://star-history.dera.page/Willxup/cpa-usage-keeper">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://star-history.dera.page/svg?repos=Willxup/cpa-usage-keeper&theme=dark" />
   <source media="(prefers-color-scheme: light)" srcset="https://star-history.dera.page/svg?repos=Willxup/cpa-usage-keeper" />
   <img alt="Star History Chart" src="https://star-history.dera.page/svg?repos=Willxup/cpa-usage-keeper" />
 </picture>
</a>

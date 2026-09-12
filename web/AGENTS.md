# CPA USAGE KEEPER WEB

> Parent: [../AGENTS.md](../AGENTS.md)

## OVERVIEW

`web/` is the React/Vite dashboard for usage, quota health, credentials, pricing, and API-key viewer mode. Unlike Management Center, it builds a normal SPA, not a single embedded HTML artifact.

## STRUCTURE

```text
web/
├── src/App.tsx                  # auth/session routing and CPAMC embed readiness
├── src/lib/api.ts               # only API client layer; /api/v1 path and embed session header
├── src/lib/types.ts             # backend response contracts
├── src/pages/                   # Usage, Login, KeyOverview pages
├── src/components/usage/        # dashboard cards, credentials, analysis, quota panels
├── src/stores/                  # Zustand usage stats cache
├── src/embed/                   # CPAMC embed detection/session bridge
└── src/test/, **/*.test.*       # Vitest logic/style/API tests
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Backend API calls | `src/lib/api.ts` | `apiPath()` prefixes `window.__APP_BASE_PATH__ + /api/v1`. |
| Response contracts | `src/lib/types.ts` | Keep aligned with Go handlers. |
| Usage dashboard | `src/pages/UsagePage.tsx`, `src/components/usage/` | Cards, realtime, analysis, credentials. |
| API-key viewer flow | `src/pages/KeyOverviewPage.tsx`, `App.tsx` | Role `api_key_viewer` routes to `/key-overview`. |
| CPAMC embed | `src/embed/`, `src/lib/api.ts` | Session token stored in `sessionStorage`, sent as `X-CPA-Usage-Keeper-Embed-Session`. |

## CONVENTIONS

- Use native `fetch` helpers in `src/lib/api.ts`; do not scatter raw API paths in components.
- Mutating API calls include embed session handling through shared helpers.
- Keep API paths base-path aware with `appPath()`/`apiPath()`.
- Co-locate small logic/style tests beside affected pages/components.
- Web uses npm scripts from `web/package.json`, while Management Center uses Bun.

## ANTI-PATTERNS

- Do not hardcode `/api/v1` in pages/components; use `apiPath()`.
- Do not store embed session outside `sessionStorage`.
- Do not expose CPA management keys or API keys in UI logs/errors.
- Do not copy Management Center single-file/VERSION build rules here.

## COMMANDS

Do not run these unless the user explicitly asks for cpa-usage-keeper tests.

```bash
npm --prefix ./web run test
npm --prefix ./web run lint
npm --prefix ./web run typecheck
npm --prefix ./web run build
```

## Push / PR 규칙

- forked upstream에는 절대 Pull Request를 생성하지 않는다. 모든 push는 `origin`(jc01rho) 브랜치에만 수행한다. upstream remote는 fetch/merge 전용이다.

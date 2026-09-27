# cpa-usage-keeper ↔ AI Token Monitor Bridge

Submits keeper usage straight to the
[AI Token Monitor](https://github.com/soulduse/ai-token-monitor) leaderboard
without installing the desktop app. The app scans local session logs and posts
one row per day and provider to its Supabase project; this bridge posts the same
row shape from the keeper SQLite database, so a headless gateway host (152) can
feed the leaderboard with keeper-priced usage.

## Files

- `scripts/atm_bridge.py` — bridge and login CLI (Python 3.9+, stdlib only)
- `scripts/systemd/cpa-keeper-atm-bridge.{service,timer}` — automation units
- `docs/atm-bridge.md` — this document

## How It Works

1. Opens the keeper SQLite DB **read-only** (`app.db`).
2. Collects the trailing 60 days the same way as the tokscale bridge
   (`docs/tokscale-bridge.md`), then aggregates per local calendar date:
   - `total_tokens` = `max(input − cacheRead − cacheWrite, 0) + output + cacheRead + cacheWrite`
   - `cost_usd` = keeper pricing (`internal/pricing` + rule multipliers), identical to the tokscale bridge
   - `messages` = `usage_events` rows with a non-zero token count
   - `sessions` = distinct non-empty `session_id` among those rows
3. POSTs `{p_provider, p_device_id, p_rows, p_stale_dates}` to the app's
   `sync_device_snapshots` RPC (`https://giunmtxxvapcgrpxjopq.supabase.co/rest/v1/rpc/sync_device_snapshots`)
   with the public anon key plus the logged-in user's access token.
4. Dates in the window with no keeper rows are sent as `p_stale_dates`, which
   prunes this device's rows for those dates exactly like the app does.

## Authentication (one GitHub login)

The RPC is `security definer` and requires `auth.uid()`, so a session is
mandatory. The login uses the app's own PKCE flow:

```bash
python3 scripts/atm_bridge.py --login-url
#   prints https://giunmtxxvapcgrpxjopq.supabase.co/auth/v1/authorize?provider=github&…
#   open it, authorize with GitHub; the browser ends on a URL containing ?code=…
python3 scripts/atm_bridge.py --login-code '<that final URL or just the code>'
```

`--login-code` exchanges the code, stores `access_token`/`refresh_token` in the
session file (default `~/.config/cpa-keeper-atm/session.json`, mode 0600,
override with `--session` or `$ATM_BRIDGE_SESSION`) and upserts the public
`profiles` row (`nickname` from the GitHub user name, `avatar_url`). Later runs
refresh the token in place, so no interaction is needed again.

## Provider

The desktop app submits per provider (`claude`, `codex`, `opencode`, `kimi`,
`glm`, `gjc`, `grok`, `kiro`, `omo`). The live server accepted both `omo` and
`gjc` when probed on 2026-09-27 — the public migration list lags production and
still omits `omo` — so this bridge submits as **`omo`** (default), matching the
app's own OmO provider and the tokscale bridge's "Senpi (OmO Native)" identity.
Use exactly one provider per dataset; submitting both would double-count the
same days. Re-probe with:

```bash
python3 scripts/atm_bridge.py --check-provider omo --check-provider gjc
```

The probe sends empty rows, so it writes nothing.

## Usage

```bash
# preview the payload for the trailing 60 days
python3 scripts/atm_bridge.py --db /path/to/app.db --dry-run

# submit
python3 scripts/atm_bridge.py --db /path/to/app.db

# options: --days N, --provider ID, --device-id ID, --instance UUID
```

`--device-id` defaults to `cpa-keeper-<hostname>`; the leaderboard merges this
device's rows with any other device rows the same account has submitted, so keep
one device id per host and do not run the desktop app against the same logs.

## Automation

```bash
mkdir -p ~/.config/systemd/user/
cp scripts/systemd/cpa-keeper-atm-bridge.{service,timer} ~/.config/systemd/user/
# edit the service ExecStart to point at your keeper DB
systemctl --user daemon-reload
systemctl --user enable --now cpa-keeper-atm-bridge.timer
```

The timer runs every 6 hours, matching the tokscale bridge cadence. Each run
recomputes the full 60-day window, so late-arriving keeper rows are re-submitted
and the upsert keeps a single row per (user, provider, date).

## Verify

```bash
journalctl --user -u cpa-keeper-atm-bridge.service -n 40
# leaderboard rows are world-readable (RLS select using(true)):
curl -s "$SUPABASE_URL/rest/v1/daily_snapshots?select=date,total_tokens,cost_usd,messages,sessions&provider=eq.omo&order=date.desc&limit=5" \
  -H "apikey: $ANON_KEY"
```

## Known Limitations

- Models without a keeper `model_price_settings` row contribute tokens with
  `cost_usd = 0` (the app would estimate them from its bundled pricing table);
  the run summary lists every unmatched model.
- The RPC, provider allowlist and row schema belong to the AI Token Monitor
  project and can change without notice — a rejected submit only shows up in the
  service log.
- Submissions publish aggregated daily tokens, cost, message and session counts
  under the logged-in GitHub identity on a public leaderboard.

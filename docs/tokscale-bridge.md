# cpa-usage-keeper ↔ tokscale Bridge

Bridges keeper usage data into [tokscale](https://github.com/junhoyeo/tokscale)
via gjc-format JSONL files, following the pattern of tokscale's own 9Router
bridge (`docs/9router-bridge.md` upstream). Enables tokscale analytics,
graphs, and leaderboard submission for usage that flowed through
CLIProxyAPIPlus and landed in the keeper SQLite database.

## Files

- `scripts/tokscale_bridge.py` — bridge script (Python 3.9+, stdlib only)
- `scripts/systemd/cpa-keeper-tokscale-bridge.{service,timer}` — automation units
- `docs/tokscale-bridge.md` — this document

## How It Works

1. Opens the keeper SQLite DB **read-only** (`app.db`).
2. Reads `usage_events` joined with `model_price_settings` / `model_price_rules`.
3. Computes cost per event with the **same formula as the keeper**
   (`internal/pricing` + `internal/helper/usage_cost.go`):
   - model matched by exact `model`, then `model_alias`
   - `uncached_input = max(input_tokens − cache_read − cache_creation, 0)`
   - `(uncached_input×prompt + cache_read×cache_read + cache_creation×cache_write + output×completion) / 1e6`
   - × `price_multiplier` × matching `model_price_rules` multipliers
4. Writes gjc-format JSONL grouped by local date to
   `~/.local/share/cpa-keeper-tokscale/sessions/<date>.jsonl`.
5. Tokscale's gjc parser reads the files via `extraScanPaths` (below). Bridge
   messages are stamped with source client `cpa-keeper` (override: `--client`).

## Cost Field Policy (keeper-authoritative)

Unlike the 9Router bridge (which omits cost and lets tokscale reprice), this
bridge **embeds the keeper-computed cost** as `usage.cost.total` for every
event whose model has a `model_price_settings` row. The gjc parser treats an
embedded cost as `CostSource::ProviderReported` — authoritative — so the
leaderboard and graphs show exactly what the keeper's own dashboard bills,
including custom prices, zero-priced (free) configs, and rule multipliers.

Models **without** a keeper price row get no embedded cost; tokscale then
estimates from tokens via LiteLLM pricing. The run summary lists every
unmatched model so you can add its price in the keeper UI instead.

## Token Mapping

| gjc field | keeper column | note |
|---|---|---|
| `input` | `max(input_tokens − cache_read − cache_creation, 0)` | CPA canonical input includes cache buckets |
| `output` | `output_tokens` | already includes reasoning (CPA keeps reasoning as a subset of output; gjc has no reasoning bucket) |
| `cacheRead` | `cache_read_tokens` | |
| `cacheWrite` | `cache_creation_tokens` | |

Events with every token field zero are skipped (keeper marks legacy
WebSocket warmups this way too, via `generate=false`).

## Setup

### 1. One-time backfill

```bash
python3 scripts/tokscale_bridge.py --db /path/to/keeper/app.db --full
```

`--db` also resolves from `$KEEPER_DB`, `./data/app.db`, or
`~/cpa-usage-keeper/data/app.db`. Use `--instance <uuid>` to restrict to one
CPA instance, `--dry-run` to preview.

### 2. Configure the tokscale scanner

Add the bridge output directory to `~/.config/tokscale/settings.json`
(override with `TOKSCALE_CONFIG_DIR`):

```json
{
  "scanner": {
    "extraScanPaths": {
      "gjc": ["/home/USER/.local/share/cpa-keeper-tokscale/sessions"]
    }
  }
}
```

### 3. Verify

```bash
tokscale graph --client gjc
tokscale models --client gjc
```

### 4. Submit

```bash
tokscale submit
```

Bridge data flows through the gjc client (`submit_default: true`), so it is
included in submissions automatically.

> **Double-counting warning:** if tokscale also scans native local sessions
> (Claude Code, OpenCode, …) on the same machine, requests sent through the
> proxy are counted twice — once natively, once through this bridge. Submit
> only the clients you want (`tokscale submit` client selection) or keep the
> native clients out of the scan scope.

### 5. Automation

```bash
mkdir -p ~/.config/systemd/user/
cp scripts/systemd/cpa-keeper-tokscale-bridge.{service,timer} ~/.config/systemd/user/
# edit the service ExecStart to point at your keeper DB
systemctl --user daemon-reload
systemctl --user enable --now cpa-keeper-tokscale-bridge.timer
```

The timer runs every 10 minutes. Routine runs refresh only dates that own a
row **created** within the last 7 days (`--since-days`), which also catches
late-arriving keeper-export push batches for older dates; each refreshed
date file is rewritten from **all** of that date's rows, so a partial
refresh can never drop earlier rows of a rewritten date. Date files with no
matching rows on a run are left untouched.

## Known Limitations

- Keeper `reasoning_tokens` are not exported separately (gjc has no
  reasoning bucket); they are already inside `output_tokens`, so totals and
  cost are unaffected.
- The keeper's ad-hoc API-key exclusions (`ExcludedAPIGroupKeys` in queries)
  are not replicated; filter with `--instance` or prune date files manually.
- Timestamps are bucketed by the bridge host's local timezone to match
  tokscale's `chrono::Local` grouping; run the bridge on the same host as
  `tokscale submit`.
- Failed requests with tokens are included (the keeper's own cost overview
  includes them); zero-token events are always skipped.

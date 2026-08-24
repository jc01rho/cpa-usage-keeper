#!/usr/bin/env python3
"""Bridge cpa-usage-keeper usage into tokscale via gjc-format JSONL files.

Reads usage events (plus model price settings/rules) from the keeper SQLite
database and writes date-grouped JSONL files that tokscale's gjc client
parser can consume, following the pattern of tokscale's own 9Router bridge
(scripts/9router_tokscale_bridge_gjc.py, docs/9router-bridge.md).

Cost policy (differs from the 9Router bridge): every event whose model (or
model alias) has a keeper `model_price_settings` row embeds the
keeper-computed cost as an authoritative `usage.cost.total`
(CostSource::ProviderReported in tokscale), so leaderboard numbers reflect
the prices configured in the keeper instead of tokscale's LiteLLM estimates.
Models without a keeper price row omit the cost field, letting tokscale
estimate from tokens; they are listed in the run summary so missing prices
can be added in the keeper UI.

Token mapping (keeper usage_events -> gjc usage):
  input     = max(input_tokens - cache_read_tokens - cache_creation_tokens, 0)
              (CPA canonical input_tokens includes both cache buckets)
  output    = output_tokens (already includes reasoning tokens; CPA keeps
              reasoning as a subset of output, and the gjc format has no
              separate reasoning bucket)
  cacheRead = cache_read_tokens
  cacheWrite= cache_creation_tokens

Setup:
    python3 scripts/tokscale_bridge.py --db /path/to/app.db --full

Then add to ~/.config/tokscale/settings.json:
    {"scanner": {"extraScanPaths": {"gjc": ["~/.local/share/cpa-keeper-tokscale/sessions"]}}}

See docs/tokscale-bridge.md for the full documentation.
"""

from __future__ import annotations

import argparse
import json
import math
import os
import re
import sqlite3
import sys
import tempfile
from datetime import datetime, timedelta, timezone
from pathlib import Path
from urllib.parse import quote

DEFAULT_CLIENT_STAMP = "senpi"
DEFAULT_DEST = Path.home() / ".local" / "share" / "cpa-keeper-tokscale" / "sessions"
DEFAULT_SINCE_DAYS = 7
ID_CHUNK = 500

RULE_DIMENSION_COLUMNS = {
    "api_group_key": "api_group_key",
    "model": "model",
    "auth_index": "auth_index",
    "model_alias": "model_alias",
    "service_tier": "service_tier",
    "response_service_tier": "response_service_tier",
    "reasoning_effort": "reasoning_effort",
    "endpoint": "endpoint",
    "executor_type": "executor_type",
}

EVENT_COLUMNS = [
    "id", "event_key", "api_group_key", "provider", "endpoint", "model",
    "model_alias", "service_tier", "response_service_tier", "reasoning_effort",
    "executor_type", "auth_index", "failed", "timestamp", "input_tokens",
    "output_tokens", "reasoning_tokens", "cache_read_tokens",
    "cache_creation_tokens",
]

# keeper stores timestamps as Go RFC3339Nano in the writer's local offset,
# but legacy rows (and migration fixtures) also carry a space separator,
# 9-digit nanoseconds, a bare "Z", or no offset at all. Python 3.9's
# fromisoformat rejects 7-9 fractional digits, so parse by hand.
TIMESTAMP_RE = re.compile(
    r"^(?P<date>\d{4}-\d{2}-\d{2})[T ](?P<time>\d{2}:\d{2}:\d{2})"
    r"(?:\.(?P<frac>\d+))?(?P<tz>Z|[+-]\d{2}:?\d{2})?$"
)


def parse_storage_timestamp(text):
    """Parse a keeper storage timestamp into an aware datetime.

    Returns None for NULL/empty/unparseable values. Offsetless values are
    interpreted in the bridge host's local timezone (FormatStorageTime always
    writes an offset, so an offsetless value can only come from legacy data).
    """
    if not text:
        return None
    match = TIMESTAMP_RE.match(str(text).strip())
    if not match:
        return None
    frac = (match.group("frac") or "")[:6].ljust(6, "0")
    tz_text = match.group("tz")
    iso = "{date}T{time}.{frac}".format(date=match.group("date"), time=match.group("time"), frac=frac)
    if tz_text in (None, ""):
        try:
            return datetime.fromisoformat(iso).astimezone()
        except ValueError:
            return None
    if tz_text == "Z":
        iso += "+00:00"
    else:
        sign = tz_text[0]
        digits = tz_text[1:].replace(":", "")
        iso += "{sign}{h}:{m}".format(sign=sign, h=digits[:2], m=digits[2:4])
    try:
        return datetime.fromisoformat(iso)
    except ValueError:
        return None


def to_unix_ms(moment):
    return int(moment.timestamp() * 1000)


def local_date_string(moment):
    return moment.astimezone().strftime("%Y-%m-%d")


def safe_int(value):
    try:
        number = int(value or 0)
    except (TypeError, ValueError):
        return 0
    return max(number, 0)


def open_readonly_db(db_path):
    if not db_path.exists():
        return None
    conn = sqlite3.connect("file:{}?mode=ro".format(quote(str(db_path))), uri=True)
    conn.row_factory = sqlite3.Row
    return conn


def table_columns(conn, table):
    return {row[1] for row in conn.execute("PRAGMA table_info({})".format(table))}


def resolve_db_path(explicit):
    candidates = []
    if explicit:
        candidates.append(Path(explicit))
    env_path = os.environ.get("KEEPER_DB")
    if env_path:
        candidates.append(Path(env_path))
    candidates.append(Path("data") / "app.db")
    candidates.append(Path.home() / "cpa-usage-keeper" / "data" / "app.db")
    for candidate in candidates:
        if candidate.exists():
            return candidate
    return None


class PriceCatalog(object):
    """Mirror of keeper internal/pricing: model exact match, then alias match,
    model-level multiplier, then per-field rule multipliers."""

    def __init__(self, conn):
        self.settings_by_model = {}
        self.rules_by_setting = {}
        self._load(conn)

    def _load(self, conn):
        columns = table_columns(conn, "model_price_settings")
        if not columns:
            return
        cache_read_col = (
            "cache_read_price_per1_m"
            if "cache_read_price_per1_m" in columns
            else "cache_price_per1_m"
        )
        select = ["id", "model", "prompt_price_per1_m", "completion_price_per1_m", cache_read_col]
        if "cache_creation_price_per1_m" in columns:
            select.append("cache_creation_price_per1_m")
        if "price_multiplier" in columns:
            select.append("price_multiplier")
        for row in conn.execute(
            "SELECT {} FROM model_price_settings".format(", ".join(select))
        ):
            setting = {
                "id": row["id"],
                "model": (row["model"] or "").strip(),
                "prompt": float(row["prompt_price_per1_m"] or 0.0),
                "completion": float(row["completion_price_per1_m"] or 0.0),
                "cache_read": float(row[cache_read_col] or 0.0),
                "cache_write": float(row["cache_creation_price_per1_m"] or 0.0)
                if "cache_creation_price_per1_m" in columns
                else 0.0,
                "multiplier": float(row["price_multiplier"])
                if "price_multiplier" in columns and row["price_multiplier"] is not None
                else 1.0,
            }
            if setting["model"]:
                self.settings_by_model[setting["model"]] = setting
        if table_columns(conn, "model_price_rules"):
            for row in conn.execute(
                "SELECT model_price_setting_id, key, value, multiplier FROM model_price_rules"
            ):
                setting_id = row["model_price_setting_id"]
                key = (row["key"] or "").strip().lower()
                if key not in RULE_DIMENSION_COLUMNS:
                    continue
                self.rules_by_setting.setdefault(setting_id, []).append(
                    ((key, (row["value"] or "").strip()), float(row["multiplier"] or 0.0))
                )

    def resolve(self, model, model_alias):
        if model and model in self.settings_by_model:
            return self.settings_by_model[model]
        if model_alias and model_alias in self.settings_by_model:
            return self.settings_by_model[model_alias]
        return None

    def cost_usd(self, setting, dimensions, input_tokens, output_tokens, cache_read, cache_write):
        """Replicates helper.CalculateUsageTokenCostBreakdown + Resolver rule
        multipliers: (segments * model multiplier) * rule multiplier."""
        model_multiplier = setting["multiplier"]
        if model_multiplier == 0:
            return 0.0
        normal_input = max(input_tokens - cache_read - cache_write, 0)
        base = (
            normal_input / 1_000_000.0 * setting["prompt"]
            + cache_read / 1_000_000.0 * setting["cache_read"]
            + cache_write / 1_000_000.0 * setting["cache_write"]
            + output_tokens / 1_000_000.0 * setting["completion"]
        )
        total = base * model_multiplier
        rule_multiplier = 1.0
        for (identity, multiplier) in self.rules_by_setting.get(setting["id"], []):
            key, value = identity
            if dimensions.get(key, "") != value:
                continue
            if multiplier == 0:
                return 0.0
            rule_multiplier *= multiplier
        return total * rule_multiplier


def build_entry(row, catalog, client_stamp, stats):
    """Convert one usage_events row into a gjc message entry, or None."""
    model = (row["model"] or "").strip() or "unknown"
    model_alias = (row["model_alias"] or "").strip()
    input_tokens = safe_int(row["input_tokens"])
    output_tokens = safe_int(row["output_tokens"])
    cache_read = safe_int(row["cache_read_tokens"])
    cache_write = safe_int(row["cache_creation_tokens"])
    reasoning = safe_int(row["reasoning_tokens"])

    if input_tokens == 0 and output_tokens == 0 and cache_read == 0 and cache_write == 0 and reasoning == 0:
        stats["zero_token_skipped"] += 1
        return None

    moment = parse_storage_timestamp(row["timestamp"])
    if moment is None:
        stats["bad_timestamp_skipped"] += 1
        return None

    uncached_input = max(input_tokens - cache_read - cache_write, 0)
    provider = (row["provider"] or "").strip()

    dimensions = {}
    for key, column in RULE_DIMENSION_COLUMNS.items():
        dimensions[key] = (row[column] or "").strip() if column in row.keys() else ""

    usage = {
        "input": uncached_input,
        "output": output_tokens,
        "cacheRead": cache_read,
        "cacheWrite": cache_write,
        "totalTokens": uncached_input + output_tokens + cache_read + cache_write,
    }

    setting = catalog.resolve(model, model_alias)
    if setting is not None:
        cost = catalog.cost_usd(
            setting, dimensions, input_tokens, output_tokens, cache_read, cache_write
        )
        if math.isfinite(cost) and cost >= 0:
            usage["cost"] = {"total": cost}
        else:
            stats["nonfinite_cost_skipped"] += 1
    else:
        stats["unmatched"][model] = stats["unmatched"].get(model, 0) + 1

    message = {
        "role": "assistant",
        "model": model,
        "source": client_stamp,
        "timestamp": to_unix_ms(moment),
        "usage": usage,
    }
    if provider:
        message["provider"] = provider
        message["api"] = provider

    return {
        "date_str": local_date_string(moment),
        "entry": {"type": "message", "id": "ck-{}".format(row["id"]), "message": message},
    }


def collect_refresh_dates(conn, full, since_days, instance_id):
    """Scan (id, timestamp, created_at) and return {local_date: [ids]}.

    With full=True every date is returned; otherwise only dates that own at
    least one row created within the lookback window (this catches
    late-arriving keeper-export batches for old dates too). Files for dates
    outside the result are left untouched, and every returned date file is
    rewritten from ALL of that date's rows, so partial refreshes can never
    drop earlier rows of a rewritten date.
    """
    columns = table_columns(conn, "usage_events")
    if not columns:
        raise SystemExit("usage_events table not found in the keeper database")
    has_instance = "instance_id" in columns
    cutoff = datetime.now(timezone.utc) - timedelta(days=since_days)

    where = ""
    params = []
    if has_instance and instance_id:
        where = " WHERE instance_id = ?"
        params.append(instance_id)

    dates = {}
    for row in conn.execute(
        "SELECT id, timestamp, created_at FROM usage_events{} ORDER BY id".format(where), params
    ):
        moment = parse_storage_timestamp(row["timestamp"])
        if moment is None:
            continue
        date_str = local_date_string(moment)
        if not full:
            created = parse_storage_timestamp(row["created_at"])
            if created is None or created < cutoff:
                continue
        dates.setdefault(date_str, []).append(row["id"])
    return dates


def fetch_rows_for_dates(conn, dates, instance_id):
    columns = table_columns(conn, "usage_events")
    has_instance = "instance_id" in columns and instance_id
    base_sql = "SELECT {} FROM usage_events WHERE id IN ({})".format(
        ", ".join(EVENT_COLUMNS), "{}"
    )
    if has_instance:
        base_sql += " AND instance_id = ?"
    all_ids = [row_id for ids in dates.values() for row_id in ids]
    for start in range(0, len(all_ids), ID_CHUNK):
        chunk = all_ids[start:start + ID_CHUNK]
        placeholders = ",".join("?" for _ in chunk)
        params = list(chunk) + ([instance_id] if has_instance else [])
        for row in conn.execute(base_sql.format(placeholders), params):
            yield row


def atomic_write_jsonl(path, entries):
    path.parent.mkdir(parents=True, exist_ok=True)
    handle = tempfile.NamedTemporaryFile(
        "w", dir=str(path.parent), prefix=".{}.".format(path.name), delete=False
    )
    try:
        with handle:
            for entry in entries:
                handle.write(json.dumps(entry, separators=(",", ":"), ensure_ascii=False))
                handle.write("\n")
        os.replace(handle.name, str(path))
    finally:
        if os.path.exists(handle.name):
            os.unlink(handle.name)


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--db", help="keeper SQLite path (default: $KEEPER_DB, ./data/app.db)")
    parser.add_argument("--dest", default=str(DEFAULT_DEST), help="output sessions directory")
    parser.add_argument("--client", default=DEFAULT_CLIENT_STAMP, help="gjc source client stamp")
    parser.add_argument("--instance", help="restrict to one CPA instance_id")
    parser.add_argument("--full", action="store_true", help="rewrite every date (initial backfill)")
    parser.add_argument("--since-days", type=int, default=DEFAULT_SINCE_DAYS,
                        help="refresh dates owning rows created in the last N days (default 7)")
    parser.add_argument("--dry-run", action="store_true", help="print plan, write nothing")
    args = parser.parse_args()

    db_path = resolve_db_path(args.db)
    if db_path is None:
        raise SystemExit(
            "keeper database not found; pass --db /path/to/app.db (default candidates: "
            "$KEEPER_DB, ./data/app.db, ~/cpa-usage-keeper/data/app.db)"
        )
    conn = open_readonly_db(db_path)
    if conn is None:
        raise SystemExit("cannot open {}".format(db_path))

    missing = [column for column in EVENT_COLUMNS if column not in table_columns(conn, "usage_events")]
    if missing:
        raise SystemExit("usage_events is missing expected columns: {}".format(", ".join(missing)))

    catalog = PriceCatalog(conn)
    stats = {
        "zero_token_skipped": 0,
        "bad_timestamp_skipped": 0,
        "nonfinite_cost_skipped": 0,
        "unmatched": {},
    }

    dates = collect_refresh_dates(conn, args.full, max(args.since_days, 0), args.instance)
    if not dates:
        print("tokscale bridge: no dates to refresh (db={}, prices={})".format(
            db_path, len(catalog.settings_by_model)))
        return 0

    rows_by_date = {date: [] for date in dates}
    for row in fetch_rows_for_dates(conn, dates, args.instance):
        built = build_entry(row, catalog, args.client, stats)
        if built is not None:
            rows_by_date[built["date_str"]].append(built["entry"])

    dest = Path(args.dest)
    written = 0
    for date in sorted(rows_by_date):
        entries = rows_by_date[date]
        if not entries:
            continue
        target = dest / "{}.jsonl".format(date)
        if args.dry_run:
            print("would write {} ({} entries)".format(target, len(entries)))
        else:
            atomic_write_jsonl(target, entries)
        written += 1

    total_entries = sum(len(entries) for entries in rows_by_date.values())
    print("tokscale bridge: {} date file(s), {} entries -> {}".format(written, total_entries, dest))
    if stats["zero_token_skipped"]:
        print("  skipped {} zero-token event(s)".format(stats["zero_token_skipped"]))
    if stats["bad_timestamp_skipped"]:
        print("  skipped {} event(s) with unparseable timestamps".format(stats["bad_timestamp_skipped"]))
    if stats["nonfinite_cost_skipped"]:
        print("  omitted {} non-finite cost value(s)".format(stats["nonfinite_cost_skipped"]))
    if stats["unmatched"]:
        print("  WARNING: no keeper price row for these models (cost omitted, tokscale will estimate):")
        for model in sorted(stats["unmatched"]):
            print("    {} ({} events)".format(model, stats["unmatched"][model]))
    return 0


if __name__ == "__main__":
    sys.exit(main())

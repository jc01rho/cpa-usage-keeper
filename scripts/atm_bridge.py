#!/usr/bin/env python3
"""Submit cpa-usage-keeper daily usage to the AI Token Monitor leaderboard.

AI Token Monitor (soulduse/ai-token-monitor) scans local session logs and posts
one row per day and provider to its Supabase project through the
`sync_device_snapshots` RPC. This bridge performs that same submission from the
keeper's SQLite usage — the source the tokscale bridge already reads — so the
leaderboard can be fed from a headless host without installing the desktop app.

Auth
----
`sync_device_snapshots` is `security definer` and requires `auth.uid()`, so one
GitHub login is needed. Three commands cover the whole lifecycle:

    python3 scripts/atm_bridge.py --login-url
        # prints the Supabase/GitHub authorize URL and records the PKCE
        # verifier in the session file
    python3 scripts/atm_bridge.py --login-code '<final URL or code>'
        # exchanges the code, stores access/refresh tokens and upserts the
        # leaderboard profile row
    python3 scripts/atm_bridge.py              # every run afterwards

The refresh token lives in the session file (0600) and is renewed in place, so
the timer runs unattended.

Payload shape (mirrors the app's per-provider daily snapshot)
-------------------------------------------------------------
date          local (bridge host) calendar date
total_tokens  input + output + cache read + cache write, keeper token mapping
cost_usd      keeper-computed cost when the model has a price row, else 0
messages      usage_events rows with a non-zero token count
sessions      distinct non-empty session_id among those rows

Provider
--------
The server allowlist currently holds claude, codex, opencode, kimi, glm, gjc,
grok and kiro. `omo` is not registered (the desktop app still sends it and the
server answers "Invalid provider"), so keeper usage is submitted as `gjc`,
matching the tokscale bridge's gjc-derived payload. Use --check-provider to
probe a provider before relying on it.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import importlib.util
import json
import os
import secrets
import sqlite3
import sys
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timedelta, timezone
from pathlib import Path

SUPABASE_URL = "https://giunmtxxvapcgrpxjopq.supabase.co"
# Public anon key shipped in the desktop app; RLS policies carry the security.
SUPABASE_ANON_KEY = (
    "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9."
    "eyJpc3MiOiJzdXBhYmFzZSIsInJlZiI6ImdpdW5tdHh4dmFwY2dycHhqb3BxIiwicm9sZSI6ImFub24iLCJpYXQiOjE3NzQwNTY1NTgsImV4cCI6MjA4OTYzMjU1OH0."
    "Hr_xtU1FGUrlNjWS8g4KeiYQWt0vC3bd16VVlAZdldk"
)
RPC_PATH = "/rest/v1/rpc/sync_device_snapshots"
DEFAULT_PROVIDER = "gjc"
DEFAULT_DAYS = 60
REFRESH_SKEW_SECONDS = 300
ID_CHUNK = 500
ROW_COLUMNS = [
    "id",
    "model",
    "model_alias",
    "provider",
    "endpoint",
    "service_tier",
    "response_service_tier",
    "reasoning_effort",
    "executor_type",
    "api_group_key",
    "auth_index",
    "input_tokens",
    "output_tokens",
    "cache_read_tokens",
    "cache_creation_tokens",
    "session_id",
]


def default_session_path():
    override = os.environ.get("ATM_BRIDGE_SESSION")
    if override:
        return Path(override).expanduser()
    return Path.home() / ".config" / "cpa-keeper-atm" / "session.json"


def default_device_id():
    host = (os.uname().nodename or "host").strip() or "host"
    slug = "".join(ch if ch.isalnum() or ch in "-_" else "-" for ch in host.lower())
    return "cpa-keeper-{}".format(slug)


def load_bridge_module(explicit):
    """Import tokscale_bridge.py so pricing and token mapping stay identical."""
    path = Path(explicit).expanduser() if explicit else Path(__file__).with_name("tokscale_bridge.py")
    if not path.exists():
        raise SystemExit(
            "tokscale_bridge.py not found at {} (pass --bridge-script)".format(path)
        )
    spec = importlib.util.spec_from_file_location("tokscale_bridge", str(path))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def http_json(method, url, payload=None, headers=None, timeout=60):
    data = json.dumps(payload).encode("utf-8") if payload is not None else None
    request = urllib.request.Request(url, data=data, method=method)
    request.add_header("Content-Type", "application/json")
    for key, value in (headers or {}).items():
        request.add_header(key, value)
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            return response.status, response.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as error:
        return error.code, error.read().decode("utf-8", "replace")
    except urllib.error.URLError as error:
        raise SystemExit("network error talking to {}: {}".format(url, error))


def load_session(path):
    if not path.exists():
        return {}
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except ValueError as error:
        raise SystemExit("session file {} is not valid JSON: {}".format(path, error))


def save_session(path, session):
    path.parent.mkdir(parents=True, exist_ok=True)
    handle = os.open(str(path), os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(handle, "w", encoding="utf-8") as stream:
        json.dump(session, stream, indent=2, sort_keys=True)
        stream.write("\n")


def require_access_token(session_path, session):
    """Return a valid access token, refreshing it in place when needed."""
    token = session.get("access_token")
    expires_at = float(session.get("expires_at") or 0)
    if token and expires_at - REFRESH_SKEW_SECONDS > datetime.now(timezone.utc).timestamp():
        return token
    refresh_token = session.get("refresh_token")
    if not refresh_token:
        raise SystemExit(
            "no usable session in {}; run --login-url then --login-code first".format(session_path)
        )
    status, body = http_json(
        "POST",
        "{}/auth/v1/token?grant_type=refresh_token".format(SUPABASE_URL),
        {"refresh_token": refresh_token},
        {"apikey": SUPABASE_ANON_KEY},
    )
    if status != 200:
        raise SystemExit("token refresh failed ({}): {}".format(status, body.strip()))
    store_session_payload(session_path, session, json.loads(body))
    return session["access_token"]


def store_session_payload(session_path, session, payload):
    session["access_token"] = payload["access_token"]
    session["refresh_token"] = payload["refresh_token"]
    session["expires_at"] = (
        datetime.now(timezone.utc).timestamp() + float(payload.get("expires_in") or 3600)
    )
    user = payload.get("user") or {}
    if user.get("id"):
        session["user_id"] = user["id"]
        metadata = user.get("user_metadata") or {}
        session["nickname"] = (
            metadata.get("user_name")
            or metadata.get("preferred_username")
            or (user.get("email") or "").split("@")[0]
            or "Anonymous"
        )
        session["avatar_url"] = metadata.get("avatar_url")
    session.pop("pending_login", None)
    save_session(session_path, session)


def authorize_url(session_path, redirect_to):
    verifier = base64.urlsafe_b64encode(secrets.token_bytes(32)).rstrip(b"=").decode("ascii")
    challenge = base64.urlsafe_b64encode(
        hashlib.sha256(verifier.encode("ascii")).digest()
    ).rstrip(b"=").decode("ascii")
    state = secrets.token_urlsafe(16)
    session = load_session(session_path)
    session["pending_login"] = {
        "code_verifier": verifier,
        "state": state,
        "redirect_to": redirect_to,
        "created_at": datetime.now(timezone.utc).isoformat(),
    }
    save_session(session_path, session)
    query = urllib.parse.urlencode(
        {
            "provider": "github",
            "redirect_to": redirect_to,
            "code_challenge": challenge,
            "code_challenge_method": "s256",
            "state": state,
        }
    )
    return "{}/auth/v1/authorize?{}".format(SUPABASE_URL, query)


def extract_auth_code(pasted):
    text = (pasted or "").strip()
    if not text:
        raise SystemExit("empty --login-code value")
    if "code=" in text:
        query = urllib.parse.urlparse(text).query or text.split("?", 1)[-1]
        params = urllib.parse.parse_qs(query)
        if params.get("code"):
            return params["code"][0]
        raise SystemExit("no code= parameter found in the pasted URL")
    if any(ch.isspace() for ch in text) or len(text) > 512:
        raise SystemExit("pasted value is neither a URL with code= nor a bare code")
    return text


def complete_login(session_path, pasted, skip_profile=False):
    session = load_session(session_path)
    pending = session.get("pending_login")
    if not pending:
        raise SystemExit("no pending login in {}; run --login-url first".format(session_path))
    code = extract_auth_code(pasted)
    status, body = http_json(
        "POST",
        "{}/auth/v1/token?grant_type=pkce".format(SUPABASE_URL),
        {"auth_code": code, "code_verifier": pending["code_verifier"]},
        {"apikey": SUPABASE_ANON_KEY},
    )
    if status != 200:
        raise SystemExit("code exchange failed ({}): {}".format(status, body.strip()))
    store_session_payload(session_path, session, json.loads(body))
    print("logged in as {}".format(session.get("nickname") or session.get("user_id")))
    if not skip_profile:
        upsert_profile(session, session["access_token"])
    return session


def upsert_profile(session, access_token):
    user_id = session.get("user_id")
    if not user_id:
        return
    status, body = http_json(
        "POST",
        "{}/rest/v1/profiles".format(SUPABASE_URL),
        {
            "id": user_id,
            "nickname": session.get("nickname") or "Anonymous",
            "avatar_url": session.get("avatar_url"),
        },
        {
            "apikey": SUPABASE_ANON_KEY,
            "Authorization": "Bearer {}".format(access_token),
            "Prefer": "resolution=merge-duplicates,return=minimal",
        },
    )
    if status not in (200, 201, 204):
        print("warning: profile upsert failed ({}): {}".format(status, body.strip()), file=sys.stderr)


def call_sync_rpc(session_path, session, provider, device_id, rows, stale_dates):
    token = require_access_token(session_path, session)
    status, body = http_json(
        "POST",
        SUPABASE_URL + RPC_PATH,
        {
            "p_provider": provider,
            "p_device_id": device_id,
            "p_rows": rows,
            "p_stale_dates": stale_dates,
        },
        {
            "apikey": SUPABASE_ANON_KEY,
            "Authorization": "Bearer {}".format(token),
        },
    )
    return status, body.strip()


def window_dates(days):
    today = datetime.now().date()
    start = today - timedelta(days=max(days, 1) - 1)
    dates = []
    cursor = start
    while cursor <= today:
        dates.append(cursor.isoformat())
        cursor += timedelta(days=1)
    return dates


def iter_rows(bridge, conn, dates, instance_id):
    """Yield (date, row) for the window in one streaming pass.

    The keeper writes offset-bearing timestamps (verified on the 152 store:
    every row ends in +09:00), so `substr(timestamp, 1, 10)` is the same local
    calendar date the tokscale bridge derives, without re-parsing ~500k
    timestamps in Python or holding every row in memory.
    """
    wanted = set(dates)
    columns = bridge.table_columns(conn, "usage_events")
    select = [name for name in ROW_COLUMNS if name in columns]
    select.insert(0, "substr(timestamp, 1, 10) AS usage_date")
    sql = "SELECT {} FROM usage_events WHERE substr(timestamp, 1, 10) >= ?".format(
        ", ".join(select)
    )
    params = [min(dates)]
    if "instance_id" in columns and instance_id:
        sql += " AND instance_id = ?"
        params.append(instance_id)
    for row in conn.execute(sql, params):
        date = row["usage_date"]
        if date in wanted:
            yield date, row


def build_daily_rows(bridge, catalog, rows, dates, dimension_keys):
    tokens_by_date = {date: 0 for date in dates}
    cost_by_date = {date: 0.0 for date in dates}
    messages_by_date = {date: 0 for date in dates}
    sessions_by_date = {date: set() for date in dates}
    unmatched = {}
    for date, row in rows:
        input_tokens = bridge.safe_int(row["input_tokens"])
        output_tokens = bridge.safe_int(row["output_tokens"])
        cache_read = bridge.safe_int(row["cache_read_tokens"])
        cache_write = bridge.safe_int(row["cache_creation_tokens"])
        if input_tokens == 0 and output_tokens == 0 and cache_read == 0 and cache_write == 0:
            continue
        messages_by_date[date] += 1
        session_id = (row["session_id"] or "").strip() if "session_id" in row.keys() else ""
        if session_id:
            sessions_by_date[date].add(session_id)
        uncached_input = max(input_tokens - cache_read - cache_write, 0)
        tokens_by_date[date] += uncached_input + output_tokens + cache_read + cache_write
        model = (row["model"] or "").strip() or "unknown"
        model_alias = (row["model_alias"] or "").strip()
        setting = catalog.resolve(model, model_alias)
        if setting is None:
            unmatched[model] = unmatched.get(model, 0) + 1
            continue
        dimensions = {}
        for key, column in dimension_keys.items():
            dimensions[key] = (row[column] or "").strip() if column in row.keys() else ""
        cost_by_date[date] += catalog.cost_usd(
            setting, dimensions, input_tokens, output_tokens, cache_read, cache_write
        )
    payload = {}
    for date in dates:
        if messages_by_date[date]:
            payload[date] = {
                "date": date,
                "total_tokens": tokens_by_date[date],
                "cost_usd": round(cost_by_date[date], 4),
                "messages": messages_by_date[date],
                "sessions": len(sessions_by_date[date]),
            }
    return payload, unmatched


def resolve_db_path(bridge, explicit):
    path = bridge.resolve_db_path(explicit)
    if path is None:
        raise SystemExit(
            "keeper database not found; pass --db /path/to/app.db (or set $KEEPER_DB)"
        )
    return path


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--db", help="keeper SQLite path (default: $KEEPER_DB, ./data/app.db, ~/cpa-usage-keeper/data/app.db)")
    parser.add_argument("--bridge-script", help="path to tokscale_bridge.py (default: next to this script)")
    parser.add_argument("--session", default=str(default_session_path()), help="session file holding the ATM tokens")
    parser.add_argument("--provider", default=DEFAULT_PROVIDER, help="leaderboard provider id (default: gjc)")
    parser.add_argument("--device-id", default=default_device_id(), help="stable device id for this host")
    parser.add_argument("--days", type=int, default=DEFAULT_DAYS, help="trailing days to submit (default: 60)")
    parser.add_argument("--instance", help="restrict to one CPA instance_id")
    parser.add_argument("--dry-run", action="store_true", help="print the payload without submitting")
    parser.add_argument("--check-provider", action="append", default=[], help="probe a provider id with an empty payload (repeatable)")
    parser.add_argument("--login-url", action="store_true", help="start the GitHub login and print the authorize URL")
    parser.add_argument("--login-code", help="finish the GitHub login with the pasted redirect URL or code")
    parser.add_argument("--redirect-to", default="aitokenmonitor://auth-callback", help="OAuth redirect target used by --login-url")
    parser.add_argument("--skip-profile", action="store_true", help="do not upsert the leaderboard profile row on login")
    args = parser.parse_args()

    session_path = Path(args.session).expanduser()

    if args.login_url:
        print(authorize_url(session_path, args.redirect_to))
        print("open the URL, authorize with GitHub, then run:")
        print("  {} --login-code '<final redirect URL>'".format(Path(__file__).name))
        return 0

    if args.login_code:
        complete_login(session_path, args.login_code, args.skip_profile)
        return 0

    bridge = load_bridge_module(args.bridge_script)
    session = load_session(session_path)

    if args.check_provider:
        failures = 0
        for provider in args.check_provider:
            status, body = call_sync_rpc(session_path, session, provider, args.device_id, [], [])
            if status < 300:
                print("provider {}: accepted".format(provider))
            else:
                failures += 1
                print("provider {}: rejected ({}): {}".format(provider, status, body), file=sys.stderr)
        return 1 if failures else 0

    db_path = resolve_db_path(bridge, args.db)
    conn = bridge.open_readonly_db(db_path)
    if conn is None:
        raise SystemExit("cannot open {} read-only".format(db_path))
    dates = window_dates(args.days)
    rows = iter_rows(bridge, conn, dates, args.instance)
    catalog = bridge.PriceCatalog(conn)
    payload, unmatched = build_daily_rows(
        bridge, catalog, rows, dates, bridge.RULE_DIMENSION_COLUMNS
    )
    rows = [payload[date] for date in dates if payload.get(date, {}).get("messages")]
    empty_dates = [date for date in dates if not payload.get(date, {}).get("messages")]

    total_tokens = sum(row["total_tokens"] for row in rows)
    total_cost = sum(row["cost_usd"] for row in rows)
    messages = sum(row["messages"] for row in rows)
    sessions = sum(row["sessions"] for row in rows)
    print(
        "atm bridge: {} row(s) over {} day(s) -> {} tokens, ${:.4f}, {} messages, {} sessions".format(
            len(rows), len(dates), total_tokens, total_cost, messages, sessions
        )
    )
    if rows:
        print("  window: {} .. {}".format(rows[0]["date"], rows[-1]["date"]))
    if unmatched:
        print("  WARNING: no keeper price row for these models (cost counted as 0):")
        for model in sorted(unmatched):
            print("    {} ({} events)".format(model, unmatched[model]))
    stale_preview = "{}{}".format(empty_dates[:3], "..." if len(empty_dates) > 3 else "")
    print("  stale dates ({}/{}): {}".format(len(empty_dates), len(dates), stale_preview))

    if args.dry_run:
        print(json.dumps({"p_provider": args.provider, "p_device_id": args.device_id,
                          "p_rows": rows, "p_stale_dates": empty_dates}, indent=2)[:4000])
        print("dry run - nothing submitted")
        return 0

    status, body = call_sync_rpc(session_path, session, args.provider, args.device_id, rows, empty_dates)
    if status >= 300:
        print("submit failed ({}): {}".format(status, body), file=sys.stderr)
        return 1
    print("submitted provider={} device={}".format(args.provider, args.device_id))
    return 0


if __name__ == "__main__":
    sys.exit(main())

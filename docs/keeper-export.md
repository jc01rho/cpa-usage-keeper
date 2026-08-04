# Keeper export operator runbook

This runbook covers migration, deployment, credential lifecycle, export ingestion, recovery, monitoring, and Keeper release procedures for `cpa-usage-keeper`.

Related umbrella notes:

- [CLIProxyAPIPlus Keeper export notes](../../CLIProxyAPIPlus/docs/keeper-export.md)
- [Management Center Keeper export notes](../../Cli-Proxy-API-Management-Center/docs/keeper-export.md)

> **Scope:** Keeper uses SQLite. Treat the database, WAL/SHM files, backups, bearer tokens, credential responses, browser credentials, and private CA material as secrets.

## 1. Operating invariants

- Migration `20260803_keeper_instances` creates instance support and seeds the deterministic immutable instance `00000000-0000-7000-8000-000000000000`, named `Legacy`.
- Migration `20260803_keeper_metadata_snapshots` must run after `20260803_keeper_instances`.
- Migrations are forward-only; no destructive down migration is supported.
- Downgrade only by stopping Keeper, restoring a verified pre-migration backup, and starting the older binary.
- Fresh and migrated schemas enforce instance integrity with 42 triggers across 14 tables.
- `CPAUsageDelivery.InboxID` intentionally has no foreign key: the immutable replay ledger must survive inbox cleanup.
- Every initial instance credential must include `identity:test`.
- Raw tokens are displayed only when created, issued, or rotated; Keeper stores a salted Argon2id hash.
- Revoked or expired credentials are rejected, as are credentials for disabled instances.
- Public export requests cannot select an instance through query parameters, headers, or bodies; the bearer credential determines it.

## 2. API routes and scopes

All routes are under `/api/v1`.

### Browser-admin routes

These require browser administrator authentication and deployed request-intent protection; never substitute an export bearer token.

| Method | Exact route | Purpose |
|---|---|---|
| `GET` | `/api/v1/instances` | List instances |
| `POST` | `/api/v1/instances` | Create instance and initial credential |
| `PATCH` | `/api/v1/instances/:instanceId` | Update or disable instance |
| `GET` | `/api/v1/instances/:instanceId/credentials` | List credential metadata |
| `POST` | `/api/v1/instances/:instanceId/credentials` | Issue credential |
| `POST` | `/api/v1/instances/:instanceId/credentials/:credentialId/rotate` | Rotate credential |
| `POST` | `/api/v1/instances/:instanceId/credentials/:credentialId/revoke` | Revoke credential |

### Public bearer export routes

These use bearer credentials and do not use browser request-intent.

| Required scope | Method | Exact route |
|---|---|---|
| `identity:test` | `GET` | `/api/v1/export/identity` |
| `usage:push` | `POST` | `/api/v1/export/usage` |
| `usage:push` | `POST` | `/api/v1/export/usage-batches` |
| `metadata:push` | `PUT` | `/api/v1/export/metadata/auth-files` |
| `metadata:push` | `PUT` | `/api/v1/export/metadata/api-keys` |
| `metadata:push` | `PUT` | `/api/v1/export/metadata/provider-identities` |

`/api/v1/export/usage-batches` is an alias for `/api/v1/export/usage`.
Grant only `usage:push`, `metadata:push`, and `identity:test` as required; verify the mandatory initial `identity:test` scope before enabling export.

## 3. Instance selector rules

Administrative status/query interfaces use:

| Selector | Meaning |
|---|---|
| Omitted | Legacy only |
| `all` | All instances |
| Specific UUID | That instance only |

Legacy UUID: `00000000-0000-7000-8000-000000000000`.
Public ingestion forbids request-controlled instance selectors; never send an instance query parameter, header, or body field.
Every new CPA uses a separate instance, credential set, and outbox. API-key fingerprints are HMAC-derived inside that credential-bound instance domain; the same raw key in two CPAs intentionally produces different fingerprints and must not be joined across instances.

## 4. Migration preflight and backup

Require an active maintenance window, stoppable writers, known DB location, recorded old/new versions, room for two copies, non-ephemeral backup storage, and named rollback/go-no-go owners.
Do not commit deployment values or secrets:

```sh
export KEEPER_SERVICE='<systemd-unit-or-orchestrator-workload>'
export KEEPER_DB='<absolute-path-to-keeper.sqlite>'
export BACKUP_DIR='<protected-backup-directory>'
export NEW_BINARY='<path-to-new-keeper-binary>'
export OLD_BINARY='<path-to-current-keeper-binary>'
export KEEPER_BASE_URL='https://keeper.internal.example'
```

Stop every writer; scale replicated workloads to zero rather than stopping one replica.

```sh
sudo systemctl stop "$KEEPER_SERVICE"
sudo systemctl is-active "$KEEPER_SERVICE"
# Expected: inactive or failed; it must not be active.
sudo lsof "$KEEPER_DB" "${KEEPER_DB}-wal" "${KEEPER_DB}-shm" 2>/dev/null || true
```

Stop any remaining writer. If the optional `sqlite3` CLI is installed, check integrity and record SQLite state before copying:

```sh
if command -v sqlite3 >/dev/null 2>&1; then
  sqlite3 -readonly "$KEEPER_DB" 'PRAGMA integrity_check;'
  sqlite3 -readonly "$KEEPER_DB" <<'SQL'
PRAGMA journal_mode;
PRAGMA foreign_keys;
PRAGMA user_version;
SQL
fi
```

When the CLI is available, stop the upgrade unless integrity output is exactly `ok`; preserve all files and investigate.

### Required stopped byte-copy backup

Copy the main DB and every present sidecar as one set. The service must already be stopped:

```sh
umask 077
mkdir -p "$BACKUP_DIR"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
BACKUP_SET="$BACKUP_DIR/keeper-pre-migration-$STAMP"
mkdir -p "$BACKUP_SET"
cp --preserve=mode,ownership,timestamps "$KEEPER_DB" "$BACKUP_SET/"
for suffix in -wal -shm -journal; do
  [ ! -e "${KEEPER_DB}${suffix}" ] || cp --preserve=mode,ownership,timestamps "${KEEPER_DB}${suffix}" "$BACKUP_SET/"
done
chmod 0600 "$BACKUP_SET"/*
sha256sum "$BACKUP_SET"/* > "$BACKUP_SET/SHA256SUMS"
chmod 0600 "$BACKUP_SET/SHA256SUMS"
sha256sum -c "$BACKUP_SET/SHA256SUMS"
if command -v sqlite3 >/dev/null 2>&1; then
  sqlite3 -readonly "$BACKUP_SET/$(basename "$KEEPER_DB")" 'PRAGMA integrity_check; PRAGMA foreign_key_check;'
fi
```

Never copy only the main file while a WAL exists, or use ordinary `cp` on a live database.

### Validate on a temporary copy

Never make production the first migration test.

```sh
TEST_DIR="$(mktemp -d)"
chmod 700 "$TEST_DIR"
cp --preserve=mode,timestamps "$BACKUP_SET/$(basename "$KEEPER_DB")" "$TEST_DIR/app.db"
for suffix in -wal -shm -journal; do
  [ ! -e "$BACKUP_SET/$(basename "$KEEPER_DB")${suffix}" ] || cp --preserve=mode,timestamps "$BACKUP_SET/$(basename "$KEEPER_DB")${suffix}" "$TEST_DIR/app.db${suffix}"
done
TEST_PORT="$(python3 - <<'PY'
import socket
with socket.socket() as sock:
    sock.bind(('127.0.0.1', 0))
    print(sock.getsockname()[1])
PY
)"
cat >"$TEST_DIR/keeper.env" <<EOF
WORK_DIR=$TEST_DIR
CPA_BASE_URL=http://127.0.0.1:9
CPA_MANAGEMENT_KEY=temporary-nonproduction-key
APP_HOST=127.0.0.1
APP_PORT=$TEST_PORT
AUTH_ENABLED=true
LOGIN_PASSWORD=temporary-local-only-password
BACKUP_ENABLED=false
EOF
chmod 600 "$TEST_DIR/keeper.env"
"$NEW_BINARY" -env "$TEST_DIR/keeper.env" >"$TEST_DIR/server.log" 2>&1 &
TEST_PID=$!
cleanup_test_keeper() {
  kill "$TEST_PID" 2>/dev/null || true
  wait "$TEST_PID" 2>/dev/null || true
  rm -rf "$TEST_DIR"
}
trap cleanup_test_keeper EXIT INT TERM
# Bounded readiness/health probe against the temporary listener only.
curl --fail --silent --show-error --retry 30 --retry-connrefused \
  --retry-delay 0 --retry-max-time 30 "http://127.0.0.1:$TEST_PORT/healthz"
kill "$TEST_PID" 2>/dev/null || true
wait "$TEST_PID" 2>/dev/null || true
if command -v sqlite3 >/dev/null 2>&1; then
  sqlite3 -readonly "$TEST_DIR/app.db" 'PRAGMA integrity_check;'
  sqlite3 -readonly -header -column "$TEST_DIR/app.db" <<'SQL'
SELECT type, name, tbl_name
FROM sqlite_schema
WHERE type = 'trigger'
ORDER BY name;
SQL
  TRIGGER_COUNT="$(sqlite3 -readonly "$TEST_DIR/app.db" "SELECT count(*) FROM sqlite_schema WHERE type='trigger';")"
  printf 'Total SQLite triggers: %s\n' "$TRIGGER_COUNT"
fi
trap - EXIT INT TERM
rm -rf "$TEST_DIR"
```

Validate migration-defined trigger names, not only the total: unrelated triggers may make the total exceed 42.
Verify the actual migrated instance table contains the immutable Legacy row:

```sql
SELECT id, display_name, enabled
FROM cpa_instances
WHERE id = '00000000-0000-7000-8000-000000000000';
```
Proceed only if migration succeeds, integrity is `ok`, Legacy exists, expected triggers exist, and startup logs contain no migration/schema errors.

## 5. Production migration

1. Keep all Keeper processes stopped and retain checksum/integrity records.
2. Deploy the new binary without starting multiple replicas.
3. Start exactly one process so migrations run once under normal SQLite serialization.
4. Confirm order: `20260803_keeper_instances`, then `20260803_keeper_metadata_snapshots`.
5. Never manually apply the metadata migration first.
6. Watch startup logs, run post-migration checks and the identity canary, then restore normal capacity.
7. Check `https://<keeper-host><APP_BASE_PATH>/healthz`; omit `APP_BASE_PATH` when it is empty.

```sh
if command -v sqlite3 >/dev/null 2>&1; then
  sqlite3 -readonly "$KEEPER_DB" 'PRAGMA integrity_check;'
fi
sudo systemctl status "$KEEPER_SERVICE" --no-pager
sudo journalctl -u "$KEEPER_SERVICE" --since '-15 minutes' --no-pager
```

Integrity must be `ok`.

## 6. Rollback and downgrade

> **Warning:** Never delete tables, columns, triggers, instance rows, delivery rows, or metadata snapshot rows to make an older binary start.

1. Stop every process.
2. Preserve the failed post-migration DB for diagnosis.
3. Restore the verified pre-migration backup.
4. Restore ownership and restrictive permissions.
5. Verify integrity, deploy the older binary, then validate one process before restoring capacity.

```sh
sudo systemctl stop "$KEEPER_SERVICE"
backup_main="$BACKUP_SET/$(basename "$KEEPER_DB")"
if [ ! -f "$backup_main" ]; then
  printf '%s\n' 'Verified backup main DB is missing; aborting before replacement.' >&2
  exit 1
fi
sha256sum -c "$BACKUP_SET/SHA256SUMS"
FAILED_SET="$BACKUP_DIR/keeper-post-migration-failed-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -m 0700 "$FAILED_SET"
shopt -s nullglob
current_db_files=("$KEEPER_DB"*)
if ((${#current_db_files[@]} > 0)); then
  cp --preserve=mode,ownership,timestamps "${current_db_files[@]}" "$FAILED_SET/"
fi

# WARNING: destructive restoration begins here only after backup verification.
rm -f "$KEEPER_DB" "${KEEPER_DB}-wal" "${KEEPER_DB}-shm" "${KEEPER_DB}-journal"
cp --preserve=mode,ownership,timestamps "$backup_main" "$KEEPER_DB"
for suffix in -wal -shm -journal; do
  [ ! -e "$BACKUP_SET/$(basename "$KEEPER_DB")${suffix}" ] || cp --preserve=mode,ownership,timestamps "$BACKUP_SET/$(basename "$KEEPER_DB")${suffix}" "${KEEPER_DB}${suffix}"
done
chmod 0600 "$KEEPER_DB"*
if command -v sqlite3 >/dev/null 2>&1; then
  sqlite3 -readonly "$KEEPER_DB" 'PRAGMA integrity_check;'
fi
# Reinstall or select $OLD_BINARY only after the old database is restored.
sudo systemctl start "$KEEPER_SERVICE"
```

Rollback loses writes accepted after migration; if they must be preserved, stop ingestion and escalate instead of attempting schema reversal.

## 7. Network and TLS

- Expose export routes only through HTTPS on a private network or tightly restricted ingress.
- Prefer private subnets/services; restrict sources to approved exporters and administrator networks.
- Preserve the protocol's 1 MiB decompressed request-body limit and CPA batch limits at the reverse proxy; do not silently decompress, rewrite, or compress request bodies.
- Preserve Keeper status codes, including authentication, conflict, and rate-limit responses.
- Never log authorization headers or secret-bearing request bodies.
- Install private CA certificates in exporter trust stores; never disable verification as a permanent workaround.
- A reverse proxy may add mTLS, but bearer authorization remains required unless application documentation explicitly states otherwise.
- Do not invent Keeper TLS, custom-CA, or mTLS flags; use only verified binary capabilities or the reverse proxy.

## 8. Credential bootstrap and lifecycle

Admin routes require the existing Keeper admin session when `AUTH_ENABLED=true`, and every mutating browser API call requires the exact request-intent header. Use a protected cookie jar captured through the normal login flow; do not copy the cookie into shell history.

```sh
export ADMIN_COOKIE_JAR='<absolute-path-to-mode-0600-cookie-jar>'
export INSTANCE_ID='<instance-uuid>'
export CREDENTIAL_ID='<credential-uuid>'
```

Keep request/response files mode `0600`; delete sensitive temporary files under local policy. For instance creation, initial credentials must include `identity:test`; a normal exporter credential uses all three scopes.

```sh
umask 077
cat >./instance-create.json <<'JSON'
{
  "protocolVersion": "keeper-export/v1",
  "displayName": "cpa-prod-01",
  "credential": {
    "name": "bootstrap-2026-08",
    "scopes": ["usage:push", "metadata:push", "identity:test"]
  }
}
JSON
curl --fail-with-body --silent --show-error \
  -X POST "$KEEPER_BASE_URL/api/v1/instances" \
  -H 'Content-Type: application/json' \
  -H 'X-CPA-Usage-Keeper-Request: fetch' \
  --cookie "$ADMIN_COOKIE_JAR" \
  --data-binary @./instance-create.json \
  >./instance-create-response.secret.json
chmod 600 ./instance-create-response.secret.json
```

Capture the one-time raw token directly into the approved secret manager; it cannot be retrieved from the Argon2id hash.
Test identity before enabling export:

```sh
read -r -s -p 'Keeper bearer token: ' KEEPER_TOKEN
printf '\n'
curl --config - <<EOF
fail-with-body
silent
show-error
url = "$KEEPER_BASE_URL/api/v1/export/identity"
header = "Authorization: Bearer $KEEPER_TOKEN"
EOF
unset KEEPER_TOKEN
```

Confirm returned `instance.instanceId` and `instance.displayName` match the intended instance.
Issue additional credentials through `POST /api/v1/instances/$INSTANCE_ID/credentials`, capturing the displayed-once token securely. Issue/rotate requests use the same exact credential body:

```sh
cat >./credential-replacement.json <<'JSON'
{
  "name": "replacement-2026-08",
  "scopes": ["usage:push", "metadata:push", "identity:test"],
  "expiresAt": null
}
JSON
# Safer overlap rotation: issue first, install/test the new token, then revoke old.
curl --fail-with-body --silent --show-error \
  -X POST "$KEEPER_BASE_URL/api/v1/instances/$INSTANCE_ID/credentials" \
  -H 'Content-Type: application/json' \
  -H 'X-CPA-Usage-Keeper-Request: fetch' \
  --cookie "$ADMIN_COOKIE_JAR" \
  --data-binary @./credential-replacement.json \
  >./credential-issue-response.secret.json
chmod 600 ./credential-issue-response.secret.json

# Atomic rotate: replacement is created and old credential revoked together.
curl --fail-with-body --silent --show-error \
  -X POST "$KEEPER_BASE_URL/api/v1/instances/$INSTANCE_ID/credentials/$CREDENTIAL_ID/rotate" \
  -H 'Content-Type: application/json' \
  -H 'X-CPA-Usage-Keeper-Request: fetch' \
  --cookie "$ADMIN_COOKIE_JAR" \
  --data-binary @./credential-replacement.json \
  >./credential-rotate-response.secret.json
chmod 600 ./credential-rotate-response.secret.json
```

Store the new token, update the exporter without shell-history exposure, reload it, test identity, verify the instance, and remove the response file.
Revoke compromised, retired, or superseded credentials:

```sh
curl --fail-with-body --silent --show-error \
  -X POST "$KEEPER_BASE_URL/api/v1/instances/$INSTANCE_ID/credentials/$CREDENTIAL_ID/revoke" \
  -H 'X-CPA-Usage-Keeper-Request: fetch' \
  --cookie "$ADMIN_COOKIE_JAR"
```

Verify the old token is rejected without pasting it into tickets or logs.
Disabling an instance rejects all its credentials:

```sh
cat >./instance-disable.json <<'JSON'
{"enabled":false}
JSON
chmod 600 ./instance-disable.json
curl --fail-with-body --silent --show-error \
  -X PATCH "$KEEPER_BASE_URL/api/v1/instances/$INSTANCE_ID" \
  -H 'Content-Type: application/json' \
  -H 'X-CPA-Usage-Keeper-Request: fetch' \
  --cookie "$ADMIN_COOKIE_JAR" \
  --data-binary @./instance-disable.json
```

Set `{"enabled":true}` to re-enable after containment. Verify rejection while disabled, and decide which credentials require revocation or rotation before re-enabling.

After capture, securely remove `instance-create.json`, `instance-create-response.secret.json`, `credential-replacement.json`, `credential-issue-response.secret.json`, `credential-rotate-response.secret.json`, and `instance-disable.json`; never archive plaintext one-time tokens in evidence.

## 9. Export semantics

### Legacy pull to push cutover

Use a quiet or maintenance window for each CPA. Stop/drain new CPA request traffic, let the legacy pull source finish its final drain, disable that legacy source, enable the new exporter, wait for it to bind to the expected non-Legacy instance and report healthy status, then resume traffic. The legacy path writes to `Legacy`; push writes to the newly registered instance. Running both creates separately attributed duplicate collection, while admitting traffic before initial outbox identity binding can create an observable drop. Record the cutover timestamp and never rewrite `instance_id` to merge the namespaces.

For rollback, stop/drain traffic, disable push while preserving the CPA outbox, confirm disabled state, enable legacy pull, then resume. This preserves queued push data for a later forward recovery.

### Usage

Usage ingestion atomically updates the inbox, immutable delivery/replay ledger, and watermark.
Exact replays are idempotent; the same replay identity with different content returns HTTP `409 Conflict`.
Forward gaps are accepted, but acknowledgement remains at the highest contiguous point; retain and resend unacknowledged batches until gaps close.
If Keeper commits and the HTTP response is lost, CPA retries the same sequence/digest and Keeper treats it as replay instead of a second event. Keeper, proxy, or network restarts therefore require no stream reset; delivery resumes from the durable watermark.
Never delete or alter `CPAUsageDelivery` rows or add an FK to `InboxID`.
Send usage to `/api/v1/export/usage` or its `/api/v1/export/usage-batches` alias with `usage:push`; do not add an instance selector.

### Metadata

Categories are independent: `/metadata/auth-files`, `/metadata/api-keys`, and `/metadata/provider-identities` under `/api/v1/export`.
An absent revision means revision `0`; equal revision plus identical body is idempotent, equal revision plus changed body conflicts, and lower revision is stale.
A newer `complete:true` snapshot is the full category and deletes only absent records in the same instance/category. A newer valid empty complete snapshot intentionally clears that category; incomplete, malformed, stale, conflicting, or unauthorized requests mutate nothing.
Exporters must persist each attempted body/revision until acknowledged, retry outages identically, never reuse a revision for changed content, and increment monotonically per instance/category.
Mark complete only after collecting the entire category; never assume incomplete snapshots were partially applied.

## 10. Rate limiting

Limits are 60 requests/minute sustained, burst 20, 4096 limiter entries, and 30-minute idle eviction.
Batch records, pace retries, and use bounded exponential backoff with jitter while retaining the exact body and replay identity.
Never rotate credentials to evade limits; alert on sustained limiting, which indicates poor pacing, excess concurrency, or undersized batches.

## 11. Monitoring and inspection

Alert on unavailability/restart loops; migration, lock, corruption, or SQLite errors; abnormal authentication failures; revoked/expired/disabled-instance rejection; sustained limits; usage `409`; metadata conflicts; sequence gaps/stalled ACKs; latency/backlog; disk/inode exhaustion; abnormal DB/WAL growth; and failed backup integrity checks.
Never place tokens, bodies, cookies, or request-intent values in logs or metric labels.
Use selector semantics exactly: omitted for Legacy, `all` fleet-wide, or UUID for one instance; do not infer public-ingest selectors.

```sh
curl --fail-with-body --silent --show-error \
  --cookie "$ADMIN_COOKIE_JAR" \
  "$KEEPER_BASE_URL/api/v1/instances"
curl --fail-with-body --silent --show-error \
  --cookie "$ADMIN_COOKIE_JAR" \
  "$KEEPER_BASE_URL/api/v1/instances/$INSTANCE_ID/credentials"
```

Prefer HTTP status/query APIs. For direct inspection, avoid expensive live-writer queries, use read-only mode, never edit protected rows, and back up before emergency repair.

```sh
sqlite3 -readonly "$KEEPER_DB" <<'SQL'
SELECT type, name, tbl_name
FROM sqlite_schema
WHERE type IN ('table', 'index', 'trigger')
ORDER BY type, name;
SQL
```

## 12. Incident procedures

- **Outage:** queue durably; preserve replay IDs, sequences, revisions, and bodies; restore Keeper; retry with jitter; verify usage ACK and metadata exact replay. A timeout does not prove transaction failure.
- **Usage `409`:** stop the stream, preserve local payload/response, inspect persistence and allocation, never edit the ledger or hide conflict behind a new identity, and resume only after identifying authoritative content.
- **Forward gap:** find the earliest missing durable batch, replay its original identity/body, replay later retained batches as needed, and verify contiguous ACK advancement; never delete accepted forward batches or edit the watermark.
- **Equal metadata conflict:** stop that instance/category, restore one immutable body per revision, and publish changed content at a higher revision.
- **Stale metadata revision:** discover the last accepted revision, repair persistence, rebuild the snapshot, and send a higher revision.
- Never mark incomplete metadata complete to force convergence; complete snapshots can delete records.
- **Token compromise:** revoke immediately; disable the instance if impact is uncertain; search logs without exposing the token; replace and store the credential; test `/api/v1/export/identity`; re-enable only after containment; review unexpected writes.

## 13. Secret handling and troubleshooting

Store tokens only in an approved secret manager; use hidden input, mode-`0600` files, or process-level injection rather than visible arguments.
Never commit tokens, cookies, private keys, request-intent values, or raw credential responses; redact authorization, cookie, and proxy-authorization headers and disable body logging on export/credential routes.
Backups and Argon2id hashes remain sensitive; independently restrict custom-CA keys and mTLS client keys.

| Symptom | Action |
|---|---|
| `401`/authentication rejection | Check injection and credential metadata; rotate if missing, malformed, expired, or revoked |
| Valid credential rejected for all scopes | Check disabled instance state before re-enabling |
| Identity works; usage or metadata denied | Add only the required `usage:push` or `metadata:push` scope |
| Initial identity test fails | Correct missing `identity:test` or wrong deployed token before export |
| Sustained limiting | Batch, reduce concurrency, and add jittered backoff |
| Usage `409` | Stop and repair replay persistence; never edit the ledger |
| Usage accepted but ACK stalls | Replay earliest gap with original identity/body |
| Metadata exact retry conflicts | Restore immutable revision/body mapping; use a higher revision for changes |
| Metadata stale | Recover accepted revision and send newer state |
| Unexpected metadata deletion | Repair false-complete collection and send a corrected higher complete revision |
| Integrity check fails | Stop, preserve evidence, and restore a verified backup |
| Older binary fails | Restore the pre-migration DB; do not reverse the schema |
| Legacy data missing | Omit selector/use Legacy UUID; if seed is absent, stop and investigate migration |
| TLS verification fails | Repair CA chain/hostname; never permanently disable verification |

## 14. Canary, rollout, and tag strategy

Concise migration/release checklist:

- [ ] Production DB path, maintenance owner, old/new binaries, and rollback owner recorded.
- [ ] All Keeper writers stopped; cold DB/sidecar backup hashed and access-restricted.
- [ ] Temporary-copy migration and optional integrity checks passed.
- [ ] `20260803_keeper_instances` then metadata migration applied; Legacy row and integrity triggers verified.
- [ ] One instance/credential/outbox per CPA; `identity:test` bootstrap confirms the intended ID.
- [ ] Quiet legacy-pull-to-push canary cutover completed; ACK/gap/replay/metadata behavior healthy.
- [ ] Credential rotate/revoke and instance disable/re-enable exercised without exposing tokens.
- [ ] Genuine rollback procedure restores backup before older binary; no down migration attempted.
- [ ] Logs, DB exports, screenshots, and evidence contain no tokens or provider secrets.
- [ ] Explicit release approval obtained before any commit/tag/push.

Before deployment, verify owners/window, versions, stopped backup, integrity `ok`, checksum, restricted storage, temporary migration, Legacy seed, 42 triggers/14 tables, absent `CPAUsageDelivery.InboxID` FK, and reviewed restore procedure.
Canary one process: verify migration order/logs/integrity, list instances and credential metadata, test identity, send and exactly replay usage, and test changed-body `409` only in an isolated non-production stream.
Test metadata exact replay in an isolated category/test instance; confirm no secrets in telemetry and that proxies preserve status codes and rate limiting.
Roll out exporters gradually while watching authentication, conflicts, gaps, limits, DB growth, latency, contiguous ACKs, and independent metadata revisions; restore capacity only after success and retain the backup through the rollback window.
Post-release, require no unexplained migration/SQLite errors, conflicts, gaps, or deletions; verify disabled/revoked/expired enforcement and backup integrity; record only actions actually performed.
Keeper tags are independent of CPA/UI tags and use `v<major>.<minor>.<patch>-<seq>`; sibling versions need not match. If upstream has a newer base version, reset the fork suffix to `-1`; otherwise increment the current suffix. Validate the commit, migrations, checklist, and release validation before calculating the candidate tag. Never claim release from a proposed tag and never use `git push --tags`.

```sh
TAG='v<major>.<minor>.<patch>-<seq>'
case "$TAG" in
  v[0-9]*.[0-9]*.[0-9]*-[0-9]*) ;;
  *) printf 'Refusing invalid tag: %s\n' "$TAG" >&2; exit 1 ;;
esac
printf 'Candidate tag: %s\n' "$TAG"
```

Only after explicit release approval, create one lightweight tag inside this repository. Every `main` push must be paired with exactly that one tag push. The commands are intentionally commented so a documentation walkthrough cannot release accidentally:

```sh
# git tag "$TAG"
# git push origin main
# git push origin "$TAG"
```

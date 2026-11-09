# Kira API contract (v1)

Source of truth for Phase 1–2. Mirrors TRD §5. Go types: `internal/api/dto.go`. TS types: `web/src/api/types.ts`.
**Only the main session changes this file.** Lane agents request changes under "Cross-lane requests" in PLAN.md.

## Conventions
- JSON, UTF-8, `snake_case` fields. Dates `YYYY-MM-DD`. Timestamps RFC 3339 UTC (`2026-11-09T03:12:44Z`). Months `YYYY-MM`.
- **Money is integer sen** (`*_sen`, int64). The API never sends floats or formatted strings for money (CSV export is the only place amounts are formatted).
- Absent optional values are `null`, never omitted.
- All `/api/*` routes require the session cookie `kira_session`. Middleware re-checks the session's email + sub against the allow-list on **every** request (TR-3).
- Request bodies are limited to 64 KB, except `POST /api/receipts` (10 MB + multipart overhead).
- State-changing requests (`POST`/`PATCH`) must send `Content-Type: application/json` (or `multipart/form-data` for receipts). This, plus `SameSite=Lax`, is the CSRF defence.

### Errors
Every non-2xx JSON response:
```json
{ "error": "human readable message", "code": "validation", "fields": { "amount_sen": "must be greater than 0" } }
```
`fields` is `null` unless `code` is `validation`.

| HTTP | code | When |
|---|---|---|
| 400 | `bad_request` | Malformed JSON, bad query param |
| 401 | `unauthorized` | No/expired/unknown session. Frontend redirects to `/login` |
| 403 | `forbidden` | Session's identity no longer passes the allow-list |
| 404 | `not_found` | Unknown entry/receipt/category |
| 409 | `conflict` | Amend/void of an already-voided entry; amend/void target is not an original `create` row; duplicate category name |
| 413 | `payload_too_large` | Body over limit; receipt > 10 MB |
| 415 | `unsupported_media_type` | Receipt not JPEG/PNG/PDF; wrong request Content-Type |
| 422 | `validation` | Field errors (see `fields`) |
| 422 | `date_confirm_required` | `occurred_on` is more than 1 year from today and `confirm_date` was not `true` |
| 423 | `ledger_broken` | A write was attempted while verify has failed and it has not been acknowledged (TR-7) |
| 429 | `rate_limited` | More than 10 requests/min/IP on `/auth/*` |
| 500 | `internal` | Anything else (message is generic; details only in server log) |

### Enums
- `kind`: `create` | `amend` | `void`
- `direction`: `expense` | `income` (only `expense` is used in v1; FR-9 deferred)
- `method`: `card` | `cash` | `bank_transfer` | `tng_ewallet` | `grabpay` | `other`
  - Labels: Card, Cash, Bank transfer, TNG eWallet, GrabPay, Other
- `status` (current view): `active` | `amended` | `voided`

### Validation (TRD §7)
| Field | Rule |
|---|---|
| `amount_sen` | integer, `> 0` and `< 1_000_000_000` (RM 10,000,000) |
| `occurred_on` | valid `YYYY-MM-DD`; if more than 365 days before/after today (server local date) → 422 `date_confirm_required` unless `confirm_date: true` |
| `category_id` | existing, non-archived category (an amend may keep the entry's current category even if it was archived since) |
| `method` | one of the enum |
| `payee` | required, trimmed, 1–120 chars |
| `note` | optional, ≤ 500 chars (empty string → `null`) |
| `receipt_sha256` | `null` or 64 lowercase hex of a receipt already uploaded |
| `reason` (void) | required, trimmed, 1–300 chars |
| category `name` | trimmed, 1–60 chars, unique case-insensitively |

## Objects

### Entry (current view, `v_current` — TR-9)
One per original `create` row, showing its latest version.
```json
{
  "id": 126,                    // id of the ORIGINAL create row; stable entry id used in URLs
  "version_id": 127,            // id of the latest row applied (create or amend); == id if never amended
  "direction": "expense",
  "occurred_on": "2026-11-08",
  "amount_sen": 4480,
  "currency": "MYR",
  "category_id": 2,
  "category_name": "Software & subscriptions",
  "payee": "GitHub Copilot",
  "method": "card",
  "note": "Monthly plan, Nov",
  "receipt_sha256": "3e9a…c41f",   // null if none
  "status": "amended",          // active | amended | voided
  "amend_count": 1,
  "void_reason": null,          // set when status = voided
  "created_at": "2026-11-08T02:02:00Z",  // original create row
  "updated_at": "2026-11-08T13:14:00Z",  // latest row affecting this entry (amend or void)
  "hash": "9f1c…a03e"           // hash of the latest row affecting this entry
}
```
A voided entry keeps the field values of its last create/amend version.

### LedgerRow (raw chain row — entry history)
```json
{
  "id": 127, "kind": "amend", "ref_id": 126,
  "direction": "expense", "occurred_on": "2026-11-08", "amount_sen": 4480, "currency": "MYR",
  "category_id": 2, "payee": "GitHub Copilot", "method": "card", "note": "Monthly plan, Nov",
  "void_reason": null, "receipt_sha256": "3e9a…c41f",
  "created_at": "2026-11-08T13:14:00Z",
  "prev_hash": "4b7e…11d9", "hash": "9f1c…a03e"
}
```
For `void` rows: `occurred_on`, `amount_sen`, `category_id`, `payee`, `method`, `note`, `receipt_sha256` are `null`; `void_reason` is set. For `create` rows `ref_id` is `null`.

### AppendResult (response of create / amend / void, 201)
Superset of the TRD §5 example: the new row's fields at top level, plus the updated current view.
```json
{ "id": 128, "kind": "create", "ref_id": null, "amount_sen": 4590, "...": "all LedgerRow fields",
  "hash": "9f1c…", "created_at": "2026-11-09T03:12:44Z",
  "entry": { "...": "Entry" } }
```
Returned **only after the transaction has committed** with `synchronous=FULL` (TR-5).

### Category
```json
{ "id": 1, "name": "Cloud & hosting", "color": "#0F766E", "icon": null, "archived_at": null }
```
Seed order (TR-14, FR-7): Cloud & hosting, Software & subscriptions, Hardware, Internet & phone, Travel & transport, Meals & entertainment, Professional fees, Marketing, Office, Other.
Seed colors cycle through `--cat-1..--cat-5` in that order (`#0F766E #2F6FB5 #C2701E #7A5BB0 #9AA09B`). `Other` uses `#9AA09B`.

### VerifyResult
```json
{
  "ok": true,
  "entries": 128,                    // total ledger rows checked
  "head_id": 128, "head_hash": "9f1c…a03e",   // null/"" on empty ledger: head_id 0, head_hash = 64 zeros
  "broken_at": null,                 // first bad row id when ok = false
  "reason": null,                    // e.g. "hash mismatch", "prev_hash mismatch", "id gap"
  "checked_at": "2026-11-09T03:12:44Z",
  "acknowledged": false,             // true once the owner acknowledged a broken ledger (writes re-enabled)
  "last_backup_at": null             // RFC 3339 if known (Litestream replica mtime), else null
}
```

## Endpoints

### Auth (public unless noted; rate limited 10/min/IP)
| Method | Path | Behaviour |
|---|---|---|
| GET | `/auth/google/login` | 302 to Google. Sets short-lived signed cookie `kira_oauth` holding state, PKCE verifier, nonce (10 min, HttpOnly, SameSite=Lax). |
| GET | `/auth/google/callback?code&state` | Verifies state, exchanges code with PKCE, verifies ID token (sig, iss, aud, exp, nonce) via go-oidc, then allow-list (TR-2). **Pass:** create session, set `kira_session`, 302 to `/`. **Fail:** 403 HTML page "This Google account is not allowed", `auth_log` row with result reason, no session. Bad/missing state → 400 HTML page. |
| POST | `/auth/logout` | Session required. Deletes the session, clears cookie, 204. |
| POST | `/auth/test-login` | **Only in `-tags e2e` builds** (404 in normal builds). Creates a session for `ALLOWED_EMAIL` with sub `e2e-test-sub`, 204. |

`kira_session` cookie: 32 random bytes base64url, `HttpOnly; SameSite=Lax; Path=/; Max-Age=2592000`, `Secure` when `PUBLIC_URL` is https. DB stores only SHA-256 of the token.

`auth_log.result` values: `ok`, `owner_pinned`, `reject_email`, `reject_unverified`, `reject_sub`, `reject_token`, `reject_state`.

### GET /api/me → 200
```json
{ "email": "owner@gmail.com", "name": "Jaxz", "picture": "https://…" }
```
`name`/`picture` may be `null`.

### GET /api/entries → 200
Query (all optional): `from`, `to` (YYYY-MM-DD, inclusive, on `occurred_on`), `category` (id), `method` (enum), `q` (case-insensitive substring of payee or note), `status` = `all` (default) | `active` | `voided` (`active` includes amended).
```json
{ "entries": [Entry, …], "total_sen": 128450, "count": 18, "voided_count": 1 }
```
Ordered by `occurred_on` desc, then `id` desc. Voided entries are included (struck through in UI) but excluded from `total_sen` and `count`. No pagination in v1.

### POST /api/entries → 201 AppendResult (FR-3)
```json
{ "occurred_on": "2026-11-09", "amount_sen": 4590, "category_id": 1, "payee": "Cloudflare",
  "method": "card", "note": "R2 + domain", "receipt_sha256": null, "confirm_date": false }
```
`direction` is optional (default `expense`). `confirm_date` optional (default false).

### GET /api/entries/:id → 200 Entry
`:id` is the original create id. 404 if not a create row.

### GET /api/entries/:id/history → 200 (FR-4, FR-5)
```json
{ "entry": Entry, "rows": [LedgerRow, …] }
```
`rows` = the create row plus every amend/void with `ref_id = :id`, ordered by `id` **ascending**.

### POST /api/entries/:id/amend → 201 AppendResult (FR-4)
Body = same fields as create (the full corrected version, not a patch). `ref_id` = `:id` (always the original create, even after several amends). 409 if voided.

### POST /api/entries/:id/void → 201 AppendResult (FR-5)
```json
{ "reason": "Duplicate of #123" }
```
409 if already voided.

### POST /api/receipts → 201 (FR-6, TR-8)
`multipart/form-data`, field `file`. JPEG/PNG/PDF only (detected by content sniffing, not filename), ≤ 10 MB. Images are re-encoded with EXIF/GPS stripped before hashing; the stored file is `data/receipts/<sha256>.<ext>`. Uploading identical content is idempotent.
```json
{ "sha256": "3e9a…c41f", "ext": "pdf", "content_type": "application/pdf", "size": 48213 }
```

### GET /api/receipts/:sha256 → 200 file
`:sha256` must be 64 lowercase hex (else 400). Served with the stored content type, `Content-Disposition: inline; filename="<sha256>.<ext>"`, `X-Content-Type-Options: nosniff`, `Cache-Control: private, max-age=31536000, immutable`.

### Categories (FR-7)
| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/api/categories?include_archived=1` | — | 200 `{ "categories": [Category] }` (archived excluded unless flag; ordered by id) |
| POST | `/api/categories` | `{ "name": "Courses", "color": "#2F6FB5" }` (`color` optional) | 201 Category |
| PATCH | `/api/categories/:id` | `{ "name": "…" }` and/or `{ "archived": true \| false }` | 200 Category |

Categories are not part of the ledger chain (entries store `category_id`); renaming does not change history rows. Archived categories still resolve their names on old entries.

### GET /api/summary?month=YYYY-MM → 200 (FR-10)
`month` defaults to the current month (server local time). All totals use the current view and exclude voided entries.
```json
{
  "month": "2026-11",
  "total_sen": 128450,
  "count": 18,
  "voided_count": 1,
  "prev_month_total_sen": 114680,
  "change_pct": 12.0,            // null when prev month total is 0; rounded to 1 decimal
  "missing_receipts": 3,         // non-voided entries this month with receipt_sha256 = null
  "by_category": [ { "category_id": 1, "name": "Cloud & hosting", "color": "#0F766E", "total_sen": 41200, "pct": 32.1 } ],
  "trend": [ { "month": "2026-06", "total_sen": 64000 }, "… 6 items, oldest first, ending with month" ],
  "recent": [Entry]              // 5 most recent by occurred_on desc, id desc (all months; voided included)
}
```
`by_category` is sorted by `total_sen` desc and only includes categories with a non-zero total.

### GET /api/export?from&to&format=csv|zip → 200 (FR-12, TR-10)
`from`/`to` required, inclusive. `format` default `csv`.
- `csv`: `text/csv; charset=utf-8`, `Content-Disposition: attachment; filename="kira-<from>_<to>.csv"`.
- `zip`: `application/zip`, `kira-<from>_<to>.zip` containing `expenses.csv` and `receipts/<sha256>.<ext>` for every referenced receipt.

CSV header (exact order): `date,category,payee,method,amount_myr,note,status,entry_id,receipt_file`
- Rows = current view in range **including voided** entries (`status` = `active` | `amended` | `voided`), ordered by date asc, id asc.
- `amount_myr` = sen formatted as `1234.56` (no thousands separator, no currency prefix). `method` = label (e.g. `TNG eWallet`). `receipt_file` = `<sha256>.<ext>` or empty. RFC 4180 quoting, CRLF line endings. Cells beginning with `= + - @`, TAB or CR are prefixed with `'` (CSV injection guard). If a referenced receipt file is missing, `receipt_file` is the bare sha256 and the zip lists it in `MISSING_RECEIPTS.txt`.

### GET /api/ledger/verify → 200 VerifyResult (FR-13, TR-7)
Recomputes the full chain on every call, and compares against the last recorded head (`data/head`, `id:hash`, written by the server after each successful verify/append) so deleted tail rows are detected (TRD §4).

### POST /api/ledger/acknowledge → 200 VerifyResult (TR-7)
Owner acknowledges a broken ledger; writes are re-enabled. The acknowledgement covers the exact set of problems found; a later verify that finds any new or different problem blocks writes again (new valid appends do not). Derived from TR-7 ("app stays read-only until acknowledged"); no body.

## Implementation notes (Phase 2, accepted by main)
- A JSON field of the wrong type → 422 `validation` naming the field (not 400).
- While the ledger is broken and unacknowledged, **every** POST/PATCH under `/api` except `/api/ledger/acknowledge` returns 423 (categories and receipt uploads too).
- Unknown `/api/*` paths: 401 without a session, 404 with one. 405 under `/api` uses code `bad_request`.
- Empty category PATCH, or export with `from` > `to` → 400 `bad_request`.
- Server listens on `127.0.0.1:PORT` by default; optional `HOST` overrides.
- `last_backup_at` = newest file mtime under the local Litestream replica (`./backup/kira.db` or `DATA_DIR/../backup/kira.db`), else null.

## Phase 4 hardening (accepted by main)
- **CSRF:** every non-GET request under `/api/*` and `POST /auth/logout` must carry `Sec-Fetch-Site: same-origin`, or (if that header is absent) an `Origin` equal to `PUBLIC_URL`'s origin; otherwise 403 `forbidden` ("cross-site request"). Responses add `Cross-Origin-Resource-Policy: same-origin`.
- **direction:** only `expense` is accepted in v1; `income` → 422 `fields.direction` (FR-9 deferred).
- **receipt_sha256:** `""` → 422 (only `null` or 64 lowercase hex). `note` is trimmed; whitespace-only → `null`.
- **Validation** reports every failing field in one response (including unknown `category_id`).
- **Unknown JSON fields** → 400 `bad_request`. Duplicate keys: last wins.
- **GET /api/entries** with `from` > `to` → 400 (same as export).
- **Verify also checks receipts:** every distinct referenced `receipt_sha256` is re-hashed; problems use `reason` `"receipt mismatch"` / `"receipt missing"` with `broken_at` = the first ledger row referencing it. `GET /api/receipts/:sha` returns 409 `conflict` if the file no longer matches its hash; zip export lists such files in `TAMPERED_RECEIPTS.txt`.
- **Head file:** `DATA_DIR/head` is `id:hash:mac` where mac = HMAC-SHA256(HKDF(LEDGER_KEY,"kira-head"), "head|id|hash"). It only ever moves forward. If entries exist and the head file is missing, malformed or has a bad MAC → verify fails with `reason` `"head missing"` / `"head invalid"` (needs acknowledge).
- **Acknowledge** is persisted (`DATA_DIR/ack`, 0600) so it survives restarts, and is recorded in an append-only `integrity_events` table (`at`, `kind`=`acknowledge`, `recorded_head`, `actual_head`, `problems` JSON). An acknowledgement covers its problems; a later verify whose problems are a **subset** of acknowledged ones stays acknowledged (so appends after an acknowledged tail deletion keep working); any new problem blocks writes again.

## Frontend routes (design/README.md)
`/login`, `/` (dashboard), `/entries`, `/entries/:id`, `/export`, `/settings`. Unknown paths serve `index.html` (SPA fallback) except `/api/*` and `/auth/*`.

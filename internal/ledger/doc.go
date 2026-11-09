// Package ledger is the only code path that writes Kira's append-only,
// hash-chained entries table (TR-4), plus the read side over v_current
// (TR-9): list, summary, export rows, entry detail and history.
//
// # Writes
//
// Service.Append (and the Create/Amend/Void wrappers) validates input,
// takes the service mutex, opens a BEGIN IMMEDIATE transaction, assigns
// id = head id + 1, computes prev_hash/hash, inserts, and commits with
// synchronous=FULL before returning. A returned AppendResult therefore means
// the row is durable (TR-5).
//
// # Hash chain (CLAUDE.md rule 3)
//
//	hash_n = lowercase_hex( HMAC_SHA256( LEDGER_KEY, prev_hash_n || canonical_json(row_n) ) )
//	prev_hash_1 = "0000…0000" (64 ASCII zeros); prev_hash_n = hash_(n-1)
//
// prev_hash_n is concatenated as its 64 ASCII hex characters (not decoded).
// row_n is every column of the entries row EXCEPT hash, i.e. exactly these
// 15 keys:
//
//	amount_sen category_id created_at currency direction id kind method
//	note occurred_on payee prev_hash receipt_sha256 ref_id void_reason
//
// canonical_json is a single JSON object with:
//   - keys sorted by byte order (the order listed above), no whitespace
//     anywhere (no spaces after ':' or ',');
//   - SQL NULL encoded as null; INTEGER columns (id, ref_id, amount_sen,
//     category_id) as base-10 integers with no sign for positives, no
//     exponent, no leading zeros; TEXT columns as JSON strings;
//   - strings escaped as Go's encoding/json does with SetEscapeHTML(false):
//     '"' and '\\' backslash-escaped, \b \f \n \r \t short escapes, other
//     control characters < 0x20 as \u00XX (lowercase hex), U+2028 and
//     U+2029 as the six-character escapes backslash-u-2-0-2-8/9, and every
//     other character (including '<', '>', '&', DEL and non-ASCII) as
//     literal UTF-8. Input text is rejected unless valid UTF-8.
//
// Example (create row 1):
//
//	{"amount_sen":4590,"category_id":1,"created_at":"2026-11-09T03:12:44Z","currency":"MYR","direction":"expense","id":1,"kind":"create","method":"card","note":"R2 + domain","occurred_on":"2026-11-09","payee":"Cloudflare","prev_hash":"000…000","receipt_sha256":null,"ref_id":null,"void_reason":null}
//
// Verify recomputes this for every row in id order and reports the first
// bad row (broken_at) with a reason: "id gap", "prev_hash mismatch",
// "hash mismatch", "invalid column type" or "head mismatch" (the last only
// when a previously recorded head is supplied; it catches deleted tail rows).
package ledger

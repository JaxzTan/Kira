# PRD: Kira — Personal Business Expense Recorder

| Field | Value |
|---|---|
| Author | Jaxz |
| Status | Draft |
| Version | 0.1 |
| Last updated | 2026-11-09 |
| Related TRD | TRD-kira.md |

> "Kira" = Malay for "count / calculate". Rename freely.

## 1. Problem statement
Jaxz now has business expenses (cloud, software, hardware, travel, meals) that must be recorded for tax and for knowing where the money goes. Spendee-style tools are built for personal budgeting, live on someone else's servers, and let records be silently edited or deleted, which makes them weak as a business record. He needs a record that is captured the moment money is spent, can't be quietly changed later, and stays on infrastructure he controls.

## 2. Goals & success metrics
| Goal | Metric | Target |
|---|---|---|
| Capture expenses at the moment of spending | Time from opening app to saved entry | ≤ 10 s, keyboard only |
| Records are trustworthy | Ledger integrity check result | 100% pass; any tampering detected on next check |
| Zero data loss | Entries lost across restarts / crashes / reboots | 0 |
| Tax-time ready | Time to produce a year's expense export | < 1 min |

## 3. Non-goals / Out of scope
- Multiple users, sharing, teams. Exactly one Google account can ever log in.
- Bank / e-wallet sync (Maybank, TNG, GrabPay). Manual entry only in v1.
- Multi-currency conversion. MYR only (currency column kept for later).
- Invoicing, payroll, SST filing, double-entry accounting.
- Mobile apps or a PWA. Kira is a desktop-first web app (still usable in a phone browser, but not optimised for it).
- Docker. Runs as a single binary on the host (see TRD §8).

## 4. Target users & personas
- **Jaxz, solo builder** — just paid for a domain/API/client coffee; logs it from his laptop browser before forgetting.
- **Jaxz at tax time (or his accountant)** — needs a clean CSV of the year's business expenses with receipts, and confidence nothing was altered.

## 5. User stories & functional requirements

**Epic A — Access**
| ID | User story | Priority | Acceptance criteria |
|---|---|---|---|
| FR-1 | As Jaxz, I want to sign in with Google so that I don't manage a password | Must | Google sign-in works; session persists 30 days |
| FR-2 | As the owner, I want every other Google account rejected so that nobody else can see my records | Must | Any email ≠ allowed email gets 403 and no session; backend re-checks on every API call, not just at login |

**Epic B — Recording**
| ID | User story | Priority | Acceptance criteria |
|---|---|---|---|
| FR-3 | As Jaxz, I want to add an expense (amount, date, category, payee, payment method, note) so that it's recorded immediately | Must | Saved and visible in list instantly; survives app/server restart; date defaults to today |
| FR-4 | As Jaxz, I want to correct a wrong entry so that my totals are right | Must | Correction creates a new version; original stays visible in the entry's history; nothing is overwritten |
| FR-5 | As Jaxz, I want to void an entry so that mistakes don't count | Must | Voided entry excluded from totals, still visible (struck through) with reason |
| FR-6 | As Jaxz, I want to attach a receipt (image/PDF) so that I have proof for tax | Should | Upload or drag-and-drop; receipt viewable from entry; receipt fingerprint recorded in the ledger; dashboard counts missing receipts |
| FR-7 | As Jaxz, I want to manage categories so that they match my business | Should | Add/rename/archive; seeded with defaults (Cloud & hosting, Software & subscriptions, Hardware, Internet & phone, Travel & transport, Meals & entertainment, Professional fees, Marketing, Office, Other) |
| FR-8 | As Jaxz, I want recurring entries (e.g. monthly SaaS) so that I don't retype them | Could | Template creates a draft on due date for one-tap confirm |
| FR-9 | As Jaxz, I want to record income too so that I see net position | Could | Entry type income/expense; dashboard shows net |

**Epic C — Insight & export**
| ID | User story | Priority | Acceptance criteria |
|---|---|---|---|
| FR-10 | As Jaxz, I want a dashboard of this month so that I know where money went | Must | Month total, by-category breakdown, 6-month trend, recent 5 entries |
| FR-11 | As Jaxz, I want to filter/search entries so that I can find one fast | Must | Filter by month, category, method; text search on payee/note |
| FR-12 | As Jaxz, I want to export a date range to CSV so that I can file tax / send to accountant | Must | CSV includes all current entries, voids flagged, receipt filenames |
| FR-13 | As Jaxz, I want to see the ledger is intact so that I trust the data | Must | "Ledger verified ✓ / ✗ broken at entry #N" shown in Settings and on startup |
| FR-14 | As Jaxz, I want monthly budgets per category | Could | Progress bar per category; warning at 80% |

## 6. User flow
1. Open Kira in the browser → already signed in (or one-click Google sign-in).
2. Dashboard shows this month's total, categories, trend, recent entries. Press **N** or click **Add expense**.
3. **Entries page quick-add bar (the most important interaction):** date → amount → category → payee → method → note, all keyboard (Tab between fields, Enter saves).
4. Entry appears at the top of the table instantly; totals update.
5. Made a mistake → open entry → **Correct** or **Void** → history shows both versions.
6. Tax time → Settings → Export → pick year → CSV + receipts zip.

## 7. Non-functional requirements
| ID | Category | Requirement | Rationale |
|---|---|---|---|
| NFR-1 | Security | Only one allow-listed Google account (email + Google user ID) can access any data or API | Private financial records |
| NFR-2 | Durability | An entry is reported "saved" only after it is flushed to disk | "Real-time" and no loss on crash/power cut |
| NFR-3 | Integrity | Ledger is append-only; any edit/delete made outside the app (e.g. opening the DB file in a SQL tool) is detected | Business-record trustworthiness |
| NFR-4 | Backup | Automatic off-machine backup at least daily; restore tested once | Laptop death ≠ data death |
| NFR-5 | Retention | Records and receipts kept ≥ 7 years | LHDN requires business records to be kept 7 years |
| NFR-6 | Performance | Save and list responses < 200 ms on the host | Logging must feel instant |
| NFR-7 | Usability | Desktop-first web app (≥ 1024 px); full add flow doable by keyboard alone; layout still readable in a phone browser | Logging happens at the laptop |
| NFR-8 | Privacy | No third-party analytics; data never leaves Jaxz's machine except his own backup target | Personal financial data |

> Compliance note: this is a personal record-keeping tool that never holds or moves funds, so no BNM/SC licensing applies. The only regulatory hook is LHDN record-keeping (NFR-5).

## 8. Assumptions & open questions
**Assumptions**
- > ⚠️ Assumption: Jaxz runs it on a machine he owns (home PC/laptop/mini PC) and reaches it from his phone over Tailscale. If he wants it reachable from anywhere without Tailscale, it moves to a small VPS.
- > ⚠️ Assumption: "non-modifiable outside the app" means **tamper-evident**, not tamper-proof. Anyone with full access to the machine can still delete the file; the system guarantees they can't change it *without being detected*, and backups cover deletion.
- All amounts in MYR.

**Open questions**
| Question | Owner | Decide by |
|---|---|---|
| Host on own machine + Tailscale, or VPS + domain? | Jaxz | Before deploying (affects Google OAuth redirect URL) |
| Backup target: Cloudflare R2 / S3, or Google Drive folder? | Jaxz | Before first real entry |
| Encrypt DB at rest (SQLCipher) in v1 or later? | Jaxz | v1.1 |

## 9. Milestones
| Milestone | Scope | Target |
|---|---|---|
| M1 — Locked door | FR-1, FR-2 | Day 1 |
| M2 — Ledger works | FR-3, FR-4, FR-5, FR-13, NFR-2, NFR-3 | Day 1–2 |
| M3 — Usable daily | FR-10, FR-11, FR-7 | Day 2–3 |
| M4 — Tax ready | FR-6, FR-12, NFR-4 backups | Day 3–4 |
| Stretch | FR-8, FR-9, FR-14 | Later |

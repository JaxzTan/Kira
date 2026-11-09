// Wire contract. Mirrors docs/api-contract.md and internal/api/dto.go.
// Only the main session edits this file; request changes in PLAN.md "Cross-lane requests".
// Money is always integer sen (number, never fractional). Format only at the edge.

export type Kind = 'create' | 'amend' | 'void'
export type Direction = 'expense' | 'income'
export type Status = 'active' | 'amended' | 'voided'
export type Method = 'card' | 'cash' | 'bank_transfer' | 'tng_ewallet' | 'grabpay' | 'other'

export const METHODS: ReadonlyArray<{ value: Method; label: string }> = [
  { value: 'card', label: 'Card' },
  { value: 'cash', label: 'Cash' },
  { value: 'bank_transfer', label: 'Bank transfer' },
  { value: 'tng_ewallet', label: 'TNG eWallet' },
  { value: 'grabpay', label: 'GrabPay' },
  { value: 'other', label: 'Other' },
]

export const LIMITS = {
  maxAmountSen: 1_000_000_000, // exclusive
  maxPayeeLen: 120,
  maxNoteLen: 500,
  maxVoidReasonLen: 300,
  maxCategoryLen: 60,
  maxReceiptBytes: 10 * 1024 * 1024,
  dateConfirmDays: 365,
} as const

export type ErrorCode =
  | 'bad_request'
  | 'unauthorized'
  | 'forbidden'
  | 'not_found'
  | 'conflict'
  | 'payload_too_large'
  | 'unsupported_media_type'
  | 'validation'
  | 'date_confirm_required'
  | 'ledger_broken'
  | 'rate_limited'
  | 'internal'

export interface ErrorResponse {
  error: string
  code: ErrorCode
  fields: Record<string, string> | null
}

export interface Entry {
  id: number
  version_id: number
  direction: Direction
  occurred_on: string
  amount_sen: number
  currency: string
  category_id: number
  category_name: string
  payee: string
  method: Method
  note: string | null
  receipt_sha256: string | null
  status: Status
  amend_count: number
  void_reason: string | null
  created_at: string
  updated_at: string
  hash: string
}

export interface LedgerRow {
  id: number
  kind: Kind
  ref_id: number | null
  direction: Direction
  occurred_on: string | null
  amount_sen: number | null
  currency: string
  category_id: number | null
  payee: string | null
  method: Method | null
  note: string | null
  void_reason: string | null
  receipt_sha256: string | null
  created_at: string
  prev_hash: string
  hash: string
}

export interface AppendResult extends LedgerRow {
  entry: Entry
}

export interface EntryInput {
  direction?: Direction
  occurred_on: string
  amount_sen: number
  category_id: number
  payee: string
  method: Method
  note: string | null
  receipt_sha256: string | null
  confirm_date?: boolean
}

export interface VoidInput {
  reason: string
}

export interface EntryFilter {
  from?: string
  to?: string
  category?: number
  method?: Method
  q?: string
  status?: 'all' | 'active' | 'voided'
}

export interface EntryList {
  entries: Entry[]
  total_sen: number
  count: number
  voided_count: number
}

export interface EntryHistory {
  entry: Entry
  rows: LedgerRow[]
}

export interface Me {
  email: string
  name: string | null
  picture: string | null
}

export interface Receipt {
  sha256: string
  ext: 'jpg' | 'png' | 'pdf'
  content_type: string
  size: number
}

export interface Category {
  id: number
  name: string
  color: string | null
  icon: string | null
  archived_at: string | null
}

export interface CategoryList {
  categories: Category[]
}

export interface CategoryCreate {
  name: string
  color?: string | null
}

export interface CategoryPatch {
  name?: string
  archived?: boolean
}

export interface CategoryTotal {
  category_id: number
  name: string
  color: string | null
  total_sen: number
  pct: number
}

export interface MonthTotal {
  month: string
  total_sen: number
}

export interface Summary {
  month: string
  total_sen: number
  count: number
  voided_count: number
  prev_month_total_sen: number
  change_pct: number | null
  missing_receipts: number
  by_category: CategoryTotal[]
  trend: MonthTotal[]
  recent: Entry[]
}

export interface VerifyResult {
  ok: boolean
  entries: number
  head_id: number
  head_hash: string
  broken_at: number | null
  reason: string | null
  checked_at: string
  acknowledged: boolean
  last_backup_at: string | null
}

export type ExportFormat = 'csv' | 'zip'

/** The typed client interface. Real (fetch) and mock (VITE_MOCK_API=1) both implement it. */
export interface KiraApi {
  me(): Promise<Me>
  logout(): Promise<void>
  listEntries(filter?: EntryFilter): Promise<EntryList>
  getEntry(id: number): Promise<Entry>
  createEntry(input: EntryInput): Promise<AppendResult>
  history(id: number): Promise<EntryHistory>
  amendEntry(id: number, input: EntryInput): Promise<AppendResult>
  voidEntry(id: number, input: VoidInput): Promise<AppendResult>
  uploadReceipt(file: File): Promise<Receipt>
  receiptUrl(sha256: string): string
  listCategories(includeArchived?: boolean): Promise<CategoryList>
  createCategory(input: CategoryCreate): Promise<Category>
  patchCategory(id: number, input: CategoryPatch): Promise<Category>
  summary(month?: string): Promise<Summary>
  exportUrl(from: string, to: string, format: ExportFormat): string
  verify(): Promise<VerifyResult>
  acknowledge(): Promise<VerifyResult>
}

import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, isApiError } from '../api'
import type { Category, Direction } from '../api/types'
import { LIMITS } from '../api/types'
import { useMe } from '../components/auth-context'
import { IconShield, IconShieldX } from '../components/icons'
import { useLedger } from '../components/ledger-context'
import { Button, Card, ErrorNote, Field, PageTitle, Spinner, inputClass } from '../components/ui'
import { relativeTime, shortDateTime, shortHash } from '../lib/dates'

function CategoryRow({ cat, onChanged, onError }: { cat: Category; onChanged: () => void; onError: (m: string | null) => void }) {
  const [editing, setEditing] = useState(false)
  const [name, setName] = useState(cat.name)
  const archived = Boolean(cat.archived_at)

  async function patch(body: { name?: string; archived?: boolean }) {
    onError(null)
    try {
      await api.patchCategory(cat.id, body)
      setEditing(false)
      onChanged()
    } catch (e) {
      onError(isApiError(e) ? (e.fields?.name ?? e.message) : 'Could not update category')
    }
  }

  return (
    <li className="flex flex-wrap items-center gap-3 border-b border-border py-2.5 last:border-b-0">
      <span aria-hidden="true" className="h-3 w-3 shrink-0 rounded-full" style={{ background: cat.color ?? 'var(--color-cat-5)' }} />
      {editing ? (
        <form className="flex flex-1 flex-wrap items-center gap-2" onSubmit={(e) => { e.preventDefault(); void patch({ name }) }}>
          <label htmlFor={`cat-name-${cat.id}`} className="sr-only-text">Category name</label>
          <input id={`cat-name-${cat.id}`} autoFocus value={name} maxLength={LIMITS.maxCategoryLen} onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Escape') { setEditing(false); setName(cat.name) } }} className={`${inputClass} max-w-[320px] flex-1`} />
          <Button type="submit" className="h-9">Save</Button>
          <Button variant="outline" className="h-9" onClick={() => { setEditing(false); setName(cat.name) }}>Cancel</Button>
        </form>
      ) : (
        <>
          <span className={`min-w-0 flex-1 text-[15px] ${archived ? 'text-text-muted' : ''}`}>
            {cat.name}{archived && <span className="ml-2 rounded-full bg-track px-2 py-0.5 text-xs text-text-muted">Archived</span>}
          </span>
          <button type="button" onClick={() => setEditing(true)} aria-label={`Rename ${cat.name}`} className="h-control min-w-control cursor-pointer px-2 text-sm font-semibold text-accent hover:underline">Rename</button>
          <button type="button" onClick={() => void patch({ archived: !archived })} aria-label={`${archived ? 'Unarchive' : 'Archive'} ${cat.name}`} className="h-control min-w-control cursor-pointer px-2 text-sm font-semibold text-text-muted hover:underline">
            {archived ? 'Unarchive' : 'Archive'}
          </button>
        </>
      )}
    </li>
  )
}

export default function Settings() {
  const me = useMe()
  const navigate = useNavigate()
  const { verify, refresh } = useLedger()
  const [cats, setCats] = useState<Category[]>([])
  const [newName, setNewName] = useState('')
  const [newDir, setNewDir] = useState<Direction>('expense')
  const [error, setError] = useState<string | null>(null)
  const [verifying, setVerifying] = useState(false)

  const [catsLoaded, setCatsLoaded] = useState(false)
  const load = useCallback(() => {
    api.listCategories(true).then(
      (r) => { setCats(r.categories); setCatsLoaded(true); setError(null) },
      (e) => { setCatsLoaded(true); setError(isApiError(e) ? e.message : 'Could not load categories') },
    )
  }, [])
  useEffect(load, [load])

  async function add(e: FormEvent) {
    e.preventDefault()
    if (!newName.trim()) return
    setError(null)
    try {
      await api.createCategory({ name: newName.trim(), direction: newDir })
      setNewName('')
      load()
    } catch (err) {
      setError(isApiError(err) ? (err.fields?.name ?? err.message) : 'Could not add category')
    }
  }
  async function verifyNow() {
    setVerifying(true)
    await refresh()
    setVerifying(false)
  }
  async function signOut() {
    try { await api.logout() } finally { navigate('/login') }
  }

  return (
    <>
      <PageTitle>Settings</PageTitle>
      <div className="grid grid-cols-[repeat(auto-fit,minmax(360px,1fr))] items-start gap-5">
        <Card>
          <h2 className="mb-3 font-display text-base font-semibold">Categories</h2>
          <ErrorNote message={error} onRetry={load} />
          {!catsLoaded && <Spinner />}
          {catsLoaded && cats.length === 0 && !error && <p className="mb-4 text-sm text-text-muted">No categories yet. Add one below.</p>}
          {(['expense', 'income'] as const).map((d) => (
            <section key={d} aria-labelledby={`cats-${d}`} className="mb-4">
              <h3 id={`cats-${d}`} className="mb-1 text-xs font-semibold uppercase tracking-wide text-text-muted">{d === 'expense' ? 'Expense categories' : 'Income categories'}</h3>
              <ul className="m-0 list-none p-0">
                {cats.filter((c) => c.direction === d).map((c) => <CategoryRow key={`${c.id}-${c.name}-${c.archived_at}`} cat={c} onChanged={load} onError={setError} />)}
              </ul>
            </section>
          ))}
          <form onSubmit={(e) => void add(e)} className="flex flex-wrap items-end gap-3">
            <Field label="New category" htmlFor="new-cat" className="flex-1">
              <input id="new-cat" value={newName} maxLength={LIMITS.maxCategoryLen} onChange={(e) => setNewName(e.target.value)} className={inputClass} />
            </Field>
            <Field label="Type" htmlFor="new-cat-dir" className="w-[140px]">
              <select id="new-cat-dir" value={newDir} onChange={(e) => setNewDir(e.target.value as Direction)} className={inputClass}>
                <option value="expense">Expense</option>
                <option value="income">Income</option>
              </select>
            </Field>
            <Button type="submit">Add</Button>
          </form>
          <p className="mt-3 text-xs text-text-muted">Renaming or archiving never changes past ledger rows. A category's type is fixed when it is created.</p>
        </Card>
        <div className="flex flex-col gap-5">
          <Card>
            <h2 className="mb-3 font-display text-base font-semibold">Ledger status</h2>
            {verify ? (
              <>
                <p className={`mb-3 flex items-center gap-2 text-[15px] font-semibold ${verify.ok ? 'text-accent' : 'text-danger'}`}>
                  {verify.ok ? <><IconShield /> Verified</> : <><IconShieldX /> Broken at entry #{verify.broken_at}{verify.reason ? ` (${verify.reason})` : ''}</>}
                </p>
                <dl className="mb-4 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
                  <dt className="text-text-muted">Entries</dt><dd className="m-0">{verify.entries}</dd>
                  <dt className="text-text-muted">Head</dt><dd className="m-0 font-mono">#{verify.head_id} · {shortHash(verify.head_hash)}</dd>
                  <dt className="text-text-muted">Checked</dt><dd className="m-0">{shortDateTime(verify.checked_at)}</dd>
                  <dt className="text-text-muted">Last backup</dt><dd className="m-0">{relativeTime(verify.last_backup_at)}</dd>
                </dl>
              </>
            ) : <p className="mb-4 text-sm text-text-muted">Not checked yet.</p>}
            <Button variant="outline" onClick={() => void verifyNow()} disabled={verifying}>{verifying ? 'Verifying…' : 'Verify now'}</Button>
          </Card>
          <Card>
            <h2 className="mb-3 font-display text-base font-semibold">Account</h2>
            <p className="mb-4 text-sm">Signed in as <strong>{me?.email}</strong></p>
            <Button variant="outline" onClick={() => void signOut()}>Sign out</Button>
          </Card>
        </div>
      </div>
    </>
  )
}

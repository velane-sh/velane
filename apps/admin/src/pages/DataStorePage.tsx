import { useEffect, useRef, useState } from 'react'
import { ChevronLeft, ChevronRight, Copy, Database, Eye, EyeOff, Search, Trash2, X } from 'lucide-react'
import { api } from '../lib/api'
import type { KVEntry, KVEntryMeta, KVNamespace } from '../types'
import Select from '../components/Select'
import { Toast, useToast } from '../components/Toast'

const PAGE_SIZE = 25
const MAX_RENDERED_VALUE_BYTES = 512 * 1024
const ONBOARDING_SNIPPET = `import { store } from '@velane/store'

const cursor = { updatedAt: new Date().toISOString() }
await store.set('last_sync_cursor', cursor, { ttl: 86400 })
const savedCursor = await store.get('last_sync_cursor')`

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${Number((bytes / 1024).toFixed(1))} KB`
  return `${Number((bytes / (1024 * 1024)).toFixed(1))} MB`
}

function formatExpiry(expiresAt?: string | null) {
  if (!expiresAt) return 'Never'
  const milliseconds = new Date(expiresAt).getTime() - Date.now()
  if (Number.isNaN(milliseconds)) return 'Unknown'
  if (milliseconds <= 0) return 'Expired'

  const seconds = Math.ceil(milliseconds / 1000)
  if (seconds < 60) return `in ${seconds}s`
  const minutes = Math.ceil(seconds / 60)
  if (minutes < 60) return `in ${minutes}m`
  const hours = Math.ceil(minutes / 60)
  if (hours < 48) return `in ${hours}h`
  return `in ${Math.ceil(hours / 24)}d`
}

function formatUpdated(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return 'Unknown'
  return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

function formatFullDate(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return 'Unknown'
  return date.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}

function renderValue(rawJSON: string) {
  // The raw response preserves valid JSON integers that JavaScript would otherwise round.
  const json = rawJSON
  const encoded = new TextEncoder().encode(json)
  if (encoded.byteLength <= MAX_RENDERED_VALUE_BYTES) return { text: json, truncated: false }

  return {
    text: `${new TextDecoder().decode(encoded.slice(0, MAX_RENDERED_VALUE_BYTES))}\n… value truncated for display`,
    truncated: true,
  }
}

function NamespaceBadge({ namespace }: { namespace: string }) {
  return (
    <span className="inline-flex items-center rounded-full bg-gray-100 px-2.5 py-0.5 text-xs font-medium text-gray-800">
      {namespace}
    </span>
  )
}

export default function DataStorePage() {
  const { toast, showToast, dismissToast } = useToast()
  const [items, setItems] = useState<KVEntryMeta[]>([])
  const [namespaces, setNamespaces] = useState<KVNamespace[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [namespace, setNamespace] = useState('')
  const [search, setSearch] = useState('')
  const [prefix, setPrefix] = useState('')
  const [offset, setOffset] = useState(0)
  const [total, setTotal] = useState(0)
  const [hasNext, setHasNext] = useState(false)
  const [selected, setSelected] = useState<KVEntry | null>(null)
  const [revealing, setRevealing] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<KVEntryMeta | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [canReveal, setCanReveal] = useState(true)
  const revealRequest = useRef(0)

  // Any operation that changes the active result set or deletes a key must invalidate
  // in-flight reveals before dropping plaintext from state.
  function clearRevealedValue() {
    revealRequest.current += 1
    setSelected(null)
    setRevealing(false)
  }

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setPrefix(search.trim())
      setOffset(0)
      clearRevealedValue()
    }, 250)

    return () => window.clearTimeout(timer)
  }, [search])

  useEffect(() => {
    let cancelled = false

    async function load() {
      setLoading(true)
      setError('')
      try {
        const [entryList, namespaceList] = await Promise.all([
          api.listKVEntries({ namespace: namespace || undefined, prefix: prefix || undefined, limit: PAGE_SIZE + 1, offset }),
          api.listKVNamespaces(),
        ])
        if (cancelled) return
        const pageItems = entryList.items ?? []
        setItems(pageItems.slice(0, PAGE_SIZE))
        setHasNext(pageItems.length > PAGE_SIZE)
        setTotal(entryList.total ?? 0)
        setNamespaces(namespaceList ?? [])
      } catch (err) {
        if (!cancelled) setError(String(err))
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    load()
    return () => {
      cancelled = true
    }
  }, [namespace, offset, prefix])

  useEffect(() => {
    let cancelled = false

    async function loadRole() {
      if (localStorage.getItem('apiKey')) return

      try {
        const org = await api.getActiveOrg()
        if (!cancelled) setCanReveal(org.role === 'admin')
      } catch {
        // API-key scopes are intentionally unavailable to the browser. Keep reveal available and surface a 403.
      }
    }

    loadRole()
    return () => {
      cancelled = true
    }
  }, [])

  function changeNamespace(nextNamespace: string) {
    setNamespace(nextNamespace)
    setOffset(0)
    clearRevealedValue()
  }

  async function reveal(item: KVEntryMeta) {
    const requestID = revealRequest.current + 1
    revealRequest.current = requestID
    setRevealing(true)
    try {
      const entry = await api.revealKVEntry(item.namespace, item.key)
      if (revealRequest.current !== requestID) return
      setSelected(entry)
      showToast('Value revealed.')
    } catch (err) {
      if (revealRequest.current === requestID) showToast(`Failed to reveal value: ${String(err)}`, 'error')
    } finally {
      if (revealRequest.current === requestID) setRevealing(false)
    }
  }

  function requestDelete(item: KVEntryMeta) {
    setPendingDelete(item)
  }

  async function confirmDelete() {
    if (!pendingDelete) return
    const item = pendingDelete
    const itemsBeforeDelete = items
    // Clear unconditionally: the matching reveal may still be pending with no selected entry yet.
    clearRevealedValue()
    setDeleting(true)
    setItems(current => current.filter(entry => entry.id !== item.id))
    setTotal(current => Math.max(0, current - 1))
    setPendingDelete(null)

    try {
      await api.deleteKVEntry(item.namespace, item.key)
      showToast('Key deleted.')
    } catch (err) {
      setItems(itemsBeforeDelete)
      setTotal(current => current + 1)
      showToast(`Failed to delete key: ${String(err)}`, 'error')
    } finally {
      setDeleting(false)
    }
  }

  const renderedValue = selected ? renderValue(selected.value_raw) : null
  const page = Math.floor(offset / PAGE_SIZE) + 1
  const hasFilters = Boolean(namespace || prefix)

  return (
    <div>
      <div className="mb-6">
        <h1 className="text-2xl font-bold text-gray-900">Data Store</h1>
        <p className="mt-1 text-sm text-gray-500">
          Key-value storage shared across workflow invocations. Values are hidden in this list and fetched only when you reveal a key.
        </p>
      </div>

      {error && <div className="mb-6 rounded-md bg-red-50 p-3 text-sm text-red-700" role="alert">{error}</div>}

      <div className="mb-4 flex items-center gap-3">
        <div className="relative flex-1">
          <Search size={16} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
          <input
            type="search"
            aria-label="Search keys by prefix"
            placeholder="Search by key prefix..."
            value={search}
            onChange={event => {
              setSearch(event.target.value)
              clearRevealedValue()
            }}
            disabled={loading}
            className="h-9 w-full rounded-lg border border-gray-300 py-0 pl-9 pr-3 text-sm text-gray-900 placeholder-gray-400 outline-none transition-colors hover:border-gray-400 focus:border-gray-500 focus:ring-2 focus:ring-gray-200 disabled:bg-gray-50"
          />
        </div>
        <Select
          aria-label="Filter by namespace"
          value={namespace}
          onChange={event => changeNamespace(event.target.value)}
          disabled={loading}
          containerClassName="w-52"
        >
          <option value="">All namespaces</option>
          {namespaces.map(item => <option key={item.namespace} value={item.namespace}>{item.namespace}</option>)}
        </Select>
      </div>

      {loading ? (
        <div className="rounded-lg border border-gray-200 bg-white shadow-sm">
          <p className="p-6 text-sm text-gray-400">Loading...</p>
        </div>
      ) : items.length === 0 ? (
        hasFilters ? (
          <div className="rounded-xl border border-dashed border-gray-300 bg-gray-50 px-6 py-8 text-center">
            <h3 className="text-sm font-semibold text-gray-900">No matching keys</h3>
            <p className="mt-1 text-sm text-gray-500">Adjust the key prefix or namespace filter to view stored keys.</p>
          </div>
        ) : (
          <div className="rounded-xl border border-dashed border-gray-300 bg-gray-50 px-6 py-8">
            <div className="mx-auto max-w-xl">
              <div className="flex flex-col items-center text-center">
                <div className="flex h-10 w-10 items-center justify-center rounded-full bg-gray-100 text-gray-400"><Database size={20} /></div>
                <h3 className="mt-3 text-sm font-semibold text-gray-900">No keys stored yet</h3>
                <p className="mt-1 text-sm text-gray-500">Write from any workflow and the key appears here. Keys live in the <code className="rounded bg-gray-100 px-1">default</code> namespace unless you pass one.</p>
              </div>
              <div className="mt-5">
                <p className="mb-2 text-xs font-medium uppercase tracking-wide text-gray-500">Bun (TypeScript)</p>
                <div className="relative rounded-xl bg-gray-950 px-4 py-3">
                  <pre className="overflow-auto pr-10 font-mono text-xs leading-relaxed text-gray-100">{ONBOARDING_SNIPPET}</pre>
                  <button
                    type="button"
                    onClick={() => navigator.clipboard.writeText(ONBOARDING_SNIPPET).then(() => showToast('Snippet copied.')).catch(() => showToast('Failed to copy snippet.', 'error'))}
                    className="absolute right-2 top-2 rounded bg-gray-950 p-1 text-gray-400 hover:text-gray-200"
                    title="Copy to clipboard"
                    aria-label="Copy setup snippet to clipboard"
                  >
                    <Copy size={16} />
                  </button>
                </div>
                <p className="mt-2 text-xs text-gray-500"><code className="rounded bg-gray-100 px-1">ttl</code> is in seconds. Omit it and the key never expires.</p>
              </div>
            </div>
          </div>
        )
      ) : (
        <div className="flex gap-6">
          <div className="min-w-0 flex-1 overflow-hidden rounded-lg border border-gray-200 bg-white shadow-sm">
            <div className="overflow-x-auto">
              <table className="w-full table-fixed text-sm">
                <colgroup>
                  <col className="w-[30%]" />
                  <col className="w-[18%]" />
                  <col className="w-[16%]" />
                  <col className="w-[13%]" />
                  <col className="w-[13%]" />
                  <col className="w-[10%]" />
                </colgroup>
                <thead className="border-b border-gray-200 bg-gray-50 text-xs font-medium text-gray-500">
                  <tr>
                    <th className="whitespace-nowrap px-4 py-3 text-left">Key</th>
                    <th className="whitespace-nowrap px-4 py-3 text-left">Namespace</th>
                    <th className="whitespace-nowrap px-4 py-3 text-left">Value</th>
                    <th className="whitespace-nowrap px-4 py-3 text-left">Expires</th>
                    <th className="whitespace-nowrap px-4 py-3 text-left">Updated</th>
                    <th className="px-4 py-3 text-left"><span className="sr-only">Actions</span></th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {items.map(item => (
                    <tr key={item.id} className="hover:bg-gray-50">
                      <td className="px-4 py-3"><code className="block truncate text-xs font-mono text-gray-800" title={item.key}>{item.key}</code></td>
                      <td className="px-4 py-3"><span className="block truncate"><NamespaceBadge namespace={item.namespace} /></span></td>
                      <td className="px-4 py-3"><span className="block truncate font-mono text-xs tracking-widest text-gray-400">••••••••</span></td>
                      <td className="px-4 py-3 text-gray-400"><span className="block truncate" title={item.expires_at ?? 'Never'}>{formatExpiry(item.expires_at)}</span></td>
                      <td className="px-4 py-3 text-gray-400"><span className="block truncate" title={formatFullDate(item.updated_at)}>{formatUpdated(item.updated_at)}</span></td>
                      <td className="whitespace-nowrap px-4 py-3">
                        <div className="flex items-center gap-2">
                          {canReveal && (
                            <button type="button" onClick={() => reveal(item)} disabled={revealing} className="text-gray-400 hover:text-gray-900 disabled:opacity-50" title="Reveal value" aria-label={`Reveal value for ${item.key}`}>
                              <Eye size={14} />
                            </button>
                          )}
                          <button type="button" onClick={() => requestDelete(item)} disabled={deleting} className="text-gray-400 hover:text-red-600 disabled:opacity-50" title="Delete" aria-label={`Delete ${item.key}`}>
                            <Trash2 size={14} />
                          </button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="flex items-center justify-between border-t border-gray-200 px-4 py-3 text-sm">
              <p className="text-gray-500">Page {page}{total > 0 ? ` of ${Math.max(1, Math.ceil(total / PAGE_SIZE))}` : ''}</p>
              <div className="flex gap-2">
                <button type="button" onClick={() => { setOffset(current => Math.max(0, current - PAGE_SIZE)); clearRevealedValue() }} disabled={offset === 0} className="flex items-center gap-1 rounded-md border border-gray-200 px-3 py-1 text-gray-700 hover:bg-gray-50 disabled:opacity-50"><ChevronLeft size={14} />Previous</button>
                <button type="button" onClick={() => { setOffset(current => current + PAGE_SIZE); clearRevealedValue() }} disabled={!hasNext} className="flex items-center gap-1 rounded-md border border-gray-200 px-3 py-1 text-gray-700 hover:bg-gray-50 disabled:opacity-50">Next<ChevronRight size={14} /></button>
              </div>
            </div>
          </div>

          {selected && renderedValue && (
            <aside className="w-96 shrink-0" aria-label="Key details">
              <div className="rounded-lg border border-gray-200 bg-white shadow-sm">
                <div className="flex items-start justify-between gap-3 border-b border-gray-200 px-4 py-3">
                  <div className="min-w-0"><code className="block truncate text-xs font-mono text-gray-900">{selected.key}</code><div className="mt-1.5"><NamespaceBadge namespace={selected.namespace} /></div></div>
                  <button type="button" onClick={clearRevealedValue} className="rounded-md p-1 text-gray-400 hover:bg-gray-100" title="Close" aria-label="Close details"><X size={16} /></button>
                </div>
                <dl className="grid grid-cols-2 gap-x-4 gap-y-3 border-b border-gray-200 px-4 py-3">
                  <div><dt className="text-xs font-medium uppercase tracking-wide text-gray-500">Size</dt><dd className="mt-0.5 text-sm tabular-nums text-gray-700">{formatBytes(selected.size_bytes)}</dd></div>
                  <div><dt className="text-xs font-medium uppercase tracking-wide text-gray-500">Expires</dt><dd className="mt-0.5 text-sm text-gray-400">{formatExpiry(selected.expires_at)}</dd></div>
                  <div><dt className="text-xs font-medium uppercase tracking-wide text-gray-500">Updated</dt><dd className="mt-0.5 text-sm text-gray-700">{formatFullDate(selected.updated_at)}</dd></div>
                  <div><dt className="text-xs font-medium uppercase tracking-wide text-gray-500">Created</dt><dd className="mt-0.5 text-sm text-gray-700">{formatFullDate(selected.created_at)}</dd></div>
                </dl>
                <div className="px-4 py-3">
                  <div className="mb-2 flex items-center justify-between"><p className="text-xs font-medium uppercase tracking-wide text-gray-500">Value</p><button type="button" onClick={clearRevealedValue} className="flex items-center gap-1.5 text-xs text-gray-500 hover:text-gray-900"><EyeOff size={14} />Hide</button></div>
                  <div className="relative rounded-xl bg-gray-950 px-4 py-3">
                    <pre className="max-h-80 overflow-auto pr-10 font-mono text-xs leading-relaxed text-gray-100">{renderedValue.text}</pre>
                    <button type="button" onClick={() => navigator.clipboard.writeText(renderedValue.text).then(() => showToast('Value copied.')).catch(() => showToast('Failed to copy value.', 'error'))} className="absolute right-2 top-2 rounded bg-gray-950 p-1 text-gray-400 hover:text-gray-200" title="Copy to clipboard" aria-label="Copy value to clipboard"><Copy size={16} /></button>
                  </div>
                  {renderedValue.truncated && <p className="mt-2 text-xs text-gray-500">Only the first 512 KB is shown and available to copy.</p>}
                </div>
                <div className="flex items-center justify-end gap-2 border-t border-gray-200 px-4 py-3">
                  <button type="button" onClick={() => requestDelete(selected)} className="flex items-center gap-1.5 rounded-md border border-gray-200 px-2.5 py-1 text-xs text-gray-700 hover:border-red-200 hover:bg-red-50 hover:text-red-600"><Trash2 size={14} />Delete key</button>
                </div>
              </div>
            </aside>
          )}
        </div>
      )}

      {pendingDelete && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30" role="dialog" aria-modal="true" aria-labelledby="datastore-delete-title">
          <div className="w-full max-w-md rounded-xl border border-gray-200 bg-white p-6 shadow-xl">
            <h2 id="datastore-delete-title" className="mb-2 text-lg font-semibold text-gray-900">Delete key</h2>
            <p className="text-sm text-gray-500">This removes the key and its value for every workflow in this org. It cannot be undone.</p>
            <dl className="mt-4 space-y-2 rounded-lg border border-gray-200 bg-gray-50 p-3">
              <div className="flex items-center justify-between gap-3"><dt className="text-xs font-medium uppercase tracking-wide text-gray-500">Key</dt><dd className="truncate text-xs font-mono text-gray-900">{pendingDelete.key}</dd></div>
              <div className="flex items-center justify-between gap-3"><dt className="text-xs font-medium uppercase tracking-wide text-gray-500">Namespace</dt><dd><NamespaceBadge namespace={pendingDelete.namespace} /></dd></div>
              <div className="flex items-center justify-between gap-3"><dt className="text-xs font-medium uppercase tracking-wide text-gray-500">Size</dt><dd className="text-xs tabular-nums text-gray-700">{formatBytes(pendingDelete.size_bytes)}</dd></div>
            </dl>
            <div className="mt-6 flex justify-end gap-3">
              <button type="button" onClick={() => setPendingDelete(null)} disabled={deleting} className="rounded-md border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50">Cancel</button>
              <button type="button" onClick={confirmDelete} disabled={deleting} className="flex items-center gap-1.5 rounded-lg bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700 disabled:opacity-50"><Trash2 size={14} />{deleting ? 'Deleting...' : 'Delete key'}</button>
            </div>
          </div>
        </div>
      )}

      {toast && <Toast message={toast.message} type={toast.type} onDismiss={dismissToast} />}
    </div>
  )
}

import { useEffect, useState, type FormEvent } from 'react'
import { Trash2 } from 'lucide-react'
import { api } from '../lib/api'
import type { APIKey } from '../types'
import CopyBox from '../components/CopyBox'

const ALL_SCOPES = ['invoke', 'manage', 'admin']

export default function APIKeysPage() {
  const [keys, setKeys] = useState<APIKey[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  // Create form state
  const [name, setName] = useState('')
  const [scopes, setScopes] = useState<string[]>(['invoke'])
  const [creating, setCreating] = useState(false)
  const [newKey, setNewKey] = useState<string | null>(null)

  const load = async () => {
    try {
      const data = await api.listAPIKeys()
      setKeys(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load API keys')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  const handleCreate = async (e: FormEvent) => {
    e.preventDefault()
    setCreating(true)
    setNewKey(null)
    try {
      const created = await api.createAPIKey(name, scopes)
      setNewKey(created.key ?? null)
      setName('')
      setScopes(['invoke'])
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create API key')
    } finally {
      setCreating(false)
    }
  }

  const handleDelete = async (id: string) => {
    if (!confirm('Revoke this API key? This cannot be undone.')) return
    try {
      await api.deleteAPIKey(id)
      setKeys((ks) => ks.filter((k) => k.id !== id))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to revoke key')
    }
  }

  const toggleScope = (scope: string) => {
    setScopes((prev) =>
      prev.includes(scope) ? prev.filter((s) => s !== scope) : [...prev, scope],
    )
  }

  return (
    <div>
      <h1 className="mb-6 text-2xl font-bold text-gray-900">API Keys</h1>

      {error && (
        <div className="mb-4 rounded-md bg-red-50 p-3 text-sm text-red-700">{error}</div>
      )}

      {newKey && (
        <div className="mb-6 rounded-md border border-green-200 bg-green-50 p-4">
          <p className="mb-2 text-sm font-medium text-green-800">
            New API key created — copy it now. It will never be shown again.
          </p>
          <CopyBox value={newKey} />
        </div>
      )}

      {/* Create form */}
      <form
        onSubmit={handleCreate}
        className="mb-8 rounded-lg border border-gray-200 bg-white p-6 shadow-sm"
      >
        <h2 className="mb-4 text-base font-semibold text-gray-900">Create New Key</h2>
        <div className="flex flex-col gap-4 sm:flex-row sm:items-end">
          <div className="flex-1">
            <label className="mb-1 block text-sm font-medium text-gray-700">Name</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-gray-400 focus:outline-none"
              placeholder="e.g. CI deploy key"
            />
          </div>
          <div>
            <label className="mb-1 block text-sm font-medium text-gray-700">Scopes</label>
            <div className="flex gap-3">
              {ALL_SCOPES.map((scope) => (
                <label key={scope} className="flex items-center gap-1 text-sm text-gray-700">
                  <input
                    type="checkbox"
                    checked={scopes.includes(scope)}
                    onChange={() => toggleScope(scope)}
                    className="rounded"
                  />
                  {scope}
                </label>
              ))}
            </div>
            <p className="mt-1 text-xs text-gray-500">
              The key inherits your user groups, so it can only use integrations granted to them.
            </p>
          </div>
          <button
            type="submit"
            disabled={creating}
            className="rounded-md bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
          >
            {creating ? 'Creating...' : 'Create Key'}
          </button>
        </div>
      </form>

      {/* Keys table */}
      <div className="rounded-lg border border-gray-200 bg-white shadow-sm">
        {loading ? (
          <p className="p-4 text-sm text-gray-500">Loading...</p>
        ) : keys.length === 0 ? (
          <p className="p-4 text-sm text-gray-500">No API keys yet.</p>
        ) : (
          <table className="w-full text-sm">
            <thead className="border-b border-gray-200 bg-gray-50 text-left">
              <tr>
                <th className="px-4 py-3 font-medium text-gray-600">Name</th>
                <th className="px-4 py-3 font-medium text-gray-600">Prefix</th>
                <th className="px-4 py-3 font-medium text-gray-600">Scopes</th>
                <th className="px-4 py-3 font-medium text-gray-600">Created</th>
                <th className="px-4 py-3 font-medium text-gray-600">Last Used</th>
                <th className="px-4 py-3" />
              </tr>
            </thead>
            <tbody>
              {keys.map((k) => (
                <tr key={k.id} className="border-b border-gray-100 last:border-0">
                  <td className="px-4 py-3 font-medium text-gray-900">{k.name}</td>
                  <td className="px-4 py-3 font-mono text-gray-600">vl_{k.key_prefix}…</td>
                  <td className="px-4 py-3 text-gray-600">{k.scopes.join(', ')}</td>
                  <td className="px-4 py-3 text-gray-500">
                    {new Date(k.created_at).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-3 text-gray-500">
                    {k.last_used_at ? new Date(k.last_used_at).toLocaleDateString() : '—'}
                  </td>
                  <td className="px-4 py-3">
                    <button
                      onClick={() => handleDelete(k.id)}
                      className="text-red-500 hover:text-red-700"
                      title="Revoke key"
                    >
                      <Trash2 size={16} />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}

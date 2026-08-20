import { Plus, Search } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import SandboxStatusBadge, { isTransitionalSandboxState } from '../components/SandboxStatusBadge'
import Select from '../components/Select'
import { api } from '../lib/api'
import type { Sandbox } from '../types'

export default function SandboxListPage() {
  const [sandboxes, setSandboxes] = useState<Sandbox[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState('')

  const load = useCallback(async () => {
    if (document.hidden) return
    try {
      setError('')
      const response = await api.listSandboxes({ limit: 100 })
      setSandboxes(response.items ?? [])
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load sandboxes.')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  useEffect(() => {
    if (!sandboxes.some(sandbox => isTransitionalSandboxState(sandbox.observed_state))) return

    let timer: number | undefined
    function schedule() {
      if (!document.hidden) {
        timer = window.setTimeout(() => {
          load()
          schedule()
        }, 5000)
      }
    }
    function onVisibilityChange() {
      if (!document.hidden) load()
    }

    schedule()
    document.addEventListener('visibilitychange', onVisibilityChange)
    return () => {
      if (timer) window.clearTimeout(timer)
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
  }, [load, sandboxes])

  const filteredSandboxes = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase()
    return sandboxes.filter(sandbox =>
      (!normalizedQuery || sandbox.name.toLowerCase().includes(normalizedQuery) || sandbox.id.toLowerCase().includes(normalizedQuery))
      && (!status || sandbox.observed_state === status),
    )
  }, [query, sandboxes, status])

  const counts = {
    total: sandboxes.length,
    running: sandboxes.filter(item => item.observed_state === 'running').length,
    stopped: sandboxes.filter(item => item.observed_state === 'stopped').length,
    attention: sandboxes.filter(item => item.observed_state === 'failed').length,
  }

  return (
    <div className="mx-auto max-w-6xl">
      <header className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div>
          <p className="text-xs font-semibold uppercase tracking-[0.09em] text-gray-500">Agent infrastructure</p>
          <h1 className="mt-2 text-2xl font-bold tracking-tight text-gray-900">Sandboxes</h1>
          <p className="mt-1 text-sm text-gray-500">Durable workspaces for agent runs, with controlled images and restorable snapshots.</p>
        </div>
        <Link to="/dashboard/sandboxes/new" className="flex h-9 items-center gap-2 rounded-lg bg-gray-900 px-3 text-sm font-medium text-white hover:bg-gray-800">
          <Plus size={15} />
          New sandbox
        </Link>
      </header>

      <div className="mb-6 grid grid-cols-2 overflow-hidden rounded-lg border border-gray-200 bg-white sm:grid-cols-4">
        {[
          { label: 'Total sandboxes', value: counts.total },
          { label: 'Running', value: counts.running },
          { label: 'Stopped', value: counts.stopped },
          { label: 'Needs attention', value: counts.attention },
        ].map(metric => (
          <div key={metric.label} className="border-b border-r border-gray-200 px-4 py-4 last:border-r-0 sm:border-b-0">
            <p className="text-xl font-semibold tabular-nums text-gray-900">{metric.value}</p>
            <p className="mt-1 text-xs text-gray-500">{metric.label}</p>
          </div>
        ))}
      </div>

      {error && <div role="alert" className="mb-4 rounded-lg bg-red-50 p-3 text-sm text-red-700">{error}</div>}
      <section className="overflow-hidden rounded-lg border border-gray-200 bg-white">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 px-5 py-4">
          <div>
            <h2 className="text-sm font-semibold text-gray-900">Workspace fleet</h2>
            <p className="mt-1 text-sm text-gray-500">State is durable across stop and start operations.</p>
          </div>
          <div className="flex w-full gap-2 sm:w-auto">
            <div className="relative min-w-0 flex-1 sm:w-60">
              <Search size={15} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
              <input value={query} onChange={event => setQuery(event.target.value)} aria-label="Search sandboxes" placeholder="Search sandboxes" className="h-9 w-full rounded-lg border border-gray-300 pl-9 pr-3 text-sm outline-none focus:border-gray-500 focus:ring-2 focus:ring-gray-200" />
            </div>
            <Select aria-label="Filter sandbox status" value={status} onChange={event => setStatus(event.target.value)} containerClassName="w-40">
              <option value="">All statuses</option>
              <option value="running">Running</option>
              <option value="stopped">Stopped</option>
              <option value="failed">Needs attention</option>
            </Select>
          </div>
        </div>

        {loading ? (
          <p className="px-5 py-10 text-sm text-gray-500">Loading sandboxes…</p>
        ) : filteredSandboxes.length === 0 ? (
          <div className="px-5 py-12 text-center">
            <p className="font-medium text-gray-900">No sandboxes found</p>
            <p className="mt-1 text-sm text-gray-500">Create a durable workspace from a ready image recipe and immutable profile.</p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[700px] text-left text-sm">
              <thead className="bg-gray-50 text-xs font-medium uppercase tracking-wide text-gray-500">
                <tr>
                  <th className="px-5 py-3">Name</th>
                  <th className="px-4 py-3">Status</th>
                  <th className="px-4 py-3">Pinned configuration</th>
                  <th className="px-5 py-3">Last activity</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {filteredSandboxes.map(sandbox => (
                  <tr key={sandbox.id} className="hover:bg-gray-50">
                    <td className="px-5 py-3">
                      <Link className="font-medium text-gray-900 hover:underline" to={`/dashboard/sandboxes/${sandbox.id}`}>{sandbox.name}</Link>
                      <code className="mt-1 block text-xs text-gray-500">{sandbox.id}</code>
                    </td>
                    <td className="px-4 py-3"><SandboxStatusBadge state={sandbox.observed_state} /></td>
                    <td className="px-4 py-3">
                      <p className="font-mono text-xs text-gray-800">Recipe {sandbox.recipe_version_id}</p>
                      <p className="mt-1 font-mono text-xs text-gray-500">Profile {sandbox.profile_version_id}</p>
                    </td>
                    <td className="px-5 py-3 text-gray-500">{new Date(sandbox.updated_at).toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  )
}

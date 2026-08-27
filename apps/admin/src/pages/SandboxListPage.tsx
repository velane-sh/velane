import { Plus, Search } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import SandboxStatusBadge, { isTransitionalSandboxState } from '../components/SandboxStatusBadge'
import Select from '../components/Select'
import { api } from '../lib/api'
import type { Sandbox } from '../types'
import Card from '../components/Card'
import { CardHeader } from '../components/Card'
import PageHeader from '../components/PageHeader'
import { buttonClasses } from '../components/Button'
import { TD, TBody, TH, THead, TR, Table } from '../components/Table'

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
      <PageHeader
        eyebrow="Agent infrastructure"
        title="Sandboxes"
        description="Durable workspaces for agent runs, with controlled images and restorable snapshots."
        actions={(
          <Link to="/dashboard/sandboxes/new" className={buttonClasses({ variant: 'primary', size: 'sm' })}>
            <Plus size={15} />
            New sandbox
          </Link>
        )}
      />

      <Card padded={false} className="mb-6 grid grid-cols-2 overflow-hidden sm:grid-cols-4">
        {[
          { label: 'Total sandboxes', value: counts.total },
          { label: 'Running', value: counts.running },
          { label: 'Stopped', value: counts.stopped },
          { label: 'Needs attention', value: counts.attention },
        ].map(metric => (
          <div key={metric.label} className="border-b border-r border-line px-4 py-4 last:border-r-0 sm:border-b-0">
            <p className="text-xl font-semibold tabular-nums text-content">{metric.value}</p>
            <p className="mt-1 text-xs text-content-muted">{metric.label}</p>
          </div>
        ))}
      </Card>

      {error && <div role="alert" className="mb-4 rounded-lg bg-danger-subtle p-3 text-sm text-danger-text">{error}</div>}
      <Card padded={false}>
        <CardHeader
          title="Workspace fleet"
          description="State is durable across stop and start operations."
          actions={(
            <div className="flex w-full gap-2 sm:w-auto">
              <div className="relative min-w-0 flex-1 sm:w-60">
                <Search size={15} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-content-subtle" />
                <input value={query} onChange={event => setQuery(event.target.value)} aria-label="Search sandboxes" placeholder="Search sandboxes" className="h-9 w-full rounded-lg border border-line-strong bg-surface pl-9 pr-3 text-sm text-content outline-none focus:border-accent focus:ring-2 focus:ring-accent-ring" />
              </div>
              <Select aria-label="Filter sandbox status" value={status} onChange={event => setStatus(event.target.value)} containerClassName="w-40">
                <option value="">All statuses</option>
                <option value="running">Running</option>
                <option value="stopped">Stopped</option>
                <option value="failed">Needs attention</option>
              </Select>
            </div>
          )}
        />

        {loading ? (
          <p className="px-5 py-10 text-sm text-content-muted">Loading sandboxes…</p>
        ) : filteredSandboxes.length === 0 ? (
          <div className="px-5 py-12 text-center">
            <p className="font-medium text-content">No sandboxes found</p>
            <p className="mt-1 text-sm text-content-muted">Create a durable workspace from a ready image recipe and immutable profile.</p>
          </div>
        ) : (
          <Table className="rounded-none border-0 shadow-none" minWidthClassName="min-w-[700px]">
            <THead>
              <TR className="hover:bg-transparent">
                <TH className="px-5 py-3">Name</TH>
                <TH className="px-4 py-3">Status</TH>
                <TH className="px-4 py-3">Pinned configuration</TH>
                <TH className="px-5 py-3">Last activity</TH>
              </TR>
            </THead>
            <TBody>
              {filteredSandboxes.map(sandbox => (
                <TR key={sandbox.id}>
                  <TD className="px-5 py-3">
                    <Link className="font-medium text-content hover:underline" to={`/dashboard/sandboxes/${sandbox.id}`}>{sandbox.name}</Link>
                    <code className="mt-1 block text-xs text-content-muted">{sandbox.id}</code>
                  </TD>
                  <TD className="px-4 py-3"><SandboxStatusBadge state={sandbox.observed_state} /></TD>
                  <TD className="px-4 py-3">
                    <p className="font-mono text-xs text-content">Recipe {sandbox.recipe_version_id}</p>
                    <p className="mt-1 font-mono text-xs text-content-muted">Profile {sandbox.profile_version_id}</p>
                  </TD>
                  <TD className="px-5 py-3 text-content-muted">{new Date(sandbox.updated_at).toLocaleString()}</TD>
                </TR>
              ))}
            </TBody>
          </Table>
        )}
      </Card>
    </div>
  )
}

import { ArrowLeft, RefreshCw, Trash2 } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import ConfirmActionDialog from '../components/ConfirmActionDialog'
import SandboxActionBar from '../components/SandboxActionBar'
import SandboxActivityPanel from '../components/SandboxActivityPanel'
import SandboxOperationPanel from '../components/SandboxOperationPanel'
import SandboxSnapshotsPanel from '../components/SandboxSnapshotsPanel'
import SandboxStatusBadge, { isTransitionalSandboxState } from '../components/SandboxStatusBadge'
import { APIError, api } from '../lib/api'
import type { Sandbox, SandboxEvent, SandboxLog, SandboxOperation, SandboxOperationKind, SandboxSnapshot } from '../types'

function idempotencyKey() { return crypto.randomUUID() }
function isActive(operation?: SandboxOperation | null) { return Boolean(operation && ['queued', 'claimed', 'dispatched', 'waiting'].includes(operation.state)) }

type Confirmation =
  | { kind: 'restore'; snapshot: SandboxSnapshot }
  | { kind: 'deleteSnapshot'; snapshot: SandboxSnapshot }
  | { kind: 'deleteSandbox' }

export default function SandboxDetailPage() {
  const { sandboxId = '' } = useParams<{ sandboxId: string }>()
  const navigate = useNavigate()
  const [sandbox, setSandbox] = useState<Sandbox | null>(null)
  const [availableActions, setAvailableActions] = useState<SandboxOperationKind[]>([])
  const [snapshots, setSnapshots] = useState<SandboxSnapshot[]>([])
  const [events, setEvents] = useState<SandboxEvent[]>([])
  const [logs, setLogs] = useState<SandboxLog[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [operation, setOperation] = useState<SandboxOperation | null>(null)
  const [busy, setBusy] = useState(false)
  const [confirm, setConfirm] = useState<Confirmation | null>(null)

  const load = useCallback(async () => {
    if (!sandboxId || document.hidden) return
    try {
      const [detail, snapshotPage, eventPage, logPage] = await Promise.all([
        api.getSandbox(sandboxId),
        api.listSandboxSnapshots(sandboxId, { limit: 50 }),
        api.listSandboxEvents(sandboxId),
        api.listSandboxLogs(sandboxId),
      ])
      setSandbox(detail.sandbox)
      setAvailableActions(detail.available_actions)
      setSnapshots(snapshotPage.items ?? [])
      setEvents(eventPage.items ?? [])
      setLogs(logPage.items ?? [])
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load this sandbox.')
    } finally {
      setLoading(false)
    }
  }, [sandboxId])

  useEffect(() => { load() }, [load])

  useEffect(() => {
    if (!sandbox || (!isTransitionalSandboxState(sandbox.observed_state) && !isActive(operation))) return
    let timer: number | undefined
    const schedule = () => { if (!document.hidden) timer = window.setTimeout(() => { load(); schedule() }, 5000) }
    const onVisibilityChange = () => { if (!document.hidden) load() }
    schedule()
    document.addEventListener('visibilitychange', onVisibilityChange)
    return () => { if (timer) window.clearTimeout(timer); document.removeEventListener('visibilitychange', onVisibilityChange) }
  }, [load, operation, sandbox])

  useEffect(() => {
    if (!operation || !isActive(operation)) return
    let cancelled = false
    let timer: number | undefined
    const poll = async () => {
      try {
        const nextOperation = await api.getSandboxOperation(operation.id)
        if (cancelled) return
        setOperation(nextOperation)
        if (isActive(nextOperation)) timer = window.setTimeout(poll, 5000)
        else load()
      } catch {
        if (!cancelled) timer = window.setTimeout(poll, 10000)
      }
    }
    poll()
    return () => { cancelled = true; if (timer) window.clearTimeout(timer) }
  }, [load, operation?.id, operation?.state])

  async function runAction(action: Extract<SandboxOperationKind, 'start' | 'stop' | 'restart' | 'snapshot'>) {
    if (!sandbox) return
    setBusy(true); setError('')
    try {
      const response = action === 'snapshot'
        ? await api.createSandboxSnapshot(sandbox.id, idempotencyKey(), sandbox.generation)
        : await api.sandboxAction(sandbox.id, action, idempotencyKey(), sandbox.generation)
      setOperation(response.operation)
      await load()
    } catch (err) {
      setError(err instanceof APIError ? err.message : err instanceof Error ? err.message : 'Action failed.')
    } finally { setBusy(false) }
  }

  async function retryOperation() {
    if (!sandbox || !operation) return
    setBusy(true); setError('')
    try {
      const response = await api.retrySandboxOperation(sandbox.id, operation.id, idempotencyKey(), sandbox.generation)
      setOperation(response.operation)
      await load()
    } catch (err) { setError(err instanceof Error ? err.message : 'Retry failed.') } finally { setBusy(false) }
  }

  async function prepareRestore(snapshot: SandboxSnapshot) {
    if (!sandbox) return
    setBusy(true)
    setError('')
    try {
      const currentSnapshot = await api.getSandboxSnapshot(sandbox.id, snapshot.id)
      setConfirm({ kind: 'restore', snapshot: currentSnapshot })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load the selected snapshot.')
    } finally {
      setBusy(false)
    }
  }

  async function confirmAction() {
    if (!sandbox || !confirm) return
    setBusy(true); setError('')
    try {
      let response
      if (confirm.kind === 'restore') response = await api.restoreSandboxSnapshot(sandbox.id, confirm.snapshot.id, idempotencyKey(), sandbox.generation)
      if (confirm.kind === 'deleteSnapshot') response = await api.deleteSandboxSnapshot(sandbox.id, confirm.snapshot.id, idempotencyKey(), sandbox.generation)
      if (confirm.kind === 'deleteSandbox') response = await api.deleteSandbox(sandbox.id, true, idempotencyKey(), sandbox.generation)
      if (response) setOperation(response.operation)
      setConfirm(null)
      if (confirm.kind === 'deleteSandbox') navigate('/dashboard/sandboxes')
      else await load()
    } catch (err) { setError(err instanceof Error ? err.message : 'Action failed.') } finally { setBusy(false) }
  }

  if (loading) return <p className="text-sm text-gray-500">Loading sandbox…</p>
  if (!sandbox) return <div><Link to="/dashboard/sandboxes" className="inline-flex items-center gap-1 text-sm text-gray-600 hover:underline"><ArrowLeft size={15} />Back to sandboxes</Link><p className="mt-5 text-sm text-red-700">{error || 'Sandbox not found.'}</p></div>

  const pending = busy || isActive(operation)
  const canRestore = sandbox.observed_state === 'stopped'
  const canDelete = availableActions.includes('delete')

  return (
    <div className="mx-auto max-w-6xl">
      <header className="mb-6 flex flex-wrap items-start justify-between gap-4"><div><p className="text-xs font-semibold uppercase tracking-[0.09em] text-gray-500">Sandboxes / {sandbox.name}</p><div className="mt-2 flex items-center gap-3"><h1 className="text-2xl font-bold tracking-tight text-gray-900">{sandbox.name}</h1><SandboxStatusBadge state={sandbox.observed_state} /></div><p className="mt-1 font-mono text-xs text-gray-500">{sandbox.id} · created {new Date(sandbox.created_at).toLocaleDateString()}</p></div><SandboxActionBar availableActions={availableActions} pending={pending} onAction={runAction} /></header>
      {error && <div role="alert" className="mb-5 rounded-lg bg-red-50 p-3 text-sm text-red-700">{error}</div>}
      <SandboxOperationPanel operation={operation ?? undefined} />
      {operation?.state === 'failed' && <button type="button" disabled={pending} onClick={retryOperation} className="mt-3 inline-flex items-center gap-2 rounded-lg border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"><RefreshCw size={15} />Retry failed operation</button>}
      <div className="mt-5 grid grid-cols-2 overflow-hidden rounded-lg border border-gray-200 bg-white sm:grid-cols-4">{[{ value: sandbox.desired_state.replace('_', ' '), label: 'Desired state' }, { value: sandbox.observed_state.replace('_', ' '), label: 'Observed state' }, { value: String(sandbox.generation), label: 'Generation' }, { value: sandbox.latest_snapshot_id ? 'Available' : 'None yet', label: 'Latest recovery point' }].map(metric => <div key={metric.label} className="border-b border-r border-gray-200 px-4 py-4 last:border-r-0 sm:border-b-0"><p className="text-lg font-semibold capitalize text-gray-900">{metric.value}</p><p className="mt-1 text-xs text-gray-500">{metric.label}</p></div>)}</div>
      <div className="mt-5 grid gap-6 lg:grid-cols-[minmax(0,1fr)_19rem]"><div className="space-y-6"><SandboxSnapshotsPanel snapshots={snapshots} canSnapshot={availableActions.includes('snapshot')} canRestore={canRestore} pending={pending} onSnapshot={() => runAction('snapshot')} onRestore={prepareRestore} onDelete={snapshot => setConfirm({ kind: 'deleteSnapshot', snapshot })} /><SandboxActivityPanel events={events} logs={logs} /></div><aside className="space-y-5"><section className="rounded-lg border border-gray-200 bg-white p-5"><h2 className="text-sm font-semibold text-gray-900">Pinned configuration</h2><dl className="mt-4 space-y-3 text-sm"><div className="flex justify-between gap-4"><dt className="text-gray-500">Recipe version</dt><dd className="max-w-40 break-all text-right font-mono text-xs text-gray-800">{sandbox.recipe_version_id}</dd></div><div className="flex justify-between gap-4"><dt className="text-gray-500">Profile version</dt><dd className="max-w-40 break-all text-right font-mono text-xs text-gray-800">{sandbox.profile_version_id}</dd></div><div className="flex justify-between gap-4"><dt className="text-gray-500">Generation</dt><dd className="text-right text-gray-800">{sandbox.generation}</dd></div></dl></section><section className="rounded-lg border border-blue-200 bg-blue-50 p-5"><h2 className="text-sm font-semibold text-blue-900">Durable recovery</h2><p className="mt-2 text-sm leading-6 text-blue-800">Stopping records full guest memory and process state, VM/device state, and every mutable disk before compute is released.</p></section>{sandbox.failure_message && <section className="rounded-lg border border-red-200 bg-red-50 p-5"><h2 className="text-sm font-semibold text-red-900">Sandbox failure</h2><p className="mt-2 text-sm text-red-800">{sandbox.failure_message}</p></section>}<section className="rounded-lg border border-gray-200 bg-white p-5"><h2 className="text-sm font-semibold text-gray-900">Destructive actions</h2><button type="button" disabled={pending || !canDelete} onClick={() => setConfirm({ kind: 'deleteSandbox' })} className="mt-4 flex w-full items-center justify-center gap-2 rounded-lg border border-red-200 px-3 py-2 text-sm font-medium text-red-700 hover:bg-red-50 disabled:opacity-50"><Trash2 size={15} />Delete sandbox</button><p className="mt-2 text-xs leading-5 text-gray-500">Deletion removes the workspace and all snapshots after confirmation.</p></section></aside></div>
      {confirm && <ConfirmActionDialog title={confirm.kind === 'restore' ? 'Restore snapshot' : confirm.kind === 'deleteSnapshot' ? 'Delete snapshot' : 'Delete sandbox'} description={confirm.kind === 'restore' ? 'This replaces the stopped workspace with the selected full recovery point. It has no cold-start fallback.' : confirm.kind === 'deleteSnapshot' ? 'This permanently deletes this recovery point.' : 'This deletes the workspace and all of its snapshots. This action cannot be undone.'} confirmLabel={confirm.kind === 'restore' ? 'Restore snapshot' : confirm.kind === 'deleteSnapshot' ? 'Delete snapshot' : 'Delete sandbox'} danger={confirm.kind !== 'restore'} busy={busy} onCancel={() => setConfirm(null)} onConfirm={confirmAction} />}
    </div>
  )
}

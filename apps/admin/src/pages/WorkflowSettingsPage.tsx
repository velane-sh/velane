import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import WorkflowHeader from '../components/WorkflowHeader'
import { Toast, useToast } from '../components/Toast'
import { api } from '../lib/api'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import type {
  RuntimeLimits,
  RuntimeSettings,
  Snippet,
  SnippetEnvironment,
  SnippetVersion,
  Connection,
  WorkflowTrigger,
} from '../types'

const TIMEOUT_PRESETS = [
  { label: '1 minute', ms: 60000 },
  { label: '2 minutes', ms: 120000 },
  { label: '5 minutes', ms: 300000 },
  { label: '15 minutes', ms: 900000 },
]

function formatDuration(ms: number): string {
  if (ms < 60000) return `${Math.round(ms / 1000)}s`
  return `${Math.round(ms / 60000)}m`
}

export default function WorkflowSettingsPage() {
  const { id } = useParams<{ id: string }>()
  const [snippet, setSnippet] = useState<Snippet | null>(null)
  const [versions, setVersions] = useState<SnippetVersion[]>([])
  const [environments, setEnvironments] = useState<SnippetEnvironment[]>([])
  const [tenantCaps, setTenantCaps] = useState<RuntimeLimits | null>(null)
  const [settings, setSettings] = useState<RuntimeSettings>({
    timeout_ms: 60000,
    max_memory_mb: 200,
    max_cpu_percent: 10,
  })
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [connections, setConnections] = useState<Connection[]>([])
  const [triggers, setTriggers] = useState<WorkflowTrigger[]>([])
  const [syncModels, setSyncModels] = useState<string[]>([])
  const [manualModel, setManualModel] = useState(false)
  const [triggerForm, setTriggerForm] = useState<{ connection_id: string; model: string; environment: 'dev' | 'staging' | 'prod'; change_types: Array<'added' | 'updated' | 'deleted'> }>({ connection_id: '', model: '', environment: 'dev', change_types: ['added', 'updated', 'deleted'] })
  const { toast, showToast, dismissToast } = useToast()

  useDocumentTitle(
    snippet ? `${snippet.name} settings` : undefined,
    snippet ? 'Workflows' : undefined,
  )

  useEffect(() => {
    if (!id) return
    async function load() {
      try {
        const [sn, vs, envs, caps, conns, triggerRows] = await Promise.all([
          api.getSnippet(id!),
          api.listVersions(id!),
          api.listEnvironments(id!),
          api.getRuntimeLimits(),
          api.listConnections(),
          api.listWorkflowTriggers(id!),
        ])
        setSnippet(sn)
        setVersions(vs)
        setEnvironments(envs ?? [])
        setTenantCaps(caps)
        setConnections(conns)
        setTriggers(triggerRows)
        if (conns[0]) {
          setTriggerForm((f) => ({ ...f, connection_id: conns[0].id }))
          try {
            const discovered = await api.listIntegrationEventModels(conns[0].id)
            setSyncModels(discovered.models)
            setManualModel(discovered.manual_entry)
          } catch { setManualModel(true) }
        }
        if (vs.length > 0) {
          const latest = vs[vs.length - 1]
          setSettings({
            timeout_ms: latest.timeout_ms ?? 60000,
            max_memory_mb: latest.max_memory_mb ?? 200,
            max_cpu_percent: latest.max_cpu_percent ?? 10,
          })
        }
      } catch (err) {
        showToast(String(err), 'error')
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [id])

  async function handleSave() {
    if (!id || versions.length === 0) return
    setSaving(true)
    try {
      const latest = versions[versions.length - 1]
      await api.createVersion(id, latest.code, settings)
      const vs = await api.listVersions(id)
      setVersions(vs)
      showToast('Runtime settings saved as new version')
    } catch (err) {
      showToast(String(err), 'error')
    } finally {
      setSaving(false)
    }
  }

  async function selectTriggerConnection(connectionId: string) {
    setTriggerForm((f) => ({ ...f, connection_id: connectionId, model: '' }))
    if (!connectionId) { setSyncModels([]); return }
    try { const result = await api.listIntegrationEventModels(connectionId); setSyncModels(result.models); setManualModel(result.manual_entry) }
    catch { setSyncModels([]); setManualModel(true) }
  }

  async function createTrigger() {
    if (!id || !triggerForm.connection_id || !triggerForm.model.trim()) return
    try {
      const created = await api.createWorkflowTrigger(id, { ...triggerForm, model: triggerForm.model.trim() })
      setTriggers((rows) => [...rows, created])
      showToast('Trigger created disabled')
    } catch (err) { showToast(String(err), 'error') }
  }

  async function toggleTrigger(trigger: WorkflowTrigger) {
    if (!id) return
    try {
      const updated = await api.updateWorkflowTrigger(id, { ...trigger, enabled: !trigger.enabled })
      setTriggers((rows) => rows.map((row) => row.id === updated.id ? updated : row))
    } catch (err) { showToast(String(err), 'error') }
  }

  async function deleteTrigger(triggerId: string) {
    if (!id) return
    try { await api.deleteWorkflowTrigger(id, triggerId); setTriggers((rows) => rows.filter((row) => row.id !== triggerId)) }
    catch (err) { showToast(String(err), 'error') }
  }

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center text-gray-500">Loading…</div>
    )
  }

  const activeByEnv = environments.map((e) => {
    const ver = versions.find((v) => v.version_number === e.active_version_number)
    return { env: e.env, version: ver }
  })

  return (
    <div className="flex h-full flex-col">
      {toast && <Toast message={toast.message} type={toast.type} onDismiss={dismissToast} />}

      <WorkflowHeader snippet={snippet} />

      <div className="flex-1 overflow-y-auto p-6">
        <div className="mx-auto max-w-2xl space-y-8">
          <section>
            <h2 className="text-lg font-semibold text-gray-900">Runtime limits</h2>
            <p className="mt-1 text-sm text-gray-500">
              Applied when you save — creates a new workflow version with these limits.
              Published environments keep their current version until you publish again.
            </p>

            {tenantCaps && (
              <div className="mt-4 rounded-lg border border-gray-200 bg-gray-50 p-4 text-sm text-gray-600">
                <p className="font-medium text-gray-900">Tenant limits (read-only)</p>
                <ul className="mt-2 space-y-1">
                  <li>Max timeout: {formatDuration(tenantCaps.max_timeout_ms)}</li>
                  <li>Max memory: {tenantCaps.max_memory_mb} MB</li>
                  <li>Max CPU: {tenantCaps.max_cpu_percent}% of one core</li>
                </ul>
              </div>
            )}

            <div className="mt-6 space-y-5">
              <div>
                <label className="block text-sm font-medium text-gray-700">Timeout</label>
                <div className="mt-2 flex flex-wrap gap-2">
                  {TIMEOUT_PRESETS.map((p) => (
                    <button
                      key={p.ms}
                      type="button"
                      disabled={tenantCaps != null && p.ms > tenantCaps.max_timeout_ms}
                      className={`rounded-lg border px-3 py-1.5 text-sm ${
                        settings.timeout_ms === p.ms
                          ? 'border-gray-900 bg-gray-900 text-white'
                          : 'border-gray-300 text-gray-700 hover:bg-gray-50 disabled:opacity-40'
                      }`}
                      onClick={() => setSettings((s) => ({ ...s, timeout_ms: p.ms }))}
                    >
                      {p.label}
                    </button>
                  ))}
                </div>
                <div className="mt-3 flex items-center gap-2">
                  <label className="text-sm text-gray-600" htmlFor="timeout-custom">Custom (ms)</label>
                  <input
                    id="timeout-custom"
                    type="number"
                    min={1000}
                    max={tenantCaps?.max_timeout_ms ?? 900000}
                    className="rounded-lg border border-gray-300 px-3 py-1.5 text-sm"
                    value={settings.timeout_ms}
                    onChange={(e) =>
                      setSettings((s) => ({ ...s, timeout_ms: Number(e.target.value) }))
                    }
                  />
                </div>
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-700" htmlFor="memory-mb">
                  Memory (MB)
                </label>
                <input
                  id="memory-mb"
                  type="number"
                  min={64}
                  max={tenantCaps?.max_memory_mb ?? 2048}
                  className="mt-2 w-full max-w-xs rounded-lg border border-gray-300 px-3 py-2 text-sm"
                  value={settings.max_memory_mb}
                  onChange={(e) =>
                    setSettings((s) => ({ ...s, max_memory_mb: Number(e.target.value) }))
                  }
                />
                <p className="mt-1 text-xs text-gray-500">
                  Agent workflows (LangGraph / Mastra) often need 256–1024 MB.
                </p>
              </div>

              <div>
                <label className="block text-sm font-medium text-gray-700" htmlFor="cpu-percent">
                  CPU (0.1 = 10% of one vCPU)
                </label>
                <input
                  id="cpu-percent"
                  type="number"
                  min={1}
                  max={tenantCaps?.max_cpu_percent ?? 100}
                  className="mt-2 w-full max-w-xs rounded-lg border border-gray-300 px-3 py-2 text-sm"
                  value={settings.max_cpu_percent}
                  onChange={(e) =>
                    setSettings((s) => ({ ...s, max_cpu_percent: Number(e.target.value) }))
                  }
                />
              </div>

              <button
                type="button"
                disabled={saving || versions.length === 0}
                className="rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
                onClick={() => handleSave()}
              >
                {saving ? 'Saving…' : 'Save runtime settings'}
              </button>
            </div>
          </section>

          {activeByEnv.length > 0 && (
            <section>
              <h2 className="text-lg font-semibold text-gray-900">Active versions</h2>
              <p className="mt-1 text-sm text-gray-500">
                Runtime limits for the version currently pinned in each environment.
              </p>
              <ul className="mt-4 divide-y divide-gray-200 rounded-lg border border-gray-200">
                {activeByEnv.map(({ env, version }) => (
                  <li key={env} className="flex items-center justify-between px-4 py-3 text-sm">
                    <span className="font-medium text-gray-900">{env}</span>
                    {version ? (
                      <span className="text-gray-600">
                        v{version.version_number} — {formatDuration(version.timeout_ms)},{' '}
                        {version.max_memory_mb} MB, {version.max_cpu_percent}% CPU
                      </span>
                    ) : (
                      <span className="text-gray-400">No active version</span>
                    )}
                  </li>
                ))}
              </ul>
            </section>
          )}

          <section>
            <h2 className="text-lg font-semibold text-gray-900">Triggers</h2>
            <p className="mt-1 text-sm text-gray-500">Run this workflow from incremental Nango sync changes. New triggers stay disabled until explicitly enabled.</p>
            <div className="mt-4 grid gap-3 rounded-lg border border-gray-200 p-4 sm:grid-cols-2">
              <select className="rounded-lg border border-gray-300 px-3 py-2 text-sm" value={triggerForm.connection_id} onChange={(e) => selectTriggerConnection(e.target.value)}>
                <option value="">Select connection</option>
                {connections.map((c) => <option key={c.id} value={c.id}>{c.provider} · {c.alias}</option>)}
              </select>
              {syncModels.length > 0 ? <select className="rounded-lg border border-gray-300 px-3 py-2 text-sm" value={triggerForm.model} onChange={(e) => setTriggerForm((f) => ({ ...f, model: e.target.value }))}><option value="">Select sync model</option>{syncModels.map((model) => <option key={model}>{model}</option>)}</select> : <input className="rounded-lg border border-gray-300 px-3 py-2 text-sm" placeholder={manualModel ? 'Sync model name (manual)' : 'Select a connection first'} disabled={!triggerForm.connection_id} value={triggerForm.model} onChange={(e) => setTriggerForm((f) => ({ ...f, model: e.target.value }))} />}
              <select className="rounded-lg border border-gray-300 px-3 py-2 text-sm" value={triggerForm.environment} onChange={(e) => setTriggerForm((f) => ({ ...f, environment: e.target.value as 'dev' | 'staging' | 'prod' }))}>
                {['dev', 'staging', 'prod'].map((env) => <option key={env}>{env}</option>)}
              </select>
              <div className="flex items-center gap-3 text-sm text-gray-700">
                {(['added', 'updated', 'deleted'] as const).map((change) => <label key={change} className="flex items-center gap-1"><input type="checkbox" checked={triggerForm.change_types.includes(change)} onChange={(e) => setTriggerForm((f) => ({ ...f, change_types: e.target.checked ? [...f.change_types, change] : f.change_types.filter((v) => v !== change) }))} />{change}</label>)}
              </div>
              <button type="button" className="rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50" disabled={!triggerForm.connection_id || !triggerForm.model || triggerForm.change_types.length === 0} onClick={createTrigger}>Create trigger</button>
            </div>
            <ul className="mt-4 space-y-3">
              {triggers.map((trigger) => <li key={trigger.id} className="rounded-lg border border-gray-200 p-4 text-sm"><div className="flex items-center justify-between"><div><p className="font-medium text-gray-900">{trigger.model} → {trigger.environment}</p><p className="text-gray-500">{trigger.change_types.join(', ')}</p></div><div className="flex gap-2"><button type="button" className="rounded-lg border border-gray-300 px-3 py-1.5 hover:bg-gray-50" onClick={() => toggleTrigger(trigger)}>{trigger.enabled ? 'Disable' : 'Enable'}</button><button type="button" className="rounded-lg border border-gray-300 px-3 py-1.5 text-red-700 hover:bg-gray-50" onClick={() => deleteTrigger(trigger.id)}>Delete</button></div></div>{trigger.last_delivery_at && <p className="mt-2 text-gray-500">Last delivery: {new Date(trigger.last_delivery_at).toLocaleString()}</p>}{trigger.last_error && <p className="mt-2 text-red-700">{trigger.last_error}</p>}</li>)}
            </ul>
          </section>
        </div>
      </div>
    </div>
  )
}

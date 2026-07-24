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
  const { toast, showToast, dismissToast } = useToast()

  useDocumentTitle(
    snippet ? `${snippet.name} settings` : undefined,
    snippet ? 'Workflows' : undefined,
  )

  useEffect(() => {
    if (!id) return
    async function load() {
      try {
        const [sn, vs, envs, caps] = await Promise.all([
          api.getSnippet(id!),
          api.listVersions(id!),
          api.listEnvironments(id!),
          api.getRuntimeLimits(),
        ])
        setSnippet(sn)
        setVersions(vs)
        setEnvironments(envs ?? [])
        setTenantCaps(caps)
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

  if (loading) {
    return (
      <div className="flex h-full flex-col">
        <WorkflowHeader snippet={snippet} />
        <div className="flex flex-1 items-center justify-center text-gray-500">Loading settings…</div>
      </div>
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
        </div>
      </div>
    </div>
  )
}

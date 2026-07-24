import { ChevronDown, ChevronRight, RefreshCw } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'
import WorkflowHeader from '../components/WorkflowHeader'
import Select from '../components/Select'
import { Toast, useToast } from '../components/Toast'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { api } from '../lib/api'
import type {
  Invocation,
  InvocationStatus,
  InvocationSummary,
  Snippet,
} from '../types'

const STATUSES: InvocationStatus[] = [
  'pending',
  'running',
  'completed',
  'failed',
  'timeout',
  'oom_killed',
]

const statusClasses: Record<InvocationStatus, string> = {
  pending: 'bg-gray-100 text-gray-700',
  running: 'bg-blue-50 text-blue-700',
  completed: 'bg-green-50 text-green-700',
  failed: 'bg-red-50 text-red-700',
  timeout: 'bg-amber-50 text-amber-700',
  oom_killed: 'bg-red-50 text-red-700',
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms} ms`
  return `${(ms / 1000).toFixed(ms < 10000 ? 1 : 0)} s`
}

function formatPayload(value: string): string {
  if (!value) return '—'
  try {
    return JSON.stringify(JSON.parse(value), null, 2)
  } catch {
    return value
  }
}

function DetailBlock({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-gray-500">{label}</h3>
      <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-gray-900 p-4 text-xs text-gray-100">
        {value || '—'}
      </pre>
    </div>
  )
}

export default function WorkflowExecutionsPage() {
  const { id } = useParams<{ id: string }>()
  const [snippet, setSnippet] = useState<Snippet | null>(null)
  const [executions, setExecutions] = useState<InvocationSummary[]>([])
  const [environment, setEnvironment] = useState('')
  const [status, setStatus] = useState<InvocationStatus | ''>('')
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [details, setDetails] = useState<Record<string, Invocation>>({})
  const [detailLoading, setDetailLoading] = useState<string | null>(null)
  const { toast, showToast, dismissToast } = useToast()

  useDocumentTitle(
    snippet ? `${snippet.name} executions` : undefined,
    snippet ? 'Workflows' : undefined,
  )

  const loadExecutions = useCallback(async (background = false) => {
    if (!id) return
    background ? setRefreshing(true) : setLoading(true)
    try {
      const response = await api.listSnippetInvocations(id, {
        environment: environment || undefined,
        status: status || undefined,
      })
      setExecutions(response.items ?? [])
    } catch (err) {
      showToast(String(err), 'error')
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [id, environment, status])

  useEffect(() => {
    if (!id) return
    api.getSnippet(id)
      .then(setSnippet)
      .catch((err) => showToast(String(err), 'error'))
  }, [id])

  useEffect(() => {
    loadExecutions()
  }, [loadExecutions])

  async function toggleDetails(invocationId: string) {
    if (expandedId === invocationId) {
      setExpandedId(null)
      return
    }
    setExpandedId(invocationId)
    if (details[invocationId]) return
    setDetailLoading(invocationId)
    try {
      const invocation = await api.getInvocation(invocationId)
      setDetails((current) => ({ ...current, [invocationId]: invocation }))
    } catch (err) {
      setExpandedId(null)
      showToast(String(err), 'error')
    } finally {
      setDetailLoading(null)
    }
  }

  return (
    <div className="flex h-full flex-col">
      {toast && <Toast message={toast.message} type={toast.type} onDismiss={dismissToast} />}
      <WorkflowHeader snippet={snippet} />

      <div className="flex-1 overflow-y-auto p-6">
        <div className="mx-auto max-w-6xl">
          <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
            <div>
              <h1 className="text-xl font-semibold text-gray-900">Executions</h1>
              <p className="mt-1 text-sm text-gray-500">
                Recent workflow runs and their runtime details.
              </p>
            </div>
            <div className="flex flex-wrap items-center gap-3">
              <Select
                aria-label="Filter by environment"
                value={environment}
                onChange={(event) => setEnvironment(event.target.value)}
                containerClassName="w-44"
              >
                <option value="">All environments</option>
                <option value="dev">Development</option>
                <option value="staging">Staging</option>
                <option value="prod">Production</option>
              </Select>
              <Select
                aria-label="Filter by status"
                value={status}
                onChange={(event) => setStatus(event.target.value as InvocationStatus | '')}
                containerClassName="w-36"
              >
                <option value="">All statuses</option>
                {STATUSES.map((item) => (
                  <option key={item} value={item}>{item.replace('_', ' ')}</option>
                ))}
              </Select>
              <button
                type="button"
                onClick={() => loadExecutions(true)}
                disabled={refreshing}
                className="flex h-9 items-center gap-2 rounded-lg border border-gray-300 px-3 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
              >
                <RefreshCw size={15} className={refreshing ? 'animate-spin' : ''} />
                Refresh
              </button>
            </div>
          </div>

          {loading ? (
            <div className="rounded-lg border border-gray-200 py-16 text-center text-sm text-gray-500">
              Loading executions…
            </div>
          ) : executions.length === 0 ? (
            <div className="rounded-lg border border-dashed border-gray-300 py-16 text-center">
              <p className="font-medium text-gray-900">No executions found</p>
              <p className="mt-1 text-sm text-gray-500">
                Runs will appear here after this workflow is invoked.
              </p>
            </div>
          ) : (
            <div className="overflow-hidden rounded-lg border border-gray-200 bg-white">
              <div className="overflow-x-auto">
                <table className="w-full text-left text-sm">
                  <thead className="border-b border-gray-200 bg-gray-50 text-xs uppercase text-gray-500">
                    <tr>
                      <th className="w-10 px-4 py-3" />
                      <th className="px-3 py-3 font-medium">Status</th>
                      <th className="px-3 py-3 font-medium">Environment</th>
                      <th className="px-3 py-3 font-medium">Started</th>
                      <th className="px-3 py-3 font-medium">Duration</th>
                      <th className="px-3 py-3 font-medium">Memory</th>
                      <th className="px-3 py-3 font-medium">CPU</th>
                      <th className="px-3 py-3 font-medium">Mode</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-gray-200">
                    {executions.map((execution) => {
                      const isExpanded = expandedId === execution.id
                      const detail = details[execution.id]
                      return (
                        <tr key={execution.id} className="group">
                          <td colSpan={8} className="p-0">
                            <button
                              type="button"
                              onClick={() => toggleDetails(execution.id)}
                              className="grid w-full grid-cols-[40px_1.2fr_1fr_1.8fr_1fr_1fr_1fr_0.8fr] items-center text-left hover:bg-gray-50"
                            >
                              <span className="px-4 py-4 text-gray-400">
                                {isExpanded ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
                              </span>
                              <span className="px-3 py-4">
                                <span className={`rounded-full px-2 py-1 text-xs font-medium ${statusClasses[execution.status]}`}>
                                  {execution.status.replace('_', ' ')}
                                </span>
                              </span>
                              <span className="px-3 py-4 capitalize text-gray-700">{execution.environment}</span>
                              <span className="px-3 py-4 text-gray-700">{new Date(execution.created_at).toLocaleString()}</span>
                              <span className="px-3 py-4 text-gray-700">{formatDuration(execution.duration_ms)}</span>
                              <span className="px-3 py-4 text-gray-700">{execution.peak_memory_mb} MB</span>
                              <span className="px-3 py-4 text-gray-700">{formatDuration(execution.cpu_ms)}</span>
                              <span className="px-3 py-4 capitalize text-gray-700">{execution.invoke_mode}</span>
                            </button>
                            {isExpanded && (
                              <div className="border-t border-gray-100 bg-gray-50 p-5">
                                {detailLoading === execution.id || !detail ? (
                                  <p className="text-sm text-gray-500">Loading execution details…</p>
                                ) : (
                                  <div className="space-y-5">
                                    <div className="flex flex-wrap gap-x-6 gap-y-2 text-xs text-gray-500">
                                      <span>Invocation ID: <span className="font-mono text-gray-700">{detail.id}</span></span>
                                      <span>Version ID: <span className="font-mono text-gray-700">{detail.version_id}</span></span>
                                      <span>Payload: <span className="text-gray-700">{detail.payload_state}</span></span>
                                    </div>
                                    <div className="grid gap-5 lg:grid-cols-2">
                                      <DetailBlock label="Input" value={formatPayload(detail.input_payload)} />
                                      <DetailBlock label="Output" value={formatPayload(detail.output)} />
                                    </div>
                                    {(detail.error || detail.stderr) && (
                                      <div className="grid gap-5 lg:grid-cols-2">
                                        {detail.error && <DetailBlock label="Error" value={detail.error} />}
                                        {detail.stderr && <DetailBlock label="Stderr" value={detail.stderr} />}
                                      </div>
                                    )}
                                  </div>
                                )}
                              </div>
                            )}
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

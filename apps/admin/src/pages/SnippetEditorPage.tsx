import Editor from '@monaco-editor/react'
import { Check, ChevronRight, Copy, History, MoreHorizontal, Plug, Trash2, X } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import WorkflowHeader from '../components/WorkflowHeader'
import PublishDropdown from '../components/PublishDropdown'
import { Toast, useToast } from '../components/Toast'
import { api } from '../lib/api'
import { setupMonacoVelaneTypes } from '../lib/monacoVelaneTypes'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import type { InvocationResult, LogLine, RuntimeSettings, Snippet, SnippetEnvironment, SnippetVersion } from '../types'

type ActiveTab = 'test' | 'logs'

const STARTER_TEMPLATES: Record<string, string> = {
  bun: `export default async function handler(input: Record<string, unknown>) {
  // input is the parsed JSON body sent to /v1/invoke/:tenant/:snippet
  return { message: "Hello from Velane!", input }
}
`,
  python: `def handler(input: dict) -> dict:
    # input is the parsed JSON body sent to /v1/invoke/:tenant/:snippet
    return {"message": "Hello from Velane!", "input": input}
`,
}

export default function SnippetEditorPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()

  const [snippet, setSnippet] = useState<Snippet | null>(null)
  const [versions, setVersions] = useState<SnippetVersion[]>([])
  const [environments, setEnvironments] = useState<SnippetEnvironment[]>([])
  const [openEnvs, setOpenEnvs] = useState<Record<string, boolean>>({ dev: true, staging: true, prod: true })
  const [code, setCode] = useState('')
  const [loading, setLoading] = useState(!!id)
  const [autoSaving, setAutoSaving] = useState(false)
  const [autoSaved, setAutoSaved] = useState(false)
  const [showVersions, setShowVersions] = useState(false)
  const [showConnect, setShowConnect] = useState(false)
  const [connectTab, setConnectTab] = useState<'endpoint' | 'mcp'>('endpoint')
  const [copied, setCopied] = useState<string | null>(null)
  const [showMenu, setShowMenu] = useState(false)
  const menuRef = useRef<HTMLDivElement>(null)
  const [activeTab, setActiveTab] = useState<ActiveTab>('test')
  const [mcpURL, setMCPURL] = useState('')
  const [mcpError, setMCPError] = useState('')
  const [testInput, setTestInput] = useState('{}')
  const [testEnv, setTestEnv] = useState<'dev' | 'staging' | 'prod'>('dev')
  const [invokeResult, setInvokeResult] = useState<InvocationResult | null>(null)
  const [runLogs, setRunLogs] = useState<LogLine[]>([])
  const [invoking, setInvoking] = useState(false)
  const [selectedVersion, setSelectedVersion] = useState<SnippetVersion | null>(null)
  const [runtimeDraft, setRuntimeDraft] = useState<RuntimeSettings>({
    timeout_ms: 60000,
    max_memory_mb: 200,
    max_cpu_percent: 10,
  })

  const autosaveTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const isDirty = useRef(false)
  const ownVersionIds = useRef<Set<string>>(new Set())
  const { toast, showToast, dismissToast } = useToast()

  useDocumentTitle(
    snippet ? snippet.name || snippet.slug : undefined,
    snippet ? 'Workflows' : undefined,
  )

  useEffect(() => {
    if (!id) return
    async function load() {
      try {
        const [sn, vs, envs] = await Promise.all([api.getSnippet(id!), api.listVersions(id!), api.listEnvironments(id!)])
        setSnippet(sn)
        setVersions(vs)
        setEnvironments(envs ?? [])
        if (vs.length > 0) {
          const latest = vs[vs.length - 1]
          setCode(latest.code)
          setRuntimeDraft({
            timeout_ms: latest.timeout_ms ?? 60000,
            max_memory_mb: latest.max_memory_mb ?? 200,
            max_cpu_percent: latest.max_cpu_percent ?? 10,
          })
        } else {
          setCode(STARTER_TEMPLATES[sn.language] ?? STARTER_TEMPLATES.bun)
        }
      } catch (err) {
        showToast(String(err), 'error')
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [id])

  useEffect(() => {
    async function loadMCPInfo() {
      try {
        const info = await api.getMCPInfo()
        if (!info.mcp_url?.trim()) {
          setMCPError('MCP URL not configured')
          return
        }
        setMCPURL(info.mcp_url)
      } catch (err) {
        setMCPError(err instanceof Error ? err.message : 'MCP URL not configured')
      }
    }
    loadMCPInfo()
  }, [])

  async function reloadVersions() {
    if (!id) return
    try {
      const [vs, envs] = await Promise.all([api.listVersions(id), api.listEnvironments(id)])
      setVersions(vs)
      setEnvironments(envs ?? [])
    } catch {
      // ignore
    }
  }

  useEffect(() => {
    if (!id || loading) return
    const stop = api.watchSnippet(id, (incoming) => {
      // Ignore echoes of versions this client just created (our own autosaves
      // are broadcast back to us over the same watch stream).
      if (ownVersionIds.current.has(incoming.id)) {
        setVersions((prev) => (prev.some((v) => v.id === incoming.id) ? prev : [...prev, incoming]))
        return
      }
      if (isDirty.current) {
        showToast('Workflow updated by another editor — your edits take priority', 'error')
        return
      }
      setCode(incoming.code)
      setVersions((prev) => {
        const exists = prev.some((v) => v.id === incoming.id)
        if (exists) return prev
        return [...prev, incoming]
      })
    })
    return stop
  }, [id, loading])

  const monacoLanguage = snippet?.language === 'python' ? 'python' : 'typescript'

  const handleCodeChange = useCallback(
    (value: string | undefined) => {
      const newCode = value ?? ''
      setCode(newCode)
      if (!id) return

      isDirty.current = true
      setAutoSaving(true)
      setAutoSaved(false)
      if (autosaveTimer.current) clearTimeout(autosaveTimer.current)
      autosaveTimer.current = setTimeout(async () => {
        try {
          const created = await api.createVersion(id, newCode, runtimeDraft)
          if (created?.id) ownVersionIds.current.add(created.id)
          await reloadVersions()
          isDirty.current = false
          setAutoSaved(true)
          setTimeout(() => setAutoSaved(false), 2000)
        } catch {
          // silent auto-save failure
        } finally {
          setAutoSaving(false)
        }
      }, 1500)
    },
    [id, runtimeDraft],
  )

  async function handlePublish(env: 'dev' | 'staging' | 'prod') {
    if (!id) return
    try {
      const allVersions = await api.listVersions(id)
      const latest = allVersions[allVersions.length - 1]
      if (!latest) return
      await api.publishVersion(id, latest.version_number, env)
      await reloadVersions()
      showToast(`Published to ${env}`)
    } catch (err) {
      showToast(String(err), 'error')
    }
  }

  async function handleDelete() {
    if (!id) return
    if (!confirm('Delete this snippet? This cannot be undone.')) return
    try {
      await api.deleteSnippet(id)
      navigate('/dashboard/snippets')
    } catch (err) {
      showToast(String(err), 'error')
    }
  }

  async function handleRun() {
    if (!snippet?.slug) return
    setInvoking(true)
    setInvokeResult(null)
    setRunLogs([])

    const chunks: string[] = []
    let resultOutput: unknown
    let receivedResult = false
    let errMessage = ''

    try {
      await api.invokeSnippetStream(snippet.slug, testInput, testEnv, {
        onLog: (line) => setRunLogs((prev) => [...prev, line]),
        onChunk: (data) => {
          chunks.push(data)
        },
        onResult: (output) => {
          resultOutput = output
          receivedResult = true
        },
        onError: (message) => {
          errMessage = message
        },
      })

      const rawOutput = receivedResult ? resultOutput : chunks.join('')
      let parsedOutput: unknown = rawOutput
      if (typeof rawOutput === 'string') {
        try {
          parsedOutput = JSON.parse(rawOutput)
        } catch {
          parsedOutput = rawOutput
        }
      }

      setInvokeResult({
        output: parsedOutput,
        error: errMessage,
        stderr: '',
        duration_ms: 0,
        exit_code: errMessage ? 1 : 0,
      })
    } catch (err) {
      setInvokeResult({
        output: '',
        error: String(err),
        stderr: '',
        duration_ms: 0,
        exit_code: 1,
      })
    } finally {
      setInvoking(false)
    }
  }

  if (loading) {
    return <div className="flex h-full items-center justify-center text-sm text-gray-500">Loading...</div>
  }

  const displayCode = selectedVersion ? selectedVersion.code : code
  const isReadOnly = !!selectedVersion
  const hasDraft = versions.some((v) => v.status === 'draft')

  const invokeUrl = snippet
    ? `${window.location.origin}/api/v1/invoke/${snippet.slug}`
    : ''
  function copyToClipboard(text: string, key: string) {
    navigator.clipboard.writeText(text)
    setCopied(key)
    setTimeout(() => setCopied(null), 2000)
  }

  const claudeConfig = JSON.stringify({
    mcpServers: {
      velane: {
        url: mcpURL || 'MCP_URL_NOT_CONFIGURED',
        headers: { Authorization: 'Bearer vl_YOUR_API_KEY' },
      },
    },
  }, null, 2)
  const codexConfig = JSON.stringify({
    mcpServers: {
      velane: {
        url: mcpURL || 'MCP_URL_NOT_CONFIGURED',
        headers: { Authorization: 'Bearer vl_YOUR_API_KEY' },
      },
    },
  }, null, 2)

  const curlExample = snippet
    ? `curl -X POST "${invokeUrl}" \\\n  -H "Authorization: Bearer vl_YOUR_API_KEY" \\\n  -H "Content-Type: application/json" \\\n  -d '{"key": "value"}'`
    : ''

  return (
    <div className="flex h-full flex-col">
      {toast && <Toast message={toast.message} type={toast.type} onDismiss={dismissToast} />}

      <WorkflowHeader
        snippet={snippet}
        trailing={
          <div className="flex items-center gap-2">
            {autoSaving && <span className="text-xs text-gray-400">Saving…</span>}
            {!autoSaving && autoSaved && <span className="text-xs text-gray-400">Saved</span>}
            {versions.length > 0 && (
              <button
                className="flex items-center gap-1.5 rounded-md border border-gray-300 px-3 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-50"
                onClick={() => setShowVersions(true)}
              >
                <History className="h-3.5 w-3.5" />
                Versions
                <span className="rounded bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-600">
                  {versions.length}
                </span>
              </button>
            )}
            <button
              className="flex items-center gap-1.5 rounded-md border border-gray-300 px-3 py-1.5 text-sm font-medium text-gray-700 hover:bg-gray-50"
              onClick={() => { setConnectTab('endpoint'); setShowConnect(true) }}
            >
              <Plug className="h-3.5 w-3.5" />
              Connect
            </button>
            <PublishDropdown onPublish={handlePublish} disabled={!hasDraft} />
            <div className="relative" ref={menuRef}>
              <button
                className="rounded-md p-1.5 text-gray-500 hover:bg-gray-100 hover:text-gray-900"
                onClick={() => setShowMenu((v) => !v)}
              >
                <MoreHorizontal className="h-4 w-4" />
              </button>
              {showMenu && (
                <div className="absolute right-0 top-full z-20 mt-1 w-40 rounded-lg border border-gray-200 bg-white py-1 shadow-lg">
                  <button
                    className="flex w-full items-center gap-2 px-3 py-2 text-sm text-red-600 hover:bg-gray-50"
                    onClick={() => handleDelete()}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                    Delete workflow
                  </button>
                </div>
              )}
            </div>
          </div>
        }
      />

      <div className="flex min-h-0 flex-1">
        <div className="flex-1 border-r border-gray-200">
          <Editor
            height="100%"
            language={monacoLanguage}
            value={displayCode}
            onChange={isReadOnly ? undefined : handleCodeChange}
            beforeMount={setupMonacoVelaneTypes}
            options={{
              readOnly: isReadOnly,
              minimap: { enabled: false },
              fontSize: 14,
              lineNumbers: 'on',
              scrollBeyondLastLine: false,
            }}
            theme="vs"
          />
        </div>

        <div className="flex w-96 shrink-0 flex-col bg-white">
          <div className="flex border-b border-gray-200">
            {(['test', 'logs'] as ActiveTab[]).map((tab) => (
              <button
                key={tab}
                className={`px-4 py-2 text-sm font-medium capitalize ${
                  activeTab === tab
                    ? 'border-b-2 border-gray-900 text-gray-900'
                    : 'text-gray-500 hover:text-gray-700'
                }`}
                onClick={() => setActiveTab(tab)}
              >
                {tab}
              </button>
            ))}
          </div>

          {activeTab === 'test' && (
            <div className="flex flex-1 flex-col gap-3 overflow-auto p-4">
              <div>
                <label className="mb-1 block text-xs font-medium text-gray-700">Input JSON</label>
                <textarea
                  className="h-32 w-full rounded-md border border-gray-300 p-2 font-mono text-xs focus:outline-none focus:ring-1 focus:ring-gray-400"
                  value={testInput}
                  onChange={(e) => setTestInput(e.target.value)}
                  spellCheck={false}
                />
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium text-gray-700">Environment</label>
                <select
                  className="w-full rounded-md border border-gray-300 px-2 py-1.5 text-sm focus:outline-none focus:ring-1 focus:ring-gray-400"
                  value={testEnv}
                  onChange={(e) => setTestEnv(e.target.value as 'dev' | 'staging' | 'prod')}
                >
                  <option value="dev">dev</option>
                  <option value="staging">staging</option>
                  <option value="prod">prod</option>
                </select>
              </div>
              {invokeUrl && (
                <div className="rounded-md border border-gray-200 bg-gray-50 px-3 py-2">
                  <p className="mb-1 text-xs font-medium text-gray-500">Endpoint</p>
                  <div className="flex items-center gap-2">
                    <code className="flex-1 truncate text-xs text-gray-700">{invokeUrl}</code>
                    <button
                      className="shrink-0 text-gray-400 hover:text-gray-700"
                      onClick={() => copyToClipboard(invokeUrl, 'endpoint-panel')}
                      title="Copy URL"
                    >
                      {copied === 'endpoint-panel' ? <Check className="h-3.5 w-3.5 text-green-600" /> : <Copy className="h-3.5 w-3.5" />}
                    </button>
                  </div>
                </div>
              )}
              <button
                className="w-full rounded-md bg-gray-900 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
                onClick={handleRun}
                disabled={invoking || !id}
              >
                {invoking ? 'Running...' : '▶ Run'}
              </button>
              {invokeResult && (
                <div
                  className={`rounded-md p-3 ${
                    invokeResult.error ? 'bg-red-950' : 'bg-gray-900'
                  }`}
                >
                  {invokeResult.stderr?.includes("Missing 'default' export") ? (
                    <p className="text-xs text-red-300">
                      Your workflow must export a default function. Add this to your code:
                      <br /><br />
                      <code className="rounded bg-red-900 px-1">export default async function handler(input) {'{ ... }'}</code>
                    </p>
                  ) : (
                    <pre
                      className={`overflow-auto text-xs ${
                        invokeResult.error ? 'text-red-300' : 'text-green-300'
                      }`}
                    >
                      {invokeResult.error
                        ? `Error: ${invokeResult.error}\n${invokeResult.stderr || ''}`.trim()
                        : JSON.stringify(invokeResult.output, null, 2)}
                    </pre>
                  )}
                </div>
              )}
              {runLogs.length > 0 && (
                <div className="rounded-md border border-gray-200 bg-gray-950 p-3">
                  <p className="mb-1.5 text-xs font-medium text-gray-400">
                    Logs {testEnv !== 'dev' && <span className="text-gray-600">(dev only)</span>}
                  </p>
                  <pre className="max-h-48 overflow-auto font-mono text-xs leading-relaxed">
                    {runLogs.map((l, i) => (
                      <div key={i} className={l.stream === 'stderr' ? 'text-amber-300' : 'text-gray-300'}>
                        {l.text}
                      </div>
                    ))}
                  </pre>
                </div>
              )}
            </div>
          )}

          {activeTab === 'logs' && (
            <div className="flex-1 overflow-auto p-3">
              {runLogs.length === 0 ? (
                <div className="flex h-full items-center justify-center text-sm text-gray-500">
                  Run the workflow in <code className="mx-1 rounded bg-gray-100 px-1">dev</code> to see live logs.
                </div>
              ) : (
                <pre className="font-mono text-xs leading-relaxed">
                  {runLogs.map((l, i) => (
                    <div key={i} className={l.stream === 'stderr' ? 'text-amber-600' : 'text-gray-700'}>
                      <span className="select-none text-gray-400">{l.stream === 'stderr' ? 'E ' : '  '}</span>
                      {l.text}
                    </div>
                  ))}
                </pre>
              )}
            </div>
          )}
        </div>
      </div>

      {showConnect && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/30 p-4">
          <div className="w-full max-w-lg rounded-xl border border-gray-200 bg-white shadow-xl">
            {/* Modal header */}
            <div className="flex items-center justify-between border-b border-gray-200 px-5 py-4">
              <div>
                <h2 className="text-base font-semibold text-gray-900">Connect</h2>
                <p className="mt-0.5 text-xs text-gray-500">Call this workflow from your agent or IDE</p>
              </div>
              <button
                className="rounded-md p-1.5 text-gray-400 hover:bg-gray-100"
                onClick={() => setShowConnect(false)}
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            {/* Tabs */}
            <div className="flex border-b border-gray-200 px-5">
              {([
                { key: 'endpoint', label: 'HTTP' },
                { key: 'mcp', label: 'MCP' },
              ] as const).map(({ key, label }) => (
                <button
                  key={key}
                  className={`mr-4 py-2.5 text-sm font-medium ${
                    connectTab === key
                      ? 'border-b-2 border-gray-900 text-gray-900'
                      : 'text-gray-500 hover:text-gray-700'
                  }`}
                  onClick={() => setConnectTab(key)}
                >
                  {label}
                </button>
              ))}
            </div>

            <div className="px-5 py-4">
              {connectTab === 'endpoint' && (
                <div className="space-y-4">
                  <div>
                    <p className="mb-1.5 text-xs font-medium text-gray-700">Invoke URL</p>
                    <div className="flex items-center gap-2 rounded-md border border-gray-200 bg-gray-50 px-3 py-2">
                      <code className="flex-1 truncate text-xs text-gray-800">{invokeUrl}</code>
                      <button onClick={() => copyToClipboard(invokeUrl, 'invoke-url')} className="shrink-0 text-gray-400 hover:text-gray-700">
                        {copied === 'invoke-url' ? <Check className="h-3.5 w-3.5 text-green-600" /> : <Copy className="h-3.5 w-3.5" />}
                      </button>
                    </div>
                  </div>
                  <div>
                    <p className="mb-1.5 text-xs font-medium text-gray-700">Example request</p>
                    <div className="relative rounded-md bg-gray-900 px-4 py-3">
                      <pre className="overflow-x-auto text-xs leading-relaxed text-gray-100">{curlExample}</pre>
                      <button
                        className="absolute right-2 top-2 rounded p-1 text-gray-400 hover:text-gray-200"
                        onClick={() => copyToClipboard(curlExample, 'curl')}
                      >
                        {copied === 'curl' ? <Check className="h-3.5 w-3.5 text-green-400" /> : <Copy className="h-3.5 w-3.5" />}
                      </button>
                    </div>
                  </div>
                  <p className="text-xs text-gray-500">
                    Use <code className="rounded bg-gray-100 px-1">X-Invoke-Mode: async</code> or{' '}
                    <code className="rounded bg-gray-100 px-1">stream</code> headers for async / streaming modes.
                  </p>
                </div>
              )}

              {connectTab === 'mcp' && (
                <div className="space-y-4">
                  <p className="text-xs text-gray-600">
                    Add to your MCP config to connect Claude Code, Cursor, or Codex to Velane.
                  </p>
                  {mcpError && (
                    <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">
                      {mcpError}
                    </div>
                  )}
                  <div className="relative rounded-md bg-gray-900 px-4 py-3">
                    <pre className="overflow-x-auto text-xs leading-relaxed text-gray-100">{claudeConfig}</pre>
                    <button
                      className="absolute right-2 top-2 rounded p-1 text-gray-400 hover:text-gray-200"
                      onClick={() => copyToClipboard(claudeConfig, 'mcp-config')}
                    >
                      {copied === 'mcp-config' ? <Check className="h-3.5 w-3.5 text-green-400" /> : <Copy className="h-3.5 w-3.5" />}
                    </button>
                  </div>
                  <ul className="space-y-0.5 text-xs text-gray-500">
                    <li><span className="font-medium text-gray-700">Claude Code:</span> <code className="rounded bg-gray-100 px-1">~/.claude/mcp.json</code></li>
                    <li><span className="font-medium text-gray-700">Cursor / Codex:</span> <code className="rounded bg-gray-100 px-1">.cursor/mcp.json</code></li>
                  </ul>
                  <p className="text-xs text-gray-500">
                    Replace <code className="rounded bg-gray-100 px-1">vl_YOUR_API_KEY</code> with an API key from{' '}
                    <strong>Settings → API Keys</strong>.
                  </p>
                  <div className="rounded-md border border-gray-200 bg-gray-50 px-3 py-2">
                    <p className="mb-1 text-xs font-medium text-gray-700">Codex config</p>
                    <pre className="overflow-x-auto text-xs leading-relaxed text-gray-700">{codexConfig}</pre>
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {showVersions && (
        <>
          <div
            className="fixed inset-0 z-30 bg-black/20"
            onClick={() => setShowVersions(false)}
          />
          <aside className="fixed inset-y-0 right-0 z-40 flex w-72 flex-col border-l border-gray-200 bg-white shadow-xl">
            <div className="flex items-center justify-between border-b border-gray-200 px-4 py-3">
              <span className="text-sm font-semibold text-gray-900">Versions</span>
              <button
                className="rounded p-1 text-gray-400 hover:bg-gray-100 hover:text-gray-600"
                onClick={() => setShowVersions(false)}
              >
                <X className="h-4 w-4" />
              </button>
            </div>
            {selectedVersion && (
              <div className="border-b border-gray-100 bg-blue-50 px-4 py-2">
                <button
                  className="text-xs font-medium text-blue-700 hover:underline"
                  onClick={() => { setSelectedVersion(null); setShowVersions(false) }}
                >
                  ← Back to latest (editable)
                </button>
              </div>
            )}
            <div className="flex-1 overflow-y-auto">
              {(['dev', 'staging', 'prod'] as const).map((env) => {
                const envData = environments.find((e) => e.env === env)
                const activeNum = envData?.active_version_number ?? null
                const activeVersion = activeNum != null
                  ? versions.find((v) => v.version_number === activeNum) ?? null
                  : null
                const isOpen = openEnvs[env]

                return (
                  <div key={env} className="border-b border-gray-100">
                    <button
                      className="flex w-full items-center justify-between px-4 py-2.5 text-left hover:bg-gray-50"
                      onClick={() => setOpenEnvs((p) => ({ ...p, [env]: !p[env] }))}
                    >
                      <div className="flex items-center gap-2">
                        <ChevronRight
                          className={`h-3.5 w-3.5 text-gray-400 transition-transform ${isOpen ? 'rotate-90' : ''}`}
                        />
                        <span className="text-sm font-medium capitalize text-gray-800">{env}</span>
                      </div>
                      {activeVersion ? (
                        <span className="text-xs text-gray-400">v{activeVersion.version_number}</span>
                      ) : (
                        <span className="text-xs text-gray-300">—</span>
                      )}
                    </button>

                    {isOpen && (
                      <div className="pb-2 pt-0.5">
                        {activeVersion ? (
                          <div className={`mx-3 rounded-md border border-gray-200 px-3 py-2.5 ${selectedVersion?.id === activeVersion.id ? 'bg-gray-50' : ''}`}>
                            <div className="mb-1.5 flex items-center justify-between">
                              <span className="text-xs font-medium text-gray-900">v{activeVersion.version_number}</span>
                              <span className="rounded-full bg-green-100 px-2 py-0.5 text-xs font-medium text-green-700">
                                published
                              </span>
                            </div>
                            <p className="mb-2 text-xs text-gray-400">
                              {new Date(activeVersion.created_at).toLocaleString()}
                            </p>
                            <div className="flex gap-2">
                              <button
                                className="flex-1 rounded border border-gray-300 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50"
                                onClick={() => { setSelectedVersion(selectedVersion?.id === activeVersion.id ? null : activeVersion); setShowVersions(false) }}
                              >
                                {selectedVersion?.id === activeVersion.id ? 'Viewing' : 'View'}
                              </button>
                              <button
                                className="flex-1 rounded border border-gray-300 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50"
                                onClick={async () => {
                                  setCode(activeVersion.code)
                                  setSelectedVersion(null)
                                  setShowVersions(false)
                                  if (id) {
                                    try { await api.createVersion(id, activeVersion.code); await reloadVersions() } catch { /* silent */ }
                                  }
                                }}
                              >
                                Restore
                              </button>
                            </div>
                          </div>
                        ) : (
                          <p className="px-4 pb-1 text-xs text-gray-400">Nothing published yet</p>
                        )}
                      </div>
                    )}
                  </div>
                )
              })}

              {/* Drafts */}
              {(() => {
                const drafts = [...versions].reverse().filter((v) => v.status === 'draft')
                if (drafts.length === 0) return null
                const isOpen = openEnvs['drafts']
                return (
                  <div key="drafts" className="border-b border-gray-100">
                    <button
                      className="flex w-full items-center justify-between px-4 py-2.5 text-left hover:bg-gray-50"
                      onClick={() => setOpenEnvs((p) => ({ ...p, drafts: !p['drafts'] }))}
                    >
                      <div className="flex items-center gap-2">
                        <ChevronRight className={`h-3.5 w-3.5 text-gray-400 transition-transform ${isOpen ? 'rotate-90' : ''}`} />
                        <span className="text-sm font-medium text-gray-800">Drafts</span>
                      </div>
                      <span className="text-xs text-gray-400">{drafts.length}</span>
                    </button>
                    {isOpen && (
                      <div className="space-y-1.5 px-3 pb-2 pt-0.5">
                        {drafts.map((v) => (
                          <div key={v.id} className={`rounded-md border border-gray-200 px-3 py-2.5 ${selectedVersion?.id === v.id ? 'bg-gray-50' : ''}`}>
                            <div className="mb-1.5 flex items-center justify-between">
                              <span className="text-xs font-medium text-gray-900">v{v.version_number}</span>
                              <span className="rounded-full bg-yellow-50 px-2 py-0.5 text-xs font-medium text-yellow-700">draft</span>
                            </div>
                            <p className="mb-2 text-xs text-gray-400">{new Date(v.created_at).toLocaleString()}</p>
                            <div className="flex gap-2">
                              <button
                                className="flex-1 rounded border border-gray-300 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50"
                                onClick={() => { setSelectedVersion(selectedVersion?.id === v.id ? null : v); setShowVersions(false) }}
                              >
                                {selectedVersion?.id === v.id ? 'Viewing' : 'View'}
                              </button>
                              <button
                                className="flex-1 rounded border border-gray-300 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50"
                                onClick={async () => {
                                  setCode(v.code)
                                  setSelectedVersion(null)
                                  setShowVersions(false)
                                  if (id) {
                                    try { await api.createVersion(id, v.code); await reloadVersions() } catch { /* silent */ }
                                  }
                                }}
                              >
                                Restore
                              </button>
                            </div>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                )
              })()}
            </div>
          </aside>
        </>
      )}
    </div>
  )
}

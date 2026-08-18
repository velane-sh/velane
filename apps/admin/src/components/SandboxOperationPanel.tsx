import type { SandboxOperation } from '../types'

export default function SandboxOperationPanel({ operation }: { operation?: SandboxOperation }) {
  if (!operation) return null
  const active = ['queued', 'claimed', 'dispatched', 'waiting'].includes(operation.state)
  return (
    <section className="rounded-lg border border-blue-200 bg-blue-50 px-4 py-3" aria-live="polite">
      <div className="flex items-start justify-between gap-3">
        <div>
          <p className="text-sm font-medium text-blue-900">{active ? `${operation.kind.replace('_', ' ')} in progress` : `${operation.kind.replace('_', ' ')} ${operation.state}`}</p>
          <p className="mt-1 text-xs text-blue-800">Operation <code className="font-mono">{operation.id}</code></p>
        </div>
        <span className="rounded-full bg-white px-2 py-1 text-xs font-medium capitalize text-blue-800">{operation.state}</span>
      </div>
      {operation.failure_message && <p className="mt-2 text-sm text-red-700">{operation.failure_message}</p>}
    </section>
  )
}

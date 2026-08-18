import { RotateCcw, Trash2 } from 'lucide-react'
import type { SandboxSnapshot } from '../types'

function formatBytes(bytes: number) {
  if (!bytes) return '—'
  const gb = bytes / 1024 ** 3
  return `${gb.toFixed(gb < 10 ? 1 : 0)} GB`
}

export default function SandboxSnapshotsPanel({
  snapshots,
  canSnapshot,
  canRestore,
  pending,
  onSnapshot,
  onRestore,
  onDelete,
}: {
  snapshots: SandboxSnapshot[]
  canSnapshot: boolean
  canRestore: boolean
  pending?: boolean
  onSnapshot: () => void
  onRestore: (snapshot: SandboxSnapshot) => void
  onDelete: (snapshot: SandboxSnapshot) => void
}) {
  return (
    <section className="overflow-hidden rounded-lg border border-gray-200 bg-white">
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-gray-200 px-5 py-4">
        <div>
          <h2 className="text-sm font-semibold text-gray-900">Snapshots</h2>
          <p className="mt-1 text-sm text-gray-500">Full recovery points preserve memory and process state, VM/device state, and every mutable disk.</p>
        </div>
        {canSnapshot && <button type="button" disabled={pending} onClick={onSnapshot} className="h-8 rounded-lg border border-gray-300 px-3 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50">Take snapshot</button>}
      </div>
      {snapshots.length === 0 ? (
        <p className="px-5 py-8 text-sm text-gray-500">No recovery points yet.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[650px] text-left text-sm">
            <thead className="bg-gray-50 text-xs font-medium uppercase tracking-wide text-gray-500">
              <tr><th className="px-5 py-3">Snapshot</th><th className="px-4 py-3">State</th><th className="px-4 py-3">Created</th><th className="px-4 py-3">Size</th><th className="px-5 py-3"><span className="sr-only">Actions</span></th></tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {snapshots.map(snapshot => (
                <tr key={snapshot.id}>
                  <td className="px-5 py-3"><p className="font-medium capitalize text-gray-900">{snapshot.kind}</p><code className="text-xs text-gray-500">{snapshot.id}</code></td>
                  <td className="px-4 py-3"><span className={snapshot.state === 'ready' ? 'text-green-700' : snapshot.state === 'failed' ? 'text-red-700' : 'text-amber-800'}>{snapshot.state}</span></td>
                  <td className="px-4 py-3 text-gray-600">{new Date(snapshot.created_at).toLocaleString()}</td>
                  <td className="px-4 py-3 text-gray-600">{formatBytes(snapshot.total_bytes)}</td>
                  <td className="px-5 py-3 text-right"><div className="inline-flex gap-2">
                    <button type="button" disabled={pending || !canRestore || snapshot.state !== 'ready'} onClick={() => onRestore(snapshot)} className="inline-flex items-center gap-1 rounded-lg border border-gray-300 px-2.5 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"><RotateCcw size={13} />Restore</button>
                    <button type="button" disabled={pending} onClick={() => onDelete(snapshot)} className="inline-flex items-center gap-1 rounded-lg border border-red-200 px-2.5 py-1.5 text-xs font-medium text-red-700 hover:bg-red-50 disabled:opacity-50"><Trash2 size={13} />Delete</button>
                  </div></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

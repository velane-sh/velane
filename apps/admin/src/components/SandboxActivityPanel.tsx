import type { SandboxEvent, SandboxLog } from '../types'

export default function SandboxActivityPanel({ events, logs }: { events: SandboxEvent[]; logs: SandboxLog[] }) {
  const items = [
    ...events.map(event => ({ id: `event-${event.id}`, title: event.type.replace(/[._]/g, ' '), message: event.message, createdAt: event.created_at, tone: event.level })),
    ...logs.map(log => ({ id: `log-${log.id}`, title: `${log.source} ${log.stream}`, message: log.message, createdAt: log.created_at, tone: 'info' as const })),
  ].sort((a, b) => new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()).slice(0, 12)

  return (
    <section className="overflow-hidden rounded-lg border border-gray-200 bg-white">
      <div className="border-b border-gray-200 px-5 py-4">
        <h2 className="text-sm font-semibold text-gray-900">Recent activity</h2>
        <p className="mt-1 text-sm text-gray-500">Lifecycle events and bounded system output.</p>
      </div>
      {items.length === 0 ? (
        <p className="px-5 py-8 text-sm text-gray-500">Activity appears here as the sandbox changes state.</p>
      ) : (
        <ol className="divide-y divide-gray-100">
          {items.map(item => (
            <li key={item.id} className="flex gap-3 px-5 py-3">
              <span className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${item.tone === 'error' ? 'bg-red-500' : item.tone === 'warning' ? 'bg-amber-500' : 'bg-green-500'}`} />
              <div className="min-w-0">
                <p className="capitalize text-sm font-medium text-gray-800">{item.title}</p>
                <p className="mt-0.5 break-words text-sm text-gray-500">{item.message}</p>
                <time className="mt-1 block text-xs text-gray-400">{new Date(item.createdAt).toLocaleString()}</time>
              </div>
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}

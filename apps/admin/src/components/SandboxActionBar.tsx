import { Camera, Play, RotateCw, Square } from 'lucide-react'
import type { SandboxOperationKind } from '../types'

const actionConfig: Record<Extract<SandboxOperationKind, 'start' | 'stop' | 'restart' | 'snapshot'>, { label: string; icon: typeof Play; className: string }> = {
  start: { label: 'Start', icon: Play, className: 'border-gray-300 text-gray-700 hover:bg-gray-50' },
  stop: { label: 'Stop', icon: Square, className: 'border-gray-300 text-gray-700 hover:bg-gray-50' },
  restart: { label: 'Restart', icon: RotateCw, className: 'border-gray-300 text-gray-700 hover:bg-gray-50' },
  snapshot: { label: 'Take snapshot', icon: Camera, className: 'border-gray-300 text-gray-700 hover:bg-gray-50' },
}

export default function SandboxActionBar({ availableActions, pending, onAction }: {
  availableActions: SandboxOperationKind[]
  pending?: boolean
  onAction: (action: Extract<SandboxOperationKind, 'start' | 'stop' | 'restart' | 'snapshot'>) => void
}) {
  const actions = availableActions.filter((action): action is Extract<SandboxOperationKind, 'start' | 'stop' | 'restart' | 'snapshot'> => action in actionConfig)
  if (actions.length === 0) return null
  return (
    <div className="flex flex-wrap gap-2">
      {actions.map(action => {
        const { label, icon: Icon, className } = actionConfig[action]
        return <button key={action} type="button" disabled={pending} onClick={() => onAction(action)} className={`flex h-9 items-center gap-2 rounded-lg border px-3 text-sm font-medium disabled:opacity-50 ${className}`}><Icon size={15} />{label}</button>
      })}
    </div>
  )
}

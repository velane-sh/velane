import clsx from 'clsx'
import type { SandboxObservedState } from '../types'

const labels: Record<SandboxObservedState, string> = {
  pending: 'Pending',
  awaiting_capacity: 'Awaiting capacity',
  provisioning: 'Provisioning',
  bootstrapping: 'Bootstrapping',
  restoring: 'Restoring',
  running: 'Running',
  snapshotting: 'Snapshotting',
  stopping: 'Stopping',
  stopped: 'Stopped',
  recovering: 'Recovering',
  deleting: 'Deleting',
  failed: 'Needs attention',
}

const classes: Record<SandboxObservedState, string> = {
  running: 'bg-green-50 text-green-700',
  stopped: 'bg-gray-100 text-gray-700',
  failed: 'bg-red-50 text-red-700',
  awaiting_capacity: 'bg-amber-50 text-amber-800',
  pending: 'bg-blue-50 text-blue-700',
  provisioning: 'bg-blue-50 text-blue-700',
  bootstrapping: 'bg-blue-50 text-blue-700',
  restoring: 'bg-blue-50 text-blue-700',
  snapshotting: 'bg-blue-50 text-blue-700',
  stopping: 'bg-blue-50 text-blue-700',
  recovering: 'bg-blue-50 text-blue-700',
  deleting: 'bg-gray-100 text-gray-700',
}

export function isTransitionalSandboxState(state: SandboxObservedState) {
  return !['running', 'stopped', 'failed'].includes(state)
}

export default function SandboxStatusBadge({ state }: { state: SandboxObservedState }) {
  return <span className={clsx('inline-flex rounded-full px-2.5 py-1 text-xs font-medium', classes[state])}>{labels[state]}</span>
}

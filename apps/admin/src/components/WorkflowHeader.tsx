import { NavLink, useNavigate, useParams } from 'react-router-dom'
import type { Snippet } from '../types'
import LanguageBadge from './LanguageBadge'

interface WorkflowHeaderProps {
  snippet: Snippet | null
  trailing?: React.ReactNode
}

export default function WorkflowHeader({ snippet, trailing }: WorkflowHeaderProps) {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()

  return (
    <header className="shrink-0 border-b border-gray-200 bg-white">
      <div className="flex h-14 items-center justify-between px-4">
        <div className="flex items-center gap-3">
          <button
            className="text-sm text-gray-500 hover:text-gray-900"
            onClick={() => navigate('/dashboard/snippets')}
          >
            &larr; Workflows
          </button>
          <span className="font-medium text-gray-900">{snippet?.name}</span>
          {snippet && <LanguageBadge language={snippet.language} />}
        </div>
        {trailing}
      </div>
      {id && (
        <nav className="flex gap-6 px-4">
          <NavLink
            to={`/dashboard/snippets/${id}`}
            end
            className={({ isActive }) =>
              `border-b-2 py-2 text-sm font-medium ${
                isActive
                  ? 'border-gray-900 text-gray-900'
                  : 'border-transparent text-gray-500 hover:text-gray-700'
              }`
            }
          >
            Editor
          </NavLink>
          <NavLink
            to={`/dashboard/snippets/${id}/executions`}
            className={({ isActive }) =>
              `border-b-2 py-2 text-sm font-medium ${
                isActive
                  ? 'border-gray-900 text-gray-900'
                  : 'border-transparent text-gray-500 hover:text-gray-700'
              }`
            }
          >
            Executions
          </NavLink>
          <NavLink
            to={`/dashboard/snippets/${id}/settings`}
            className={({ isActive }) =>
              `border-b-2 py-2 text-sm font-medium ${
                isActive
                  ? 'border-gray-900 text-gray-900'
                  : 'border-transparent text-gray-500 hover:text-gray-700'
              }`
            }
          >
            Settings
          </NavLink>
        </nav>
      )}
    </header>
  )
}

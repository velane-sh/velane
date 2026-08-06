import { NavLink, Outlet } from 'react-router-dom'
import clsx from 'clsx'

const tabs = [
  { to: '/dashboard/settings/api-keys', label: 'API Keys' },
  { to: '/dashboard/settings/team', label: 'Team' },
  { to: '/dashboard/settings/branding', label: 'Branding' },
  { to: '/dashboard/settings/egress', label: 'Egress Policy' },
  { to: '/dashboard/settings/embed', label: 'Embed' },
  { to: '/dashboard/settings/sso', label: 'Enterprise SSO' },
]

export default function SettingsLayout() {
  return (
    <div>
      <h1 className="mb-5 text-2xl font-bold text-gray-900">Settings</h1>
      <div className="mb-6 flex gap-1 border-b border-gray-200">
        {tabs.map(({ to, label }) => (
          <NavLink
            key={to}
            to={to}
            className={({ isActive }) =>
              clsx(
                '-mb-px border-b-2 px-4 py-2 text-sm font-medium transition-colors',
                isActive
                  ? 'border-gray-900 text-gray-900'
                  : 'border-transparent text-gray-500 hover:text-gray-700',
              )
            }
          >
            {label}
          </NavLink>
        ))}
      </div>
      <Outlet />
    </div>
  )
}

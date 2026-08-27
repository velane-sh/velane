import { NavLink, Outlet } from 'react-router-dom'
import clsx from 'clsx'

const tabs = [
  { to: '/dashboard/settings/api-keys', label: 'API Keys' },
  { to: '/dashboard/settings/team', label: 'Team' },
  { to: '/dashboard/settings/groups', label: 'User Groups' },
  { to: '/dashboard/settings/branding', label: 'Branding' },
  { to: '/dashboard/settings/egress', label: 'Egress Policy' },
  { to: '/dashboard/settings/embed', label: 'Embed' },
  { to: '/dashboard/settings/sso', label: 'Enterprise SSO' },
]

export default function SettingsLayout() {
  return (
    <div>
      <h1 className="mb-5 text-2xl font-bold text-content">Settings</h1>
      <div className="mb-6 flex gap-1 border-b border-line">
        {tabs.map(({ to, label }) => (
          <NavLink
            key={to}
            to={to}
            className={({ isActive }) =>
              clsx(
                '-mb-px border-b-2 px-4 py-2 text-sm font-medium transition-colors',
                isActive
                  ? 'border-accent text-accent'
                  : 'border-transparent text-content-muted hover:text-content',
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

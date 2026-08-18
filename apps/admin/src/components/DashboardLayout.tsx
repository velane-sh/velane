import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom'
import {
  LayoutDashboard,
  Code,
  Plug,
  Terminal,
  Layers3,
  Lock,
  Database,
  Settings,
  CreditCard,
  LogOut,
  Check,
  ChevronsUpDown,
} from 'lucide-react'
import clsx from 'clsx'
import velaneLogo from '../assets/velane_sh.png'
import RouteDocumentTitle from './RouteDocumentTitle'
import { api } from '../lib/api'
import { useEmbedMode } from '../hooks/useEmbedMode'
import { useSessionRefresh } from '../hooks/useSessionRefresh'
import { useInstance } from '../contexts/InstanceContext'
import type { OrgMembership } from '../types'

const allNavItems = [
  { to: '/dashboard/overview', label: 'Overview', icon: LayoutDashboard, embedHidden: false, cloudOnly: false },
  { to: '/dashboard/snippets', label: 'Workflows', icon: Code, embedHidden: false, cloudOnly: false },
  { to: '/dashboard/integrations', label: 'Integrations', icon: Plug, embedHidden: false, cloudOnly: false },
  { to: '/dashboard/mcp', label: 'MCP', icon: Terminal, embedHidden: false, cloudOnly: false },
  { to: '/dashboard/sandboxes', label: 'Sandboxes', icon: Layers3, embedHidden: true, cloudOnly: false, sandboxesOnly: true },
  { to: '/dashboard/variables', label: 'Variables', icon: Lock, embedHidden: false, cloudOnly: false },
  { to: '/dashboard/data-store', label: 'Data Store', icon: Database, embedHidden: true, cloudOnly: false },
  { to: '/dashboard/settings', label: 'Settings', icon: Settings, embedHidden: true, cloudOnly: false },
  { to: '/dashboard/billing', label: 'Billing', icon: CreditCard, embedHidden: true, cloudOnly: true },
]

function slugifyOrgName(value: string) {
  return value
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9\s-]/g, '')
    .replace(/\s+/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-+|-+$/g, '')
}

export default function DashboardLayout() {
  const navigate = useNavigate()
  const location = useLocation()
  const isEmbedMode = useEmbedMode()
  useSessionRefresh()
  const isEditorRoute = /^\/dashboard\/snippets\/.+/.test(location.pathname)
  const { cloud, sandboxesAvailable } = useInstance()
  const [orgs, setOrgs] = useState<OrgMembership[]>([])
  const [orgsLoading, setOrgsLoading] = useState(!isEmbedMode)
  const [orgsError, setOrgsError] = useState('')
  const [activeOrgSlug, setActiveOrgSlug] = useState('')
  const [showOrgSwitcher, setShowOrgSwitcher] = useState(false)
  const [orgName, setOrgName] = useState('')
  const [orgSlug, setOrgSlug] = useState('')
  const [orgSlugTouched, setOrgSlugTouched] = useState(false)
  const [createOrgError, setCreateOrgError] = useState('')
  const [creatingOrg, setCreatingOrg] = useState(false)

  const navItems = allNavItems.filter(item =>
    (!isEmbedMode || !item.embedHidden) && (!item.cloudOnly || cloud) && (!item.sandboxesOnly || sandboxesAvailable)
  )
  const currentOrg = useMemo(
    () => orgs.find(org => org.slug === activeOrgSlug) ?? orgs[0] ?? null,
    [activeOrgSlug, orgs],
  )
  const shouldShowCreateOrgModal = !isEmbedMode && !orgsLoading && !orgsError && orgs.length === 0
  const canRenderContent = isEmbedMode || (!orgsLoading && !orgsError && orgs.length > 0 && activeOrgSlug !== '')

  useEffect(() => {
    setShowOrgSwitcher(false)
  }, [location.pathname, location.search])

  useEffect(() => {
    if (isEmbedMode) {
      setOrgsLoading(false)
      return
    }

    let cancelled = false

    async function loadOrgs() {
      setOrgsLoading(true)
      setOrgsError('')

      try {
        const memberships = await api.listMyOrgs()
        if (cancelled) return

        setOrgs(memberships)
        if (memberships.length === 0) {
          setActiveOrgSlug('')
          return
        }

        try {
          const activeOrg = await api.getActiveOrg()
          const nextSlug = memberships.some(org => org.slug === activeOrg.slug)
            ? activeOrg.slug
            : memberships[0].slug
          setActiveOrgSlug(nextSlug)
        } catch {
          setActiveOrgSlug(memberships[0].slug)
        }
      } catch (err) {
        if (cancelled) return
        setOrgsError(err instanceof Error ? err.message : 'Failed to load orgs')
      } finally {
        if (!cancelled) setOrgsLoading(false)
      }
    }

    loadOrgs()
    return () => {
      cancelled = true
    }
  }, [isEmbedMode])

  const handleLogout = async () => {
    try {
      await api.logout()
    } catch {
      // ignore — proceed with local logout
    }
    localStorage.removeItem('apiKey')
    navigate('/login')
  }

  const handleOrgSwitch = async (slug: string) => {
    try {
      await api.setActiveOrg(slug)
      setActiveOrgSlug(slug)
    } finally {
      setShowOrgSwitcher(false)
    }
  }

  const handleOrgNameChange = (value: string) => {
    setOrgName(value)
    if (!orgSlugTouched) {
      setOrgSlug(slugifyOrgName(value))
    }
  }

  const handleCreateOrg = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault()
    setCreateOrgError('')
    setCreatingOrg(true)

    try {
      const createdOrg = await api.createOrg(orgName.trim(), orgSlug.trim())
      setOrgs([createdOrg])
      await api.setActiveOrg(createdOrg.slug)
      setActiveOrgSlug(createdOrg.slug)
      setOrgName('')
      setOrgSlug('')
      setOrgSlugTouched(false)
    } catch (err) {
      setCreateOrgError(err instanceof Error ? err.message : 'Failed to create org')
    } finally {
      setCreatingOrg(false)
    }
  }

  return (
    <div className="flex h-screen bg-gray-50">
      <RouteDocumentTitle />
      {/* Sidebar */}
      {!isEditorRoute && <aside className="flex w-64 flex-col border-r border-gray-200 bg-white">
        <div className="flex h-14 items-center gap-2.5 border-b border-gray-200 px-5">
          <img
            src={velaneLogo}
            alt="Velane"
            className="h-7 w-7 rounded-full object-contain"
          />
          <span className="text-sm font-semibold text-gray-900">Velane</span>
        </div>

        <nav className="flex-1 overflow-y-auto px-3 py-4">
          {navItems.map(({ to, label, icon: Icon }) => (
            <NavLink
              key={to}
              to={to}
              className={({ isActive }) =>
                clsx(
                  'mb-0.5 flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors',
                  isActive
                    ? 'bg-gray-100 font-medium text-gray-900'
                    : 'font-normal text-gray-500 hover:bg-gray-100 hover:text-gray-800',
                )
              }
            >
              <Icon size={16} />
              {label}
            </NavLink>
          ))}
        </nav>

        {!isEmbedMode && (
          <div className="border-t border-gray-200 p-3">
            {orgs.length > 1 && currentOrg && (
              <div className="relative mb-3">
                <button
                  type="button"
                  onClick={() => setShowOrgSwitcher(open => !open)}
                  className="flex w-full items-center justify-between rounded-lg border border-gray-200 bg-gray-50 px-3 py-2 text-left hover:border-gray-300 hover:bg-white"
                >
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium text-gray-900">{currentOrg.name}</p>
                    <p className="truncate text-xs text-gray-500">{currentOrg.slug}</p>
                  </div>
                  <ChevronsUpDown size={14} className="text-gray-400" />
                </button>
                {showOrgSwitcher && (
                  <div className="absolute bottom-full left-0 right-0 mb-2 overflow-hidden rounded-lg border border-gray-200 bg-white shadow-lg">
                    {orgs.map(org => (
                      <button
                        key={org.tenant_id}
                        type="button"
                        onClick={() => handleOrgSwitch(org.slug)}
                        className="flex w-full items-center justify-between px-3 py-2 text-left text-sm hover:bg-gray-50"
                      >
                        <div className="min-w-0">
                          <p className="truncate font-medium text-gray-900">{org.name}</p>
                          <p className="truncate text-xs text-gray-500">{org.slug}</p>
                        </div>
                        <Check
                          size={14}
                          className={clsx(
                            'ml-3 shrink-0',
                            activeOrgSlug === org.slug ? 'text-gray-900' : 'text-transparent',
                          )}
                        />
                      </button>
                    ))}
                  </div>
                )}
              </div>
            )}
            <button
              onClick={handleLogout}
              className="flex w-full items-center gap-3 rounded-md px-3 py-2 text-sm font-normal text-gray-500 hover:bg-gray-100 hover:text-gray-700"
            >
              <LogOut size={16} />
              Logout
            </button>
          </div>
        )}
      </aside>}

      {/* Main content */}
      <main className={clsx('relative flex-1 overflow-auto', isEditorRoute ? 'flex flex-col p-0' : 'p-8')}>
        {orgsLoading && !isEmbedMode ? (
          <div className="flex h-full items-center justify-center">
            <p className="text-sm text-gray-500">Loading orgs...</p>
          </div>
        ) : orgsError ? (
          <div className="flex h-full items-center justify-center">
            <div className="w-full max-w-md rounded-xl border border-red-200 bg-white p-6 shadow-sm">
              <p className="text-sm font-medium text-red-700">{orgsError}</p>
              <p className="mt-2 text-sm text-gray-500">
                Refresh the page after your session is restored to continue.
              </p>
            </div>
          </div>
        ) : canRenderContent ? (
          <div key={isEmbedMode ? 'embed' : activeOrgSlug} className="h-full">
            <Outlet />
          </div>
        ) : (
          <div className="h-full rounded-2xl border border-dashed border-gray-200 bg-white/70" />
        )}

        {shouldShowCreateOrgModal && (
          <>
            <div className="pointer-events-none absolute inset-0 bg-gray-50/80 backdrop-blur-sm" />
            <div className="absolute inset-0 flex items-center justify-center p-6">
              <div className="w-full max-w-md rounded-2xl border border-gray-200 bg-white p-6 shadow-xl">
                <h2 className="text-xl font-semibold text-gray-900">Create your first org</h2>
                <p className="mt-2 text-sm text-gray-500">
                  You&apos;re signed in, but this account does not belong to any org yet. Create one to unlock the dashboard.
                </p>

                {createOrgError && (
                  <div className="mt-4 rounded-md bg-red-50 p-3 text-sm text-red-700">{createOrgError}</div>
                )}

                <form onSubmit={handleCreateOrg} className="mt-6 space-y-4">
                  <div>
                    <label className="mb-1 block text-sm font-medium text-gray-700">Org name</label>
                    <input
                      type="text"
                      value={orgName}
                      onChange={(e) => handleOrgNameChange(e.target.value)}
                      required
                      className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-gray-400 focus:outline-none"
                      placeholder="Acme"
                    />
                  </div>

                  <div>
                    <label className="mb-1 block text-sm font-medium text-gray-700">Org slug</label>
                    <input
                      type="text"
                      value={orgSlug}
                      onChange={(e) => {
                        setOrgSlugTouched(true)
                        setOrgSlug(slugifyOrgName(e.target.value))
                      }}
                      required
                      className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-gray-400 focus:outline-none"
                      placeholder="acme"
                    />
                    <p className="mt-1 text-xs text-gray-400">
                      Use 3-63 lowercase letters, numbers, or hyphens.
                    </p>
                  </div>

                  <button
                    type="submit"
                    disabled={creatingOrg}
                    className="w-full rounded-lg bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-gray-800 disabled:opacity-50"
                  >
                    {creatingOrg ? 'Creating org...' : 'Create org'}
                  </button>
                </form>
              </div>
            </div>
          </>
        )}
      </main>
    </div>
  )
}

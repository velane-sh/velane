import { useEffect } from 'react'
import { matchPath, useLocation } from 'react-router-dom'
import { setDocumentTitle } from '../lib/documentTitle'

const dashboardRoutes: { path: string; title: string }[] = [
  { path: '/dashboard/snippets/:id/settings', title: 'Workflows' },
  { path: '/dashboard/snippets/:id', title: 'Workflows' },
  { path: '/dashboard/overview', title: 'Overview' },
  { path: '/dashboard/snippets', title: 'Workflows' },
  { path: '/dashboard/integrations', title: 'Integrations' },
  { path: '/dashboard/mcp', title: 'MCP' },
  { path: '/dashboard/variables', title: 'Variables' },
  { path: '/dashboard/data-store', title: 'Data Store' },
  { path: '/dashboard/settings/api-keys', title: 'API Keys' },
  { path: '/dashboard/settings/team', title: 'Team' },
  { path: '/dashboard/settings/groups', title: 'User Groups' },
  { path: '/dashboard/settings/branding', title: 'Branding' },
  { path: '/dashboard/settings/egress', title: 'Egress Policy' },
  { path: '/dashboard/settings/embed', title: 'Embed' },
  { path: '/dashboard/settings', title: 'Settings' },
  { path: '/dashboard/billing', title: 'Billing' },
]

function titleForPath(pathname: string): string | null {
  for (const route of dashboardRoutes) {
    if (matchPath(route.path, pathname)) return route.title
  }
  return null
}

/** Sets document.title from the current dashboard route. Entity pages may override via useDocumentTitle. */
export default function RouteDocumentTitle() {
  const { pathname } = useLocation()

  useEffect(() => {
    const title = titleForPath(pathname)
    if (title) setDocumentTitle(title)
  }, [pathname])

  return null
}

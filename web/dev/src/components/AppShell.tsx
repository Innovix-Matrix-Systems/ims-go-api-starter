import { useQuery } from '@tanstack/react-query'
import { Link, Outlet, useRouterState } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { api, fetchMe, getCachedMe, logoutRedirect } from '../api/client'
import type { SystemInfo } from '../api/types'
import { initials } from '../format'
import { CommandPalette } from './CommandPalette'
import { ErrorBox, Icon, Spinner, type IconName } from './ui'

interface NavItem {
  to: string
  label: string
  icon: IconName
  soon?: boolean
  localOnly?: boolean
}

interface NavGroup {
  section: string
  items: NavItem[]
}
const navigation: NavGroup[] = [
  {
    section: 'Monitor',
    items: [
      { to: '/', label: 'Overview', icon: 'overview' },
      { to: '/requests', label: 'Telescope', icon: 'telescope' },
      { to: '/logs', label: 'Logs', icon: 'logs' },
      { to: '/exceptions', label: 'Exceptions', icon: 'exceptions' },
    ],
  },
  {
    section: 'Operations',
    items: [
      { to: '/queue', label: 'Queue', icon: 'queue' },
      { to: '/backups', label: 'Backups', icon: 'backup' },
      { to: '/commands', label: 'Commands', icon: 'terminal' },
      { to: '/inspector', label: 'Inspector', icon: 'inspector' },
      { to: '/system', label: 'System', icon: 'system' },
      { to: '/actions', label: 'Audit log', icon: 'actions' },
    ],
  },
  {
    section: 'Management',
    items: [
      { to: '/tenants', label: 'Tenants', icon: 'tenants' },
      { to: '/sessions', label: 'Sessions', icon: 'sessions' },
      { to: '/settings', label: 'Settings', icon: 'settings' },
      { to: '/flags', label: 'Feature Flags', icon: 'flags' },
      { to: '/files', label: 'Files', icon: 'files' },
      { to: '/developers', label: 'Developers', icon: 'developers' },
    ],
  },
  {
    section: 'Sandbox',
    items: [{ to: '/playground', label: 'Playground', icon: 'playground', localOnly: true }],
  },
]

const allItems = navigation.flatMap((group) => group.items)

function sectionFor(pathname: string): string {
  if (pathname.startsWith('/jobs')) return 'Queue'
  const match = allItems.filter((item) => (item.to === '/' ? pathname === '/' : pathname.startsWith(item.to)))
  return match.length > 0 ? match[match.length - 1].label : 'Overview'
}

export function AppShell() {
  const pathname = useRouterState({ select: (state) => state.location.pathname })
  const [commandOpen, setCommandOpen] = useState(false)
  const meQuery = useQuery({
    queryKey: ['me'],
    queryFn: fetchMe,
    staleTime: 60_000,
    retry: false,
    placeholderData: getCachedMe() ?? undefined,
  })
  const system = useQuery({
    queryKey: ['system-info'],
    queryFn: ({ signal }) => api<SystemInfo>('/api/v1/system/info', { signal }),
    staleTime: 5 * 60_000,
    retry: false,
  })
  const me = meQuery.data

  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        setCommandOpen((value) => !value)
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [])

  if (meQuery.isError && !me) {
    const message = meQuery.error instanceof Error ? meQuery.error.message : 'Unable to load identity'
    return (
      <div className="login-wrap">
        <div className="login-card card">
          <div className="eyebrow">SUPERADMIN CONTROL</div>
          <h1 className="login-title">Access unavailable</h1>
          <p className="login-sub">{message}</p>
          <ErrorBox message="The developer dashboard requires the dev.view permission. Sign in with a super-admin or developer account." />
          <div className="modal-actions">
            <Link to="/login" className="btn ghost">
              Sign in
            </Link>
            <button type="button" className="btn primary" onClick={logoutRedirect}>
              Sign out
            </button>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-mark">D</div>
          <div>
            <div className="brand-title">Dev Dashboard</div>
            <div className="brand-sub">Superadmin control</div>
          </div>
        </div>
        <nav className="nav">
          {navigation.map((group) => {
            const items = group.items.filter((item) => !item.localOnly || me?.playground)
            if (items.length === 0) return null
            return (
              <div key={group.section}>
                <div className="nav-section">{group.section}</div>
                {items.map((item) => (
                  <Link
                    key={item.to}
                    to={item.to}
                    className="nav-item"
                    activeProps={{ className: 'nav-item active' }}
                    activeOptions={{ exact: item.to === '/' }}
                  >
                    <Icon name={item.icon} />
                    <span>{item.label}</span>
                    {item.soon ? <span className="nav-badge">soon</span> : null}
                  </Link>
                ))}
              </div>
            )
          })}
          <div className="nav-section">Reference</div>
          <a className="nav-item external" href="/docs" target="_blank" rel="noreferrer">
            <Icon name="docs" />
            <span>API Docs</span>
            <Icon name="external" className="icon-inline nav-ext" />
          </a>
        </nav>
        <div className="sidebar-footer">
          <div className="user-card">
            <div className="avatar">{me ? initials(me.name) : <Spinner />}</div>
            <div className="user-meta">
              <div className="user-name">{me?.name ?? 'Developer'}</div>
              <div className="user-email">{me?.email ?? 'loading…'}</div>
            </div>
            <button type="button" className="icon-btn" title="Sign out" onClick={logoutRedirect}>
              <Icon name="logout" />
            </button>
          </div>
          {system.data ? (
            <div className="sidebar-version">
              {system.data.environment} · v{system.data.version}
            </div>
          ) : null}
        </div>
      </aside>
      <div className="main">
        <header className="topbar">
          <div className="crumbs">
            <span>Developer</span>
            <span>/</span>
            <b>{sectionFor(pathname)}</b>
          </div>
          <div className="topbar-right">
            <button type="button" className="command-trigger" onClick={() => setCommandOpen(true)}>
              <Icon name="search" />
              <span className="command-label">Search or jump to…</span>
              <kbd>Ctrl K</kbd>
            </button>
            {system.data ? (
              <>
                <span className="env-pill" title={`Environment: ${system.data.environment}`}>
                  <span className={`dot ${envTone(system.data.environment)}`} /> {system.data.environment}
                </span>
                <span className="env-pill" title="Application version">
                  v{system.data.version}
                </span>
              </>
            ) : null}
            {me?.is_super_admin ? (
              <span className="env-pill">
                <span className="dot" /> Super-admin
              </span>
            ) : (
              <span className="env-pill">
                <span className="dot yellow" /> {me ? 'Developer' : '…'}
              </span>
            )}
          </div>
        </header>
        <main className="page">
          <div key={pathname} className="page-anim">
            <Outlet />
          </div>
        </main>
      </div>
      <CommandPalette open={commandOpen} onOpenChange={setCommandOpen} />
    </div>
  )
}

function envTone(env: string): '' | 'red' | 'yellow' {
  switch (env.toLowerCase()) {
    case 'production':
      return 'red'
    case 'staging':
      return 'yellow'
    default:
      return ''
  }
}


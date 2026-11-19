import { Command } from 'cmdk'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useRouter } from '@tanstack/react-router'
import { useEffect } from 'react'
import { api, fetchMe, getCachedMe, logoutRedirect } from '../api/client'
import type { CursorPage, DevBackupRun, DevCommand, DevException, DevJob, DevRequestSummary } from '../api/types'
import { useToast } from './Toasts'
import { Icon, type IconName } from './ui'
import { formatDuration } from '../format'

const pages = [
  { to: '/', label: 'Overview', icon: 'overview' },
  { to: '/requests', label: 'Telescope — Requests', icon: 'telescope' },
  { to: '/logs', label: 'Logs', icon: 'logs' },
  { to: '/exceptions', label: 'Exceptions', icon: 'exceptions' },
  { to: '/queue', label: 'Queue', icon: 'queue' },
  { to: '/backups', label: 'Backups', icon: 'backup' },
  { to: '/commands', label: 'Commands', icon: 'terminal' },
  { to: '/inspector', label: 'Inspector', icon: 'inspector' },
  { to: '/actions', label: 'Audit log', icon: 'actions' },
  { to: '/system', label: 'System health', icon: 'system' },
  { to: '/tenants', label: 'Tenants', icon: 'tenants' },
  { to: '/sessions', label: 'Sessions', icon: 'sessions' },
  { to: '/settings', label: 'Settings', icon: 'settings' },
  { to: '/flags', label: 'Feature flags', icon: 'flags' },
  { to: '/files', label: 'Files', icon: 'files' },
  { to: '/developers', label: 'Developers', icon: 'developers' },
  { to: '/playground', label: 'Playground', icon: 'playground', localOnly: true },
] as const satisfies ReadonlyArray<{ to: string; label: string; icon: IconName; localOnly?: boolean }>

const inspectorTabs = [
  { tab: 'routes', label: 'Inspector — Routes', icon: 'route' },
  { tab: 'access', label: 'Inspector — Roles & permissions', icon: 'shield' },
  { tab: 'config', label: 'Inspector — Config', icon: 'settings' },
  { tab: 'packages', label: 'Inspector — Packages', icon: 'package' },
] as const

const links = [
  { label: 'API docs (Scalar)', url: '/docs' },
  { label: 'Health endpoint', url: '/api/health' },
  { label: 'Liveness endpoint', url: '/up' },
  { label: 'Prometheus metrics', url: '/metrics' },
] as const

export function CommandPalette({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const navigate = useNavigate()
  const router = useRouter()
  const queryClient = useQueryClient()
  const { push } = useToast()
  const me = useQuery({
    queryKey: ['me'],
    queryFn: fetchMe,
    staleTime: 60_000,
    retry: false,
    placeholderData: getCachedMe() ?? undefined,
  })

  const requests = useQuery({
    queryKey: ['commands', 'requests'],
    queryFn: ({ signal }) =>
      api<CursorPage<DevRequestSummary>>('/dev/api/requests', {
        query: { limit: 8 },
        signal,
        unwrap: false,
      }),
    enabled: open,
    staleTime: 5_000,
  })
  const jobs = useQuery({
    queryKey: ['commands', 'jobs'],
    queryFn: ({ signal }) =>
      api<CursorPage<DevJob>>('/dev/api/queue/jobs', {
        query: { limit: 8 },
        signal,
        unwrap: false,
      }),
    enabled: open,
    staleTime: 5_000,
    retry: false,
  })
  const commands = useQuery({
    queryKey: ['commands', 'list'],
    queryFn: ({ signal }) => api<DevCommand[]>('/dev/api/commands', { signal }),
    enabled: open,
    staleTime: 30_000,
  })
  const exceptions = useQuery({
    queryKey: ['commands', 'exceptions'],
    queryFn: ({ signal }) =>
      api<CursorPage<DevException>>('/dev/api/exceptions', {
        query: { limit: 5 },
        signal,
        unwrap: false,
      }),
    enabled: open,
    staleTime: 5_000,
  })

  useEffect(() => {
    if (!open) return
    const handler = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onOpenChange(false)
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [open, onOpenChange])

  if (!open) return null

  const close = () => onOpenChange(false)
  const select = (action: () => void) => {
    action()
    close()
  }

  const simulate = async (logs: number, level: 'warn' | 'error', exception: boolean) => {
    try {
      const result = await api<{ request_id: string; logs: number; exception: boolean }>(
        '/dev/api/simulate-events',
        { method: 'POST', body: { logs, level, exception } },
      )
      push(
        `Generated ${result.logs} ${level} log${result.logs === 1 ? '' : 's'}${result.exception ? ' + exception' : ''}`,
        'success',
      )
      for (const key of ['logs', 'exceptions', 'actions', 'overview']) {
        void queryClient.invalidateQueries({ queryKey: [key] })
      }
      close()
    } catch (error) {
      push(error instanceof Error ? error.message : 'Simulation failed', 'error')
    }
  }

  const createBackup = async () => {
    try {
      const run = await api<DevBackupRun>('/dev/api/backups', { method: 'POST' })
      push(`Backup #${run.id} ${run.status}`, run.status === 'completed' ? 'success' : 'info')
      for (const key of ['backups', 'overview', 'actions']) {
        void queryClient.invalidateQueries({ queryKey: [key] })
      }
      close()
    } catch (error) {
      push(error instanceof Error ? error.message : 'Unable to create a backup', 'error')
    }
  }

  return (
    <div className="command-overlay" role="presentation" onClick={close}>
      <div className="command-dialog" onClick={(event) => event.stopPropagation()}>
        <Command label="Command center" shouldFilter loop>
          <div className="command-input-wrap">
            <Icon name="search" />
            <Command.Input
              autoFocus
              className="command-input"
              placeholder="Search pages, requests, jobs, actions…"
            />
            <kbd>esc</kbd>
          </div>
          <Command.List className="command-list">
            <Command.Empty className="command-empty">
              No results. Try a path, job id, or page name.
            </Command.Empty>

            <Command.Group heading="Pages">
              {pages.filter((page) => !('localOnly' in page && page.localOnly) || me.data?.playground).map((page) => (
                <Command.Item
                  key={page.to}
                  value={`${page.label} ${page.to}`}
                  className="command-item"
                  onSelect={() => select(() => void navigate({ to: page.to }))}
                >
                  <Icon name={page.icon} />
                  {page.label}
                  <span className="command-hint">{page.to}</span>
                </Command.Item>
              ))}
            </Command.Group>

            <Command.Group heading="Inspector">
              {inspectorTabs.map((item) => (
                <Command.Item
                  key={item.tab}
                  value={`${item.label} inspect`}
                  className="command-item"
                  onSelect={() => select(() => void navigate({ to: '/inspector', search: { tab: item.tab } }))}
                >
                  <Icon name={item.icon} />
                  {item.label}
                </Command.Item>
              ))}
            </Command.Group>

            {(commands.data?.length ?? 0) > 0 ? (
              <Command.Group heading="Run a command">
                {commands.data?.map((command) => (
                  <Command.Item
                    key={command.name}
                    value={`run ${command.name} ${command.label} ${command.description}`}
                    className="command-item"
                    onSelect={() => select(() => void navigate({ to: '/commands', search: { run: command.name } }))}
                  >
                    <Icon name="terminal" />
                    {command.label}
                    <span className="command-hint mono">{command.name}</span>
                  </Command.Item>
                ))}
              </Command.Group>
            ) : null}

            <Command.Group heading="Links">
              {links.map((link) => (
                <Command.Item
                  key={link.url}
                  value={`${link.label} ${link.url}`}
                  className="command-item"
                  onSelect={() => select(() => window.open(link.url, '_blank', 'noopener'))}
                >
                  <Icon name="external" />
                  {link.label}
                  <span className="command-hint">{link.url}</span>
                </Command.Item>
              ))}
            </Command.Group>

            <Command.Group heading="Actions">
              <Command.Item
                value="generate warning logs telemetry test"
                className="command-item"
                onSelect={() => select(() => void simulate(3, 'warn', false))}
              >
                <Icon name="logs" /> Generate 3 warning logs
              </Command.Item>
              <Command.Item
                value="generate error logs telemetry test"
                className="command-item"
                onSelect={() => select(() => void simulate(3, 'error', false))}
              >
                <Icon name="logs" /> Generate 3 error logs
              </Command.Item>
              <Command.Item
                value="generate exception telemetry test"
                className="command-item"
                onSelect={() => select(() => void simulate(1, 'error', true))}
              >
                <Icon name="alert" /> Generate log + exception
              </Command.Item>
              <Command.Item
                value="create backup database archive"
                className="command-item"
                onSelect={() => select(() => void createBackup())}
              >
                <Icon name="backup" /> Create database backup
              </Command.Item>
              <Command.Item
                value="refresh reload data"
                className="command-item"
                onSelect={() =>
                  select(() => {
                    void queryClient.invalidateQueries()
                    void router.invalidate()
                    push('Views refreshed', 'info')
                  })
                }
              >
                <Icon name="refresh" /> Refresh all data
              </Command.Item>
              <Command.Item
                value="sign out logout"
                className="command-item"
                onSelect={() => select(logoutRedirect)}
              >
                <Icon name="logout" /> Sign out
              </Command.Item>
            </Command.Group>

            {(requests.data?.data.length ?? 0) > 0 ? (
              <Command.Group heading="Recent requests">
                {requests.data?.data.map((request) => (
                  <Command.Item
                    key={request.id}
                    value={`request ${request.method} ${request.path} ${request.status}`}
                    className="command-item"
                    onSelect={() =>
                      select(() =>
                        void navigate({ to: '/requests/$id', params: { id: String(request.id) } }),
                      )
                    }
                  >
                    <span className={`method ${request.method}`}>{request.method}</span>
                    <span className="truncate">{request.path}</span>
                    <span className="command-hint">
                      {request.status} · {formatDuration(request.duration_ms)}
                    </span>
                  </Command.Item>
                ))}
              </Command.Group>
            ) : null}

            {(jobs.data?.data.length ?? 0) > 0 ? (
              <Command.Group heading="Queue jobs">
                {jobs.data?.data.map((job) => (
                  <Command.Item
                    key={job.id}
                    value={`job ${job.id} ${job.kind} ${job.queue} ${job.state}`}
                    className="command-item"
                    onSelect={() =>
                      select(() => void navigate({ to: '/jobs/$id', params: { id: String(job.id) } }))
                    }
                  >
                    <Icon name="queue" />
                    <span className="truncate">
                      #{job.id} {job.kind}
                    </span>
                    <span className="command-hint">{job.state}</span>
                  </Command.Item>
                ))}
              </Command.Group>
            ) : null}

            {(exceptions.data?.data.length ?? 0) > 0 ? (
              <Command.Group heading="Recent exceptions">
                {exceptions.data?.data.map((exception) => (
                  <Command.Item
                    key={exception.id}
                    value={`exception ${exception.class} ${exception.message}`}
                    className="command-item"
                    onSelect={() =>
                      select(() =>
                        void navigate({ to: '/exceptions/$id', params: { id: String(exception.id) } }),
                      )
                    }
                  >
                    <Icon name="exceptions" />
                    <span className="truncate">{exception.message}</span>
                    <span className="command-hint">{exception.class}</span>
                  </Command.Item>
                ))}
              </Command.Group>
            ) : null}
          </Command.List>
          <div className="command-footer">
            <span><kbd>↑</kbd><kbd>↓</kbd> navigate</span>
            <span><kbd>↵</kbd> open</span>
            <span><kbd>esc</kbd> close</span>
          </div>
        </Command>
      </div>
    </div>
  )
}

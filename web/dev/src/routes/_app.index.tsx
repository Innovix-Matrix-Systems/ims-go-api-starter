import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { useMemo } from 'react'
import { api } from '../api/client'
import type { CursorPage, DevAction, DevException, DevQueue, DevRequestSummary } from '../api/types'
import {
  Card,
  CardHeader,
  Chip,
  EmptyState,
  ErrorBox,
  Icon,
  LoadingBlock,
  MethodBadge,
  PageHeader,
  StatCard,
  StatusChip,
} from '../components/ui'
import { formatBytes, formatDuration, relativeTime, shortRequestID, statusTone } from '../format'

export const Route = createFileRoute('/_app/')({
  component: OverviewPage,
})

function OverviewPage() {
  const navigate = useNavigate()
  const requests = useQuery({
    queryKey: ['overview', 'requests'],
    queryFn: ({ signal }) =>
      api<CursorPage<DevRequestSummary>>('/dev/api/requests', { query: { limit: 8 }, signal, unwrap: false }),
    refetchInterval: 15_000,
  })
  const queues = useQuery({
    queryKey: ['queues'],
    queryFn: ({ signal }) => api<DevQueue[]>('/dev/api/queue/queues', { signal }),
    refetchInterval: 15_000,
    retry: false,
  })
  const exceptions = useQuery({
    queryKey: ['overview', 'exceptions'],
    queryFn: ({ signal }) =>
      api<CursorPage<DevException>>('/dev/api/exceptions', { query: { limit: 2 }, signal, unwrap: false }),
    refetchInterval: 15_000,
  })
  const actions = useQuery({
    queryKey: ['overview', 'actions'],
    queryFn: ({ signal }) =>
      api<CursorPage<DevAction>>('/dev/api/actions', { query: { limit: 5 }, signal, unwrap: false }),
    refetchInterval: 15_000,
  })

  const totals = useMemo(() => {
    const rows = queues.data ?? []
    return {
      queues: rows.length,
      active: rows.reduce((sum, queue) => sum + queue.active, 0),
      failed: rows.reduce((sum, queue) => sum + queue.failed, 0),
    }
  }, [queues.data])

  const recentRequests = requests.data?.data ?? []
  const recentExceptions = exceptions.data?.data ?? []
  const recentActions = actions.data?.data ?? []

  return (
    <>
      <PageHeader
        eyebrow="Superadmin control"
        title="Overview"
        subtitle="Live operational view of the API: traffic, queue health and recent exceptions."
        actions={
          <Link to="/requests" search={{ request_id: undefined }} className="btn">
            <Icon name="telescope" /> Open Telescope
          </Link>
        }
      />

      <div className="grid cols-4">
        <StatCard label="Active jobs" value={totals.active} tone="accent" hint={`${totals.queues} queues`} />
        <StatCard label="Failed jobs" value={totals.failed} tone={totals.failed > 0 ? 'red' : 'green'} hint="discarded in River" />
        <StatCard label="Recent exceptions" value={recentExceptions.length} tone={recentExceptions.length > 0 ? 'red' : undefined} hint="latest page" />
        <StatCard label="Recent commands" value={recentActions.length} hint="audited actions" />
      </div>

      <div className="row mt-24">
        <div style={{ flex: 2 }}>
          <Card tight>
            <CardHeader
              title="Recent requests"
              sub="Captured HTTP traffic, newest first"
              actions={
                <Link to="/requests" search={{ request_id: undefined }} className="btn ghost sm">
                  View all <Icon name="chevron" className="icon-inline" />
                </Link>
              }
            />
            {requests.isLoading ? (
              <LoadingBlock />
            ) : requests.isError ? (
              <ErrorBox message="Unable to load captured requests." />
            ) : recentRequests.length === 0 ? (
              <EmptyState
                title="No requests captured yet"
                description="Traffic appears here once DEV_DASHBOARD_ENABLED is on and requests hit the API."
              />
            ) : (
              <div className="table-wrap">
                <table className="table">
                  <tbody>
                    {recentRequests.map((request) => (
                      <tr
                        key={request.id}
                        className="clickable"
                        onClick={() => void navigate({ to: '/requests/$id', params: { id: String(request.id) } })}
                      >
                        <td className="nowrap dim small" style={{ width: 90 }}>{relativeTime(request.created_at)}</td>
                        <td style={{ width: 60 }}><MethodBadge method={request.method} /></td>
                        <td className="path">{request.path}</td>
                        <td style={{ width: 70 }}><StatusChip status={request.status} /></td>
                        <td className="num right nowrap" style={{ width: 80 }}>{formatDuration(request.duration_ms)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>
        </div>

        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: 14 }}>
          <Card tight>
            <CardHeader
              title="Queues"
              sub="River depth summary"
              actions={
                <Link to="/queue" className="btn ghost sm">
                  Manage <Icon name="chevron" className="icon-inline" />
                </Link>
              }
            />
            {queues.isLoading ? (
              <LoadingBlock label="Loading queues…" />
            ) : queues.isError ? (
              <ErrorBox message="Queue statistics unavailable (driver may be sync)." />
            ) : (queues.data ?? []).length === 0 ? (
              <EmptyState title="No queues" description="No River queues have reported jobs yet." icon="queue" />
            ) : (
              <div className="stack-tight">
                {(queues.data ?? []).map((queue) => (
                  <div key={queue.name} className="flex" style={{ padding: '6px 0' }}>
                    <span className="queue-name">{queue.name}</span>
                    {queue.paused_at ? <Chip tone="yellow">paused</Chip> : null}
                    <span className="spacer" />
                    <span className="small muted">{queue.total.toLocaleString()} total</span>
                    <Chip tone={queue.failed > 0 ? 'red' : 'gray'}>{queue.failed} failed</Chip>
                  </div>
                ))}
              </div>
            )}
          </Card>

          <Card tight>
            <CardHeader
              title="Recent exceptions"
              actions={
                <Link to="/exceptions" className="btn ghost sm">
                  View all <Icon name="chevron" className="icon-inline" />
                </Link>
              }
            />
            {recentExceptions.length === 0 ? (
              <div className="muted small" style={{ padding: '8px 0' }}>No exceptions captured.</div>
            ) : (
              <div className="stack-tight">
                {recentExceptions.slice(0, 2).map((exception) => (
                  <Link
                    key={exception.id}
                    to="/exceptions/$id"
                    params={{ id: String(exception.id) }}
                    className="stack-tight"
                    style={{ padding: '6px 0' }}
                  >
                    <div className="flex">
                      <Chip tone="red">{exception.class}</Chip>
                      <span className="spacer" />
                      <span className="small dim">{relativeTime(exception.created_at)}</span>
                    </div>
                    <div className="small truncate" style={{ color: 'var(--text-soft)' }}>{exception.message}</div>
                  </Link>
                ))}
              </div>
            )}
          </Card>
        </div>
      </div>

      <div className="row mt-24">
        <div style={{ flex: 1 }}>
          <Card tight>
            <CardHeader title="Latest commands" actions={<Link to="/actions" className="btn ghost sm">Audit log</Link>} />
            {recentActions.length === 0 ? (
              <div className="muted small" style={{ padding: '8px 0' }}>No audited actions yet.</div>
            ) : (
              <div className="stack-tight">
                {recentActions.map((action) => (
                  <div key={action.id} className="flex" style={{ padding: '5px 0' }}>
                    <span className="mono small">{action.action}</span>
                    {action.target ? <span className="dim small truncate">{action.target}</span> : null}
                    <span className="spacer" />
                    <Chip tone={action.status === 'ok' ? 'green' : 'red'}>{action.status}</Chip>
                    <span className="small dim">{relativeTime(action.created_at)}</span>
                  </div>
                ))}
              </div>
            )}
          </Card>
        </div>
        <div style={{ flex: 1 }}>
          <Card tight>
            <CardHeader title="Recent request detail" sub="Jump straight into the inspector" />
            {recentRequests.length === 0 ? (
              <div className="muted small" style={{ padding: '8px 0' }}>No captured requests yet.</div>
            ) : (
              <div className="stack-tight">
                {recentRequests.slice(0, 3).map((request) => (
                  <Link
                    key={request.id}
                    to="/requests/$id"
                    params={{ id: String(request.id) }}
                    className="flex"
                    style={{ padding: '6px 0' }}
                  >
                    <MethodBadge method={request.method} />
                    <span className="mono small truncate">{request.path}</span>
                    <span className="spacer" />
                    <Chip tone={statusTone(request.status) as any}>{request.status}</Chip>
                    <span className="dim small mono">{shortRequestID(request.request_id)}</span>
                    <span className="dim small">{formatBytes(request.response_bytes)}</span>
                  </Link>
                ))}
              </div>
            )}
          </Card>
        </div>
      </div>
    </>
  )
}


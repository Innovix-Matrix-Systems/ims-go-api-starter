import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useMemo, useState } from 'react'
import { api } from '../api/client'
import type { DevRequestDetail } from '../api/types'
import {
  Button,
  Card,
  CardHeader,
  Chip,
  CodeBlock,
  CopyButton,
  EmptyState,
  ErrorBox,
  Icon,
  JsonView,
  LoadingBlock,
  MethodBadge,
  PageHeader,
  StatusChip,
  Tabs,
} from '../components/ui'
import { formatBytes, formatDateTime, formatDuration, relativeTime } from '../format'

export const Route = createFileRoute('/_app/requests/$id')({
  component: RequestDetailPage,
})

type Tab = 'overview' | 'sql' | 'cache' | 'queue' | 'logs' | 'exceptions'

function RequestDetailPage() {
  const { id } = Route.useParams()
  const [tab, setTab] = useState<Tab>('overview')
  const query = useQuery({
    queryKey: ['request', id],
    queryFn: ({ signal }) => api<DevRequestDetail>(`/dev/api/requests/${id}`, { signal }),
  })

  const detail = query.data
  const queries = detail?.queries ?? []
  const cacheEvents = detail?.cache_events ?? []
  const queueEvents = detail?.queue_events ?? []
  const logs = detail?.logs ?? []
  const exceptions = detail?.exceptions ?? []
  const counts = useMemo(
    () => ({
      sql: queries.length,
      cache: cacheEvents.length,
      queue: queueEvents.length,
      logs: logs.length,
      exceptions: exceptions.length,
    }),
    [queries, cacheEvents, queueEvents, logs, exceptions],
  )

  if (query.isLoading) return <LoadingBlock label="Loading request…" />
  if (query.isError) {
    return <ErrorBox message={query.error instanceof Error ? query.error.message : 'Unable to load request.'} />
  }
  if (!detail) return <EmptyState title="Request not found" />

  const request = detail.request

  return (
    <>
      <div className="mb-8">
        <Link to="/requests" search={{ request_id: undefined }} className="btn ghost sm">
          <Icon name="chevron" style={{ transform: 'rotate(180deg)' }} /> Back to requests
        </Link>
      </div>
      <PageHeader
        eyebrow={`Request #${request.id} · ${relativeTime(request.created_at)}`}
        title={`${request.method} ${request.path}`}
        subtitle={request.route && request.route !== request.path ? `Route pattern: ${request.route}` : undefined}
        actions={
          <>
            <CopyButton value={request.request_id} label="Request ID" />
            <Button icon="refresh" onClick={() => void query.refetch()} disabled={query.isFetching}>
              Refresh
            </Button>
          </>
        }
      />

      <div className="grid cols-4 mb-16">
        <div className="stat-card">
          <div className="stat-label">Status</div>
          <div className="stat-value" style={{ fontSize: 22 }}>
            <span className="flex"><StatusChip status={request.status} /><span className="mono small">{request.status}</span></span>
          </div>
        </div>
        <div className="stat-card">
          <div className="stat-label">Duration</div>
          <div className="stat-value" style={{ fontSize: 22 }}>{formatDuration(request.duration_ms)}</div>
          <div className="stat-hint">{formatDateTime(request.created_at)}</div>
        </div>
        <div className="stat-card">
          <div className="stat-label">Payload</div>
          <div className="stat-value" style={{ fontSize: 22 }}>
            {formatBytes(request.request_bytes)} <span className="dim" style={{ fontSize: 14 }}>→</span> {formatBytes(request.response_bytes)}
          </div>
          <div className="stat-hint">request → response</div>
        </div>
        <div className="stat-card">
          <div className="stat-label">Correlation</div>
          <div className="stat-value" style={{ fontSize: 16, marginTop: 10 }}>
            <span className="flex">
              <span className="mono truncate" title={request.request_id}>{request.request_id}</span>
              <CopyButton value={request.request_id} />
            </span>
          </div>
          <div className="stat-hint">{counts.sql} SQL · {counts.logs} logs · {counts.exceptions} errors</div>
        </div>
      </div>

      <Tabs
        active={tab}
        onChange={setTab}
        tabs={[
          { id: 'overview', label: 'Overview' },
          { id: 'sql', label: 'SQL', count: counts.sql },
          { id: 'cache', label: 'Cache', count: counts.cache },
          { id: 'queue', label: 'Queue', count: counts.queue },
          { id: 'logs', label: 'Logs', count: counts.logs },
          { id: 'exceptions', label: 'Exceptions', count: counts.exceptions },
        ]}
      />

      {tab === 'overview' ? (
        <div className="row">
          <div style={{ flex: 1 }}>
            <Card tight>
              <CardHeader title="Request metadata" />
              <dl className="kv">
                <dt>Method</dt><dd><MethodBadge method={request.method} /></dd>
                <dt>Path</dt><dd>{request.path}</dd>
                <dt>Route</dt><dd>{request.route || '—'}</dd>
                <dt>Status</dt><dd>{request.status}</dd>
                <dt>Duration</dt><dd>{formatDuration(request.duration_ms)}</dd>
                <dt>IP</dt><dd>{request.ip ?? '—'}</dd>
                <dt>User agent</dt><dd>{request.user_agent ?? '—'}</dd>
                <dt>User</dt><dd>{request.user_id ?? '—'}</dd>
                <dt>Tenant / branch</dt><dd>{request.tenant_id ?? '—'} / {request.branch_id ?? '—'}</dd>
                <dt>Captured</dt><dd>{formatDateTime(request.created_at)}</dd>
              </dl>
              <div className="divider" />
              <CardHeader title="Headers" sub="Sensitive values redacted before persistence" />
              <JsonView value={request.headers} label="Headers" />
            </Card>
          </div>
          <div style={{ flex: 1 }} className="stack">
            <Card tight>
              <CardHeader title="Request body" />
              <JsonView value={request.request_body} label="Request body" />
            </Card>
            <Card tight>
              <CardHeader title="Response body" />
              <JsonView value={request.response_body} label="Response body" />
            </Card>
          </div>
        </div>
      ) : null}

      {tab === 'sql' ? (
        <Card tight>
          {queries.length === 0 ? (
            <EmptyState title="No SQL captured" description="This request executed no statements, or SQL capture is disabled." icon="requests" />
          ) : (
            queries.map((item) => (
              <div className="query-row" key={item.id}>
                <CodeBlock code={item.sql} label="SQL" />
                <div className="query-meta">
                  <span>{formatDuration(item.duration_ms)}</span>
                  <span>{item.row_count} rows</span>
                  {item.error ? <Chip tone="red">error</Chip> : null}
                  <span className="spacer" />
                  <span>{formatDateTime(item.created_at)}</span>
                </div>
                {item.args ? (
                  <div className="mt-8">
                    <JsonView value={item.args} label="Args" maxHeight={220} />
                  </div>
                ) : null}
                {item.error ? <div className="error-box mt-8">{item.error}</div> : null}
              </div>
            ))
          )}
        </Card>
      ) : null}

      {tab === 'cache' ? (
        <Card tight>
          {cacheEvents.length === 0 ? (
            <EmptyState title="No cache operations" icon="activity" />
          ) : (
            <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr><th>Operation</th><th>Key</th><th>Hit</th><th>Duration</th><th>Time</th></tr>
                </thead>
                <tbody>
                  {cacheEvents.map((event) => (
                    <tr key={event.id}>
                      <td><Chip tone="purple">{event.operation}</Chip></td>
                      <td className="mono small">{event.key || '—'}</td>
                      <td>{event.hit === null || event.hit === undefined ? '—' : event.hit ? <Chip tone="green">hit</Chip> : <Chip tone="yellow">miss</Chip>}</td>
                      <td className="num small">{formatDuration(event.duration_ms)}</td>
                      <td className="small dim">{relativeTime(event.created_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      ) : null}

      {tab === 'queue' ? (
        <Card tight>
          {queueEvents.length === 0 ? (
            <EmptyState title="No queue events" icon="queue" />
          ) : (
            <div className="table-wrap">
              <table className="table">
                <thead>
                  <tr><th>Event</th><th>Queue</th><th>Kind</th><th>Job</th><th>Duration</th><th>Time</th><th /></tr>
                </thead>
                <tbody>
                  {queueEvents.map((event) => (
                    <tr key={event.id}>
                      <td><Chip tone={event.event === 'failed' ? 'red' : 'green'}>{event.event}</Chip></td>
                      <td className="mono small">{event.queue}</td>
                      <td className="mono small">{event.kind}</td>
                      <td className="num small">{event.job_id ?? '—'}</td>
                      <td className="num small">{formatDuration(event.duration_ms)}</td>
                      <td className="small dim">{relativeTime(event.created_at)}</td>
                      <td className="right">
                        {event.job_id ? (
                          <Link
                            to="/jobs/$id"
                            params={{ id: String(event.job_id) }}
                            className="btn ghost sm"
                            title="Open job details"
                          >
                            <Icon name="eye" />
                          </Link>
                        ) : (
                          <span
                            className="dim"
                            title="Job id was not captured for this event (captured before job-id support)"
                          >
                            —
                          </span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Card>
      ) : null}

      {tab === 'logs' ? (
        <Card tight>
          {logs.length === 0 ? (
            <EmptyState title="No logs captured" description="Logs are captured at Warn level and above." icon="logs" />
          ) : (
            <div className="stack-tight">
              {logs.map((log) => (
                <div key={log.id} className="log-row" style={{ borderBottom: '1px solid var(--border)' }}>
                  <span className="log-time">{relativeTime(log.created_at)}</span>
                  <Chip tone={log.level.toLowerCase() === 'error' ? 'red' : 'yellow'}>{log.level}</Chip>
                  <span className="log-msg">{log.message}</span>
                  <span className="small dim mono truncate log-device">{log.request_id ?? ''}</span>
                  <Link to="/logs/$id" params={{ id: String(log.id) }} className="btn ghost sm" title="Open log">
                    <Icon name="eye" />
                  </Link>
                </div>
              ))}
            </div>
          )}
        </Card>
      ) : null}

      {tab === 'exceptions' ? (
        <Card tight>
          {exceptions.length === 0 ? (
            <EmptyState title="No exceptions" icon="exceptions" />
          ) : (
            <div className="stack">
              {exceptions.map((exception) => (
                <div key={exception.id} className="query-row">
                  <div className="flex">
                    <Chip tone="red">{exception.class}</Chip>
                    <span className="small dim">{formatDateTime(exception.created_at)}</span>
                    <span className="spacer" />
                    <Link
                      to="/exceptions/$id"
                      params={{ id: String(exception.id) }}
                      className="btn ghost sm"
                      title="Open exception"
                    >
                      <Icon name="eye" />
                    </Link>
                  </div>
                  <div className="mt-8" style={{ color: 'var(--text-soft)' }}>{exception.message}</div>
                  {exception.stack ? <CodeBlock code={exception.stack} label="Stack trace" maxHeight={260} /> : null}
                </div>
              ))}
            </div>
          )}
        </Card>
      ) : null}
    </>
  )
}



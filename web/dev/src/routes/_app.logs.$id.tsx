import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { api } from '../api/client'
import type { DevLog } from '../api/types'
import {
  Button,
  Card,
  CardHeader,
  Chip,
  CodeBlock,
  EmptyState,
  ErrorBox,
  Icon,
  JsonView,
  LoadingBlock,
  PageHeader,
} from '../components/ui'
import { formatDateTime, levelTone, relativeTime } from '../format'

export const Route = createFileRoute('/_app/logs/$id')({
  component: LogDetailPage,
})

function LogDetailPage() {
  const { id } = Route.useParams()
  const log = useQuery({
    queryKey: ['log', id],
    queryFn: ({ signal }) => api<DevLog>(`/dev/api/logs/${id}`, { signal }),
  })

  if (log.isLoading) return <LoadingBlock label="Loading log…" />
  if (log.isError) return <ErrorBox message={log.error instanceof Error ? log.error.message : 'Unable to load log.'} />
  if (!log.data) return <EmptyState title="Log not found" icon="logs" />

  const item = log.data

  return (
    <>
      <div className="mb-8">
        <Link to="/logs" className="btn ghost sm">
          <Icon name="chevron" style={{ transform: 'rotate(180deg)' }} /> Back to logs
        </Link>
      </div>
      <PageHeader
        eyebrow={`Log #${item.id} · ${relativeTime(item.created_at)}`}
        title={item.level}
        subtitle={item.message.slice(0, 140)}
        actions={
          <>
            {item.request_id ? (
              <Link to="/requests" search={{ request_id: item.request_id }} className="btn">
                <Icon name="telescope" /> Open request
              </Link>
            ) : null}
            <Button icon="refresh" onClick={() => void log.refetch()} disabled={log.isFetching}>
              Refresh
            </Button>
          </>
        }
      />

      <div className="row">
        <div style={{ flex: 1 }}>
          <Card tight>
            <CardHeader title="Log entry" />
            <dl className="kv">
              <dt>Level</dt><dd><Chip tone={levelTone(item.level)}>{item.level}</Chip></dd>
              <dt>Captured</dt><dd>{formatDateTime(item.created_at)}</dd>
              <dt>Request</dt><dd>{item.request_id ?? '—'}</dd>
            </dl>
            <div className="divider" />
            <CodeBlock code={item.message} label="Message" />
          </Card>
        </div>
        <div style={{ flex: 1 }}>
          <Card tight>
            <CardHeader title="Attributes" sub="Redacted before persistence" />
            <JsonView value={item.attributes} label="Attributes" />
          </Card>
        </div>
      </div>
    </>
  )
}

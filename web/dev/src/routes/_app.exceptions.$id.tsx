import { useQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { api } from '../api/client'
import type { DevException } from '../api/types'
import {
  Button,
  Card,
  CardHeader,
  Chip,
  CodeBlock,
  EmptyState,
  ErrorBox,
  Icon,
  LoadingBlock,
  PageHeader,
} from '../components/ui'
import { formatDateTime, relativeTime } from '../format'

export const Route = createFileRoute('/_app/exceptions/$id')({
  component: ExceptionDetailPage,
})

function ExceptionDetailPage() {
  const { id } = Route.useParams()
  const exception = useQuery({
    queryKey: ['exception', id],
    queryFn: ({ signal }) => api<DevException>(`/dev/api/exceptions/${id}`, { signal }),
  })

  if (exception.isLoading) return <LoadingBlock label="Loading exception…" />
  if (exception.isError) {
    return <ErrorBox message={exception.error instanceof Error ? exception.error.message : 'Unable to load exception.'} />
  }
  if (!exception.data) return <EmptyState title="Exception not found" icon="exceptions" />

  const item = exception.data

  return (
    <>
      <div className="mb-8">
        <Link to="/exceptions" className="btn ghost sm">
          <Icon name="chevron" style={{ transform: 'rotate(180deg)' }} /> Back to exceptions
        </Link>
      </div>
      <PageHeader
        eyebrow={`Exception #${item.id} · ${relativeTime(item.created_at)}`}
        title={item.class}
        subtitle={item.message}
        actions={
          <>
            {item.request_id ? (
              <Link to="/requests" search={{ request_id: item.request_id }} className="btn">
                <Icon name="telescope" /> Open request
              </Link>
            ) : null}
            <Button icon="refresh" onClick={() => void exception.refetch()} disabled={exception.isFetching}>
              Refresh
            </Button>
          </>
        }
      />

      <div className="stack">
        <Card tight>
          <CardHeader title="Exception" />
          <div className="flex mb-8">
            <Chip tone="red">{item.class}</Chip>
            <span className="spacer" />
            <span className="small dim">{formatDateTime(item.created_at)}</span>
          </div>
          <CodeBlock code={item.message} label="Message" />
        </Card>
        {item.stack ? (
          <Card tight>
            <CardHeader title="Stack trace" />
            <CodeBlock code={item.stack} label="Stack trace" maxHeight={520} />
          </Card>
        ) : null}
      </div>
    </>
  )
}

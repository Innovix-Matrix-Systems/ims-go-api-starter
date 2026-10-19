import { useInfiniteQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useMemo, useState } from 'react'
import { api } from '../api/client'
import type { CursorPage, DevException } from '../api/types'
import { Button, Card, Chip, EmptyState, ErrorBox, Icon, JsonView, LoadingBlock, PageHeader, RefreshToggle } from '../components/ui'
import { formatDateTime, relativeTime } from '../format'

export const Route = createFileRoute('/_app/exceptions/')({
  component: ExceptionsPage,
})

function ExceptionsPage() {
  const [live, setLive] = useState(false)
  const query = useInfiniteQuery({
    queryKey: ['exceptions'],
    queryFn: ({ pageParam, signal }) =>
      api<CursorPage<DevException>>('/dev/api/exceptions', {
        query: { limit: 25, cursor: pageParam || undefined },
        signal,
        unwrap: false,
      }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.meta?.next_cursor || undefined,
    refetchInterval: live ? 5000 : false,
  })
  const rows = useMemo(() => query.data?.pages.flatMap((page) => page.data) ?? [], [query.data])

  return (
    <>
      <PageHeader
        eyebrow="Observability"
        title="Exceptions"
        subtitle="Panics and captured server exceptions with stack traces, correlated to their request."
        actions={
          <>
            <RefreshToggle enabled={live} onChange={setLive} />
            <Button icon="refresh" onClick={() => void query.refetch()} disabled={query.isFetching}>
              Refresh
            </Button>
          </>
        }
      />

      {query.isLoading ? (
        <LoadingBlock label="Loading exceptions…" />
      ) : query.isError ? (
        <ErrorBox message={query.error instanceof Error ? query.error.message : 'Unable to load exceptions.'} />
      ) : rows.length === 0 ? (
        <Card>
          <EmptyState
            title="No exceptions captured"
            description="A clean run — panics and server exceptions will appear here with their stack trace. Generate a synthetic exception to verify the pipeline."
            icon="exceptions"
            action={
              <Link to="/actions" className="btn">
                <Icon name="alert" /> Generate an exception
              </Link>
            }
          />
        </Card>
      ) : (
        <div className="stack">
          {rows.map((exception) => (
            <Card tight key={exception.id}>
              <div className="flex">
                <Chip tone="red">{exception.class}</Chip>
                <span className="small dim">{formatDateTime(exception.created_at)}</span>
                {exception.request_id ? (
                  <Link
                    to="/requests"
                    search={{ request_id: exception.request_id }}
                    className="small link mono"
                  >
                    req {exception.request_id.slice(0, 10)}
                  </Link>
                ) : null}
                <span className="spacer" />
                <span className="small dim">{relativeTime(exception.created_at)}</span>
                <Link
                  to="/exceptions/$id"
                  params={{ id: String(exception.id) }}
                  className="btn ghost sm"
                  title="Open exception"
                >
                  <Icon name="eye" />
                </Link>
              </div>
              <div className="mt-8" style={{ color: 'var(--text-soft)', whiteSpace: 'pre-wrap' }}>{exception.message}</div>
              {exception.stack ? (
                <details className="mt-8">
                  <summary className="small muted" style={{ cursor: 'pointer' }}>Stack trace</summary>
                  <div className="mt-8">
                    <JsonView value={exception.stack} label="Stack trace" maxHeight={320} />
                  </div>
                </details>
              ) : null}
            </Card>
          ))}
          {query.hasNextPage ? (
            <Button onClick={() => void query.fetchNextPage()} disabled={query.isFetchingNextPage}>
              {query.isFetchingNextPage ? 'Loading…' : 'Load more'}
            </Button>
          ) : null}
        </div>
      )}
    </>
  )
}

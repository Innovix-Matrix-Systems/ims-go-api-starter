import { useInfiniteQuery } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useMemo, useRef, useState } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { api } from '../api/client'
import type { CursorPage, DevLog } from '../api/types'
import { Button, Chip, EmptyState, ErrorBox, Icon, LoadingBlock, PageHeader, RefreshToggle } from '../components/ui'
import { formatTime, relativeTime } from '../format'

export const Route = createFileRoute('/_app/logs/')({
  component: LogsPage,
})

const levels = ['', 'warn', 'error'] as const

function LogsPage() {
  const [level, setLevel] = useState('')
  const [live, setLive] = useState(false)
  const scrollRef = useRef<HTMLDivElement>(null)

  const query = useInfiniteQuery({
    queryKey: ['logs', { level }],
    queryFn: ({ pageParam, signal }) =>
      api<CursorPage<DevLog>>('/dev/api/logs', {
        query: { limit: 50, cursor: pageParam || undefined, level: level || undefined },
        signal,
        unwrap: false,
      }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.meta?.next_cursor || undefined,
    refetchInterval: live ? 5000 : false,
  })

  const rows = useMemo(() => query.data?.pages.flatMap((page) => page.data) ?? [], [query.data])

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 44,
    overscan: 12,
  })

  return (
    <>
      <PageHeader
        eyebrow="Observability"
        title="Logs"
        subtitle="Correlated application logs captured at Warn level and above, newest first."
        actions={
          <>
            <RefreshToggle enabled={live} onChange={setLive} />
            <Button icon="refresh" onClick={() => void query.refetch()} disabled={query.isFetching}>
              Refresh
            </Button>
          </>
        }
      />

      <div className="filters">
        <div className="pill-toggle">
          {levels.map((item) => (
            <button
              key={item || 'all'}
              className={level === item ? 'active' : ''}
              onClick={() => setLevel(item)}
              type="button"
            >
              {item ? item.toUpperCase() : 'All'}
            </button>
          ))}
        </div>
        <div className="spacer" />
        <span className="small dim">{rows.length} entries loaded</span>
      </div>

      {query.isLoading ? (
        <LoadingBlock label="Loading logs…" />
      ) : query.isError ? (
        <ErrorBox message={query.error instanceof Error ? query.error.message : 'Unable to load logs.'} />
      ) : rows.length === 0 ? (
        <div className="log-list">
          <EmptyState
            title="No logs captured yet"
            description="Warn- and error-level logs appear here with their request correlation id. Generate a few samples to see the pipeline end to end."
            icon="logs"
            action={
              <Link to="/actions" className="btn">
                <Icon name="terminal" /> Generate sample logs
              </Link>
            }
          />
        </div>
      ) : (
        <div className="log-list">
          <div
            className="log-scroll"
            ref={scrollRef}
            onScroll={(event) => {
              const target = event.currentTarget
              if (target.scrollHeight - target.scrollTop - target.clientHeight < 400) {
                if (query.hasNextPage && !query.isFetchingNextPage) void query.fetchNextPage()
              }
            }}
          >
            <div style={{ height: virtualizer.getTotalSize(), position: 'relative' }}>
              {virtualizer.getVirtualItems().map((virtualRow) => {
                const log = rows[virtualRow.index]
                if (!log) return null
                return (
                  <div
                    key={log.id}
                    className="log-row"
                    style={{
                      position: 'absolute',
                      top: 0,
                      left: 0,
                      width: '100%',
                      transform: `translateY(${virtualRow.start}px)`,
                    }}
                  >
                    <span className="log-time" title={log.created_at}>{formatTime(log.created_at)}</span>
                    <Chip tone={log.level.toLowerCase() === 'error' ? 'red' : 'yellow'}>{log.level}</Chip>
                    <span className="log-msg">
                      {log.message}
                      {log.attributes ? (
                        <span className="attrs"> {compactAttributes(log.attributes)}</span>
                      ) : null}
                    </span>
                    <span className="small dim mono truncate log-device" title={log.request_id ?? ''}>
                      {log.request_id ? `req ${log.request_id.slice(0, 10)}` : relativeTime(log.created_at)}
                    </span>
                    <Link to="/logs/$id" params={{ id: String(log.id) }} className="btn ghost sm" title="Open log">
                      <Icon name="eye" />
                    </Link>
                  </div>
                )
              })}
            </div>
          </div>
        </div>
      )}
    </>
  )
}

function compactAttributes(attributes: unknown): string {
  try {
    const text = JSON.stringify(attributes)
    return text === '{}' ? '' : text.slice(0, 180)
  } catch {
    return ''
  }
}

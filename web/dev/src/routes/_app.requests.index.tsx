import { keepPreviousData, useInfiniteQuery } from '@tanstack/react-query'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useDeferredValue, useMemo, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import { api } from '../api/client'
import type { CursorPage, DevRequestSummary } from '../api/types'
import { DataTable } from '../components/DataTable'
import {
  Button,
  EmptyState,
  ErrorBox,
  Icon,
  LoadingBlock,
  MethodBadge,
  PageHeader,
  RefreshToggle,
  StatusChip,
} from '../components/ui'
import { formatBytes, formatDuration, relativeTime } from '../format'

export const Route = createFileRoute('/_app/requests/')({
  validateSearch: (search: Record<string, unknown>) => ({
    request_id: typeof search.request_id === 'string' ? search.request_id : undefined,
  }),
  component: RequestsPage,
})

function RequestsPage() {
  const navigate = useNavigate()
  const search = Route.useSearch()
  const [searchText, setSearchText] = useState('')
  const [requestID, setRequestID] = useState(search.request_id ?? '')
  const [method, setMethod] = useState('')
  const [status, setStatus] = useState('')
  const [live, setLive] = useState(false)
  const deferredSearch = useDeferredValue(searchText)

  const query = useInfiniteQuery({
    queryKey: ['requests', { search: deferredSearch, method, status, requestID }],
    queryFn: ({ pageParam, signal }) =>
      api<CursorPage<DevRequestSummary>>('/dev/api/requests', {
        query: {
          limit: 25,
          cursor: pageParam || undefined,
          search: deferredSearch || undefined,
          method: method || undefined,
          status: status || undefined,
          request_id: requestID || undefined,
        },
        signal,
        unwrap: false,
      }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.meta?.next_cursor || undefined,
    placeholderData: keepPreviousData,
    refetchInterval: live ? 5000 : false,
  })

  const rows = useMemo(() => query.data?.pages.flatMap((page) => page.data) ?? [], [query.data])
  const maxDuration = useMemo(() => Math.max(1, ...rows.map((row) => row.duration_ms)), [rows])

  const columns = useMemo<ColumnDef<DevRequestSummary, any>[]>(
    () => [
      {
        id: 'time',
        header: 'Time',
        accessorFn: (row) => row.created_at,
        cell: ({ row }) => (
          <span className="nowrap small dim" title={row.original.created_at}>
            {relativeTime(row.original.created_at)}
          </span>
        ),
      },
      {
        id: 'method',
        header: 'Method',
        accessorFn: (row) => row.method,
        cell: ({ row }) => <MethodBadge method={row.original.method} />,
      },
      {
        id: 'path',
        header: 'Path',
        accessorFn: (row) => row.path,
        cell: ({ row }) => (
          <div>
            <div className="path" title={row.original.path}>{row.original.path}</div>
            {row.original.route && row.original.route !== row.original.path ? (
              <div className="small dim mono">{row.original.route}</div>
            ) : null}
          </div>
        ),
      },
      {
        id: 'status',
        header: 'Status',
        accessorFn: (row) => row.status,
        cell: ({ row }) => <StatusChip status={row.original.status} />,
      },
      {
        id: 'duration',
        header: 'Duration',
        accessorFn: (row) => row.duration_ms,
        cell: ({ row }) => (
          <div style={{ minWidth: 72 }}>
            <span className="num small">{formatDuration(row.original.duration_ms)}</span>
            <div className="duration-bar">
              <span style={{ width: `${Math.min(100, (row.original.duration_ms / maxDuration) * 100)}%` }} />
            </div>
          </div>
        ),
      },
      {
        id: 'user',
        header: 'User',
        accessorFn: (row) => row.user_id ?? 0,
        cell: ({ row }) => (
          <span className="small muted">{row.original.user_id ?? '—'}</span>
        ),
      },
      {
        id: 'tenant',
        header: 'Tenant',
        accessorFn: (row) => row.tenant_id ?? 0,
        cell: ({ row }) => <span className="small muted">{row.original.tenant_id ?? '—'}</span>,
      },
      {
        id: 'bytes',
        header: 'Res',
        accessorFn: (row) => row.response_bytes,
        cell: ({ row }) => <span className="small dim nowrap">{formatBytes(row.original.response_bytes)}</span>,
      },
    ],
    [maxDuration],
  )

  return (
    <>
      <PageHeader
        eyebrow="Telescope"
        title="Requests"
        subtitle="Every captured HTTP request with correlated SQL, cache, queue events, logs and exceptions."
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
        <label className="field grow">
          <span className="field-label">Search path</span>
          <input
            className="input mono"
            placeholder="/api/v1/..."
            value={searchText}
            onChange={(event) => setSearchText(event.target.value)}
          />
        </label>
        <label className="field">
          <span className="field-label">Request ID</span>
          <input
            className="input mono"
            placeholder="exact correlation id"
            value={requestID}
            onChange={(event) => setRequestID(event.target.value)}
          />
        </label>
        <label className="field">
          <span className="field-label">Method</span>
          <select className="select" value={method} onChange={(event) => setMethod(event.target.value)}>
            <option value="">Any</option>
            <option value="GET">GET</option>
            <option value="POST">POST</option>
            <option value="PATCH">PATCH</option>
            <option value="PUT">PUT</option>
            <option value="DELETE">DELETE</option>
          </select>
        </label>
        <label className="field">
          <span className="field-label">Status</span>
          <input
            className="input"
            inputMode="numeric"
            placeholder="200"
            value={status}
            onChange={(event) => setStatus(event.target.value.replace(/\D/g, '').slice(0, 3))}
          />
        </label>
        <div className="spacer" />
        <span className="small dim">{rows.length} captured</span>
      </div>

      {query.isLoading ? (
        <LoadingBlock label="Loading captured requests…" />
      ) : query.isError ? (
        <ErrorBox message={query.error instanceof Error ? query.error.message : 'Unable to load requests.'} />
      ) : (
        <>
          <DataTable
            data={rows}
            columns={columns}
            onRowClick={(row) => void navigate({ to: '/requests/$id', params: { id: String(row.id) } })}
            empty={
              <EmptyState
                title="No requests match"
                description="Adjust the filters, or generate some traffic against the API."
                icon="telescope"
              />
            }
          />
          <div className="flex mt-16">
            {query.hasNextPage ? (
              <Button
                icon="chevron"
                onClick={() => void query.fetchNextPage()}
                disabled={query.isFetchingNextPage}
              >
                {query.isFetchingNextPage ? 'Loading…' : 'Load more'}
              </Button>
            ) : (
              <span className="small dim">{rowCountLabel(rows.length)}</span>
            )}
            {query.isFetching && !query.isFetchingNextPage ? (
              <span className="small dim flex"><span className="spinner" /> refreshing</span>
            ) : null}
          </div>
        </>
      )}
    </>
  )
}

function rowCountLabel(count: number): string {
  return count === 0 ? 'No rows' : `${count} row${count === 1 ? '' : 's'} loaded`
}

import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { useMemo, useState } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import { api } from '../api/client'
import type { CursorPage, DevAction } from '../api/types'
import { DataTable } from '../components/DataTable'
import { useToast } from '../components/Toasts'
import {
  Button,
  Card,
  CardHeader,
  Chip,
  Drawer,
  EmptyState,
  ErrorBox,
  Icon,
  JsonView,
  LoadingBlock,
  PageHeader,
  RefreshToggle,
} from '../components/ui'
import { formatDuration, relativeTime } from '../format'

export const Route = createFileRoute('/_app/actions')({
  component: ActionsPage,
})

function ActionsPage() {
  const [live, setLive] = useState(false)
  const [selected, setSelected] = useState<DevAction | null>(null)
  const [simulating, setSimulating] = useState(false)
  const queryClient = useQueryClient()
  const { push } = useToast()

  const simulate = async (logs: number, level: 'warn' | 'error', exception: boolean) => {
    setSimulating(true)
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
    } catch (error) {
      push(error instanceof Error ? error.message : 'Simulation failed', 'error')
    } finally {
      setSimulating(false)
    }
  }

  const query = useInfiniteQuery({
    queryKey: ['actions'],
    queryFn: ({ pageParam, signal }) =>
      api<CursorPage<DevAction>>('/dev/api/actions', {
        query: { limit: 25, cursor: pageParam || undefined },
        signal,
        unwrap: false,
      }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.meta?.next_cursor || undefined,
    refetchInterval: live ? 5000 : false,
  })

  const rows = useMemo(() => query.data?.pages.flatMap((page) => page.data) ?? [], [query.data])

  const columns = useMemo<ColumnDef<DevAction, any>[]>(
    () => [
      {
        id: 'time',
        header: 'Time',
        accessorFn: (row) => row.created_at,
        cell: ({ row }) => <span className="small dim nowrap">{relativeTime(row.original.created_at)}</span>,
      },
      {
        id: 'action',
        header: 'Action',
        accessorFn: (row) => row.action,
        cell: ({ row }) => <span className="mono small">{row.original.action}</span>,
      },
      {
        id: 'target',
        header: 'Target',
        accessorFn: (row) => row.target ?? '',
        cell: ({ row }) => <span className="small muted truncate" style={{ maxWidth: 260 }}>{row.original.target ?? '—'}</span>,
      },
      {
        id: 'status',
        header: 'Status',
        accessorFn: (row) => row.status,
        cell: ({ row }) => <Chip tone={row.original.status === 'ok' ? 'green' : 'red'}>{row.original.status}</Chip>,
      },
      {
        id: 'duration',
        header: 'Duration',
        accessorFn: (row) => row.duration_ms ?? 0,
        cell: ({ row }) => <span className="num small">{formatDuration(row.original.duration_ms)}</span>,
      },
      {
        id: 'actor',
        header: 'Actor',
        accessorFn: (row) => row.actor_user_id,
        cell: ({ row }) => <span className="small muted">user {row.original.actor_user_id}</span>,
      },
    ],
    [],
  )

  return (
    <>
      <PageHeader
        eyebrow="Operations"
        title="Commands"
        subtitle="Audited developer actions: queue mutations, cache clears, syncs and telemetry pruning."
        actions={
          <>
            <RefreshToggle enabled={live} onChange={setLive} />
            <Button icon="refresh" onClick={() => void query.refetch()} disabled={query.isFetching}>
              Refresh
            </Button>
          </>
        }
      />

      <Card tight className="mb-16">
        <CardHeader
          title="Test telemetry"
          sub="Generate synthetic entries to verify the Logs and Exceptions screens."
          actions={<Icon name="terminal" className="icon-inline" />}
        />
        <div className="flex" style={{ flexWrap: 'wrap' }}>
          <Button icon="logs" disabled={simulating} onClick={() => void simulate(3, 'warn', false)}>
            3 warning logs
          </Button>
          <Button icon="logs" disabled={simulating} onClick={() => void simulate(3, 'error', false)}>
            3 error logs
          </Button>
          <Button
            variant="danger"
            icon="alert"
            disabled={simulating}
            onClick={() => void simulate(1, 'error', true)}
          >
            Log + exception
          </Button>
        </div>
        <div className="small dim mt-8">
          Entries are correlated with the request id of this call and appear immediately in Logs,
          Exceptions, and the Commands audit log.
        </div>
      </Card>

      {query.isLoading ? (
        <LoadingBlock label="Loading audit log…" />
      ) : query.isError ? (
        <ErrorBox message={query.error instanceof Error ? query.error.message : 'Unable to load actions.'} />
      ) : (
        <>
          <DataTable
            data={rows}
            columns={columns}
            onRowClick={setSelected}
            empty={<EmptyState title="No audited actions yet" description="Every mutating dashboard action lands here with its result." icon="actions" />}
          />
          <div className="flex mt-16">
            {query.hasNextPage ? (
              <Button icon="chevron" onClick={() => void query.fetchNextPage()} disabled={query.isFetchingNextPage}>
                {query.isFetchingNextPage ? 'Loading…' : 'Load more'}
              </Button>
            ) : (
              <span className="small dim">{rows.length} actions loaded</span>
            )}
          </div>
        </>
      )}

      <Drawer
        open={selected !== null}
        onClose={() => setSelected(null)}
        title={selected?.action ?? 'Action'}
        subtitle={selected ? relativeTime(selected.created_at) : undefined}
      >
        {selected ? (
          <div className="stack">
            <div className="flex">
              <Chip tone={selected.status === 'ok' ? 'green' : 'red'}>{selected.status}</Chip>
              <span className="small dim">actor user {selected.actor_user_id}</span>
              <span className="spacer" />
              <span className="small dim">{formatDuration(selected.duration_ms)}</span>
            </div>
            {selected.error_message ? <ErrorBox message={selected.error_message} /> : null}
            <div>
              <div className="card-sub mb-8">Target</div>
              <div className="mono small">{selected.target ?? '—'}</div>
            </div>
            <div>
              <div className="card-sub mb-8">Params</div>
              <JsonView value={selected.params} label="Params" />
            </div>
          </div>
        ) : null}
      </Drawer>
    </>
  )
}


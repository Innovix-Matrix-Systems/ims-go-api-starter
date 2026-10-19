import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useMemo, useState, type MouseEvent } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import { api } from '../api/client'
import type { CursorPage, DevJob, DevQueue } from '../api/types'
import { DataTable } from '../components/DataTable'
import { useToast } from '../components/Toasts'
import {
  Button,
  Card,
  CardHeader,
  Chip,
  CodeBlock,
  ConfirmDialog,
  Drawer,
  EmptyState,
  ErrorBox,
  Icon,
  JsonView,
  LoadingBlock,
  PageHeader,
  RefreshToggle,
  StatCard,
} from '../components/ui'
import { formatDateTime, formatDuration, relativeTime, stateTone } from '../format'

export const Route = createFileRoute('/_app/queue')({
  component: QueuePage,
})

const states = ['', 'available', 'running', 'scheduled', 'retryable', 'pending', 'completed', 'discarded', 'cancelled']

function canRetry(state: string): boolean {
  return state !== 'running'
}

function canCancel(state: string): boolean {
  return ['available', 'scheduled', 'retryable', 'pending', 'running'].includes(state)
}

function canDelete(state: string): boolean {
  return state !== 'running'
}

interface ConfirmState {
  title: string
  description: string
  confirmLabel: string
  danger?: boolean
  run: () => Promise<unknown>
}

function QueuePage() {
  const { push } = useToast()
  const queryClient = useQueryClient()
  const [queueFilter, setQueueFilter] = useState('')
  const [stateFilter, setStateFilter] = useState('')
  const [kind, setKind] = useState('')
  const [live, setLive] = useState(false)
  const [selectedJob, setSelectedJob] = useState<number | null>(null)
  const [confirm, setConfirm] = useState<ConfirmState | null>(null)
  const [confirmBusy, setConfirmBusy] = useState(false)

  const queues = useQuery({
    queryKey: ['queues'],
    queryFn: ({ signal }) => api<DevQueue[]>('/dev/api/queue/queues', { signal }),
    refetchInterval: live ? 5000 : false,
    retry: false,
  })

  const jobs = useInfiniteQuery({
    queryKey: ['jobs', { queueFilter, stateFilter, kind }],
    queryFn: ({ pageParam, signal }) =>
      api<CursorPage<DevJob>>('/dev/api/queue/jobs', {
        query: {
          limit: 25,
          cursor: pageParam || undefined,
          queue: queueFilter || undefined,
          state: stateFilter || undefined,
          kind: kind || undefined,
        },
        signal,
        unwrap: false,
      }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.meta?.next_cursor || undefined,
    refetchInterval: live ? 5000 : false,
  })

  const jobDetail = useQuery({
    queryKey: ['job', selectedJob],
    queryFn: ({ signal }) => api<DevJob>(`/dev/api/queue/jobs/${selectedJob}`, { signal }),
    enabled: selectedJob !== null,
  })

  const rows = useMemo(() => jobs.data?.pages.flatMap((page) => page.data) ?? [], [jobs.data])
  const queueRows = queues.data ?? []
  const totals = useMemo(
    () => ({
      active: queueRows.reduce((sum, queue) => sum + queue.active, 0),
      failed: queueRows.reduce((sum, queue) => sum + queue.failed, 0),
      total: queueRows.reduce((sum, queue) => sum + queue.total, 0),
    }),
    [queueRows],
  )

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['queues'] })
    void queryClient.invalidateQueries({ queryKey: ['jobs'] })
    void queryClient.invalidateQueries({ queryKey: ['job'] })
    void queryClient.invalidateQueries({ queryKey: ['actions'] })
    void queryClient.invalidateQueries({ queryKey: ['overview'] })
  }

  const queueAction = useMutation({
    mutationFn: ({ name, action }: { name: string; action: 'pause' | 'resume' }) =>
      api(`/dev/api/queue/queues/${encodeURIComponent(name)}/${action}`, { method: 'POST' }),
    onSuccess: (_result, variables) => {
      push(`Queue ${variables.name} ${variables.action === 'pause' ? 'paused' : 'resumed'}`, 'success')
      invalidate()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Queue action failed', 'error'),
  })

  const jobAction = useMutation({
    mutationFn: ({ id, action }: { id: number; action: 'retry' | 'cancel' | 'delete' }) => {
      if (action === 'delete') return api(`/dev/api/queue/jobs/${id}`, { method: 'DELETE' })
      return api(`/dev/api/queue/jobs/${id}/${action}`, { method: 'POST' })
    },
    onSuccess: (_result, variables) => {
      push(`Job #${variables.id} ${variables.action}`, 'success')
      invalidate()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Job action failed', 'error'),
    onSettled: invalidate,
  })

  const askJob = (event: MouseEvent, job: DevJob, action: 'retry' | 'cancel' | 'delete') => {
    event.stopPropagation()
    const labels: Record<typeof action, string> = {
      retry: 'Retry job',
      cancel: 'Cancel job',
      delete: 'Delete job',
    }
    const descriptions: Record<typeof action, string> = {
      retry: `Job #${job.id} becomes immediately available for work.`,
      cancel: `Job #${job.id} will not run again.`,
      delete: `Job #${job.id} is removed permanently. Running jobs cannot be deleted.`,
    }
    setConfirm({
      title: labels[action],
      description: descriptions[action],
      confirmLabel: labels[action],
      danger: action !== 'retry',
      run: () => jobAction.mutateAsync({ id: job.id, action }),
    })
  }

  const columns = useMemo<ColumnDef<DevJob, any>[]>(
    () => [
      {
        id: 'id',
        header: 'Job',
        accessorFn: (row) => row.id,
        cell: ({ row }) => <span className="mono small">#{row.original.id}</span>,
      },
      {
        id: 'kind',
        header: 'Kind',
        accessorFn: (row) => row.kind,
        cell: ({ row }) => <span className="mono small">{row.original.kind}</span>,
      },
      {
        id: 'queue',
        header: 'Queue',
        accessorFn: (row) => row.queue,
        cell: ({ row }) => <Chip tone="purple">{row.original.queue}</Chip>,
      },
      {
        id: 'state',
        header: 'State',
        accessorFn: (row) => row.state,
        cell: ({ row }) => <Chip tone={stateTone(row.original.state)}>{row.original.state}</Chip>,
      },
      {
        id: 'attempt',
        header: 'Attempt',
        accessorFn: (row) => row.attempt,
        cell: ({ row }) => <span className="num small">{row.original.attempt}/{row.original.max_attempts}</span>,
      },
      {
        id: 'created',
        header: 'Created',
        accessorFn: (row) => row.created_at,
        cell: ({ row }) => <span className="small dim nowrap" title={row.original.created_at}>{relativeTime(row.original.created_at)}</span>,
      },
      {
        id: 'scheduled',
        header: 'Scheduled',
        accessorFn: (row) => row.scheduled_at,
        cell: ({ row }) => <span className="small dim nowrap" title={row.original.scheduled_at}>{formatDateTime(row.original.scheduled_at)}</span>,
      },
      {
        id: 'actions',
        header: '',
        enableSorting: false,
        cell: ({ row }) => (
          <div className="flex" style={{ justifyContent: 'flex-end', gap: 4 }}>
            <Link
              to="/jobs/$id"
              params={{ id: String(row.original.id) }}
              className="btn ghost sm"
              title="Open job details"
              onClick={(event) => event.stopPropagation()}
            >
              <Icon name="eye" />
            </Link>
            {canRetry(row.original.state) ? (
              <Button size="sm" variant="ghost" title="Retry" onClick={(event) => askJob(event, row.original, 'retry')}>
                <Icon name="refresh" />
              </Button>
            ) : null}
            {canCancel(row.original.state) ? (
              <Button size="sm" variant="ghost" title="Cancel" onClick={(event) => askJob(event, row.original, 'cancel')}>
                <Icon name="pause" />
              </Button>
            ) : null}
            {canDelete(row.original.state) ? (
              <Button size="sm" variant="ghost" title="Delete" onClick={(event) => askJob(event, row.original, 'delete')}>
                <Icon name="trash" />
              </Button>
            ) : null}
          </div>
        ),
      },
    ],
    [jobAction],
  )

  return (
    <>
      <PageHeader
        eyebrow="Operations"
        title="Queue"
        subtitle="River queues: depth counters, job inspection, retries, cancellations and pause controls."
        actions={
          <>
            <RefreshToggle enabled={live} onChange={setLive} />
            <Button
              icon="refresh"
              onClick={() => {
                void queues.refetch()
                void jobs.refetch()
              }}
              disabled={queues.isFetching || jobs.isFetching}
            >
              Refresh
            </Button>
          </>
        }
      />

      <div className="grid cols-4 mb-16">
        <StatCard label="Queues" value={queueRows.length} hint="critical · default · heavy" />
        <StatCard label="Active jobs" value={totals.active} tone="accent" hint={`${totals.total.toLocaleString()} total tracked`} />
        <StatCard label="Failed jobs" value={totals.failed} tone={totals.failed > 0 ? 'red' : 'green'} hint="discarded, awaiting review" />
        <StatCard label="Loaded jobs" value={rows.length} hint="current page set" />
      </div>

      {queues.isError ? (
        <div className="mb-16">
          <ErrorBox
            message={
              queues.error instanceof Error
                ? queues.error.message
                : 'Queue administration is unavailable (QUEUE_DRIVER=sync?).'
            }
          />
        </div>
      ) : (
        <Card tight className="mb-16">
          <CardHeader title="Queues" sub="Pause a queue to stop workers from fetching new jobs." />
          {queues.isLoading ? (
            <LoadingBlock label="Loading queues…" />
          ) : queueRows.length === 0 ? (
            <EmptyState title="No queues reported" description="Queue metadata appears once River workers touch a queue." icon="queue" />
          ) : (
            <div>
              {queueRows.map((queue) => (
                <div className="queue-row" key={queue.name}>
                  <div>
                    <div className="queue-name">{queue.name}</div>
                    <div className="small dim">
                      {queue.paused_at ? `paused ${relativeTime(queue.paused_at)}` : 'active'}
                    </div>
                  </div>
                  <div className="queue-count">
                    {queue.total.toLocaleString()}<small>total</small>
                  </div>
                  <div className="queue-count">
                    {queue.active.toLocaleString()}<small>active</small>
                  </div>
                  <div className="queue-count" style={{ color: queue.failed > 0 ? 'var(--red)' : undefined }}>
                    {queue.failed.toLocaleString()}<small>failed</small>
                  </div>
                  <div>
                    {queue.paused_at ? (
                      <Button size="sm" icon="play" onClick={() => queueAction.mutate({ name: queue.name, action: 'resume' })}>
                        Resume
                      </Button>
                    ) : (
                      <Button size="sm" icon="pause" onClick={() => queueAction.mutate({ name: queue.name, action: 'pause' })}>
                        Pause
                      </Button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </Card>
      )}

      <div className="filters">
        <label className="field">
          <span className="field-label">Queue</span>
          <select className="select" value={queueFilter} onChange={(event) => setQueueFilter(event.target.value)}>
            <option value="">Any</option>
            {['critical', 'default', 'heavy'].map((name) => (
              <option key={name} value={name}>{name}</option>
            ))}
          </select>
        </label>
        <label className="field">
          <span className="field-label">State</span>
          <select className="select" value={stateFilter} onChange={(event) => setStateFilter(event.target.value)}>
            {states.map((state) => (
              <option key={state || 'any'} value={state}>{state || 'Any'}</option>
            ))}
          </select>
        </label>
        <label className="field grow">
          <span className="field-label">Application kind</span>
          <input
            className="input mono"
            placeholder="mail.send"
            value={kind}
            onChange={(event) => setKind(event.target.value)}
          />
        </label>
        <div className="spacer" />
        <span className="small dim">{rows.length} jobs loaded</span>
      </div>

      {jobs.isLoading ? (
        <LoadingBlock label="Loading jobs…" />
      ) : jobs.isError ? (
        <ErrorBox message={jobs.error instanceof Error ? jobs.error.message : 'Unable to load jobs.'} />
      ) : (
        <>
          <DataTable
            data={rows}
            columns={columns}
            onRowClick={(row) => setSelectedJob(row.id)}
            empty={
              <EmptyState
                title="No jobs match"
                description="Adjust the queue/state filters, or enqueue a job."
                icon="queue"
              />
            }
          />
          <div className="flex mt-16">
            {jobs.hasNextPage ? (
              <Button icon="chevron" onClick={() => void jobs.fetchNextPage()} disabled={jobs.isFetchingNextPage}>
                {jobs.isFetchingNextPage ? 'Loading…' : 'Load more'}
              </Button>
            ) : (
              <span className="small dim">{rows.length} job{rows.length === 1 ? '' : 's'} loaded</span>
            )}
          </div>
        </>
      )}

      <Drawer
        open={selectedJob !== null}
        onClose={() => setSelectedJob(null)}
        title={jobDetail.data ? `Job #${jobDetail.data.id}` : `Job #${selectedJob}`}
        subtitle={jobDetail.data ? `${jobDetail.data.kind} · ${jobDetail.data.queue}` : undefined}
        actions={
          jobDetail.data ? (
            <>
              <Link
                to="/jobs/$id"
                params={{ id: String(jobDetail.data.id) }}
                className="btn sm"
                onClick={() => setSelectedJob(null)}
              >
                <Icon name="eye" /> Details
              </Link>
              {canRetry(jobDetail.data.state) ? (
                <Button size="sm" icon="refresh" onClick={() => jobAction.mutate({ id: jobDetail.data.id, action: 'retry' })}>
                  Retry
                </Button>
              ) : null}
              {canCancel(jobDetail.data.state) ? (
                <Button size="sm" variant="danger" icon="pause" onClick={() => jobAction.mutate({ id: jobDetail.data.id, action: 'cancel' })}>
                  Cancel
                </Button>
              ) : null}
            </>
          ) : null
        }
      >
        {jobDetail.isLoading ? (
          <LoadingBlock label="Loading job…" />
        ) : jobDetail.isError ? (
          <ErrorBox message={jobDetail.error instanceof Error ? jobDetail.error.message : 'Unable to load job.'} />
        ) : jobDetail.data ? (
          <div className="stack">
            <Card tight>
              <dl className="kv">
                <dt>State</dt><dd><Chip tone={stateTone(jobDetail.data.state)}>{jobDetail.data.state}</Chip></dd>
                <dt>Queue</dt><dd>{jobDetail.data.queue}</dd>
                <dt>Priority</dt><dd>{jobDetail.data.priority}</dd>
                <dt>Attempt</dt><dd>{jobDetail.data.attempt} / {jobDetail.data.max_attempts}</dd>
                <dt>Created</dt><dd>{formatDateTime(jobDetail.data.created_at)}</dd>
                <dt>Scheduled</dt><dd>{formatDateTime(jobDetail.data.scheduled_at)}</dd>
                <dt>Attempted</dt><dd>{jobDetail.data.attempted_at ? formatDateTime(jobDetail.data.attempted_at) : '—'}</dd>
                <dt>Finalized</dt><dd>{jobDetail.data.finalized_at ? formatDateTime(jobDetail.data.finalized_at) : '—'}</dd>
                <dt>Duration</dt><dd>{formatJobDuration(jobDetail.data)}</dd>
              </dl>
            </Card>

            <Card tight>
              <CardHeader title="Args" sub="Redacted where sensitive keys are known" />
              <JsonView value={jobDetail.data.args} label="Args" />
            </Card>

            <Card tight>
              <CardHeader title={`Errors (${jobDetail.data.errors?.length ?? 0})`} />
              {(jobDetail.data.errors?.length ?? 0) === 0 ? (
                <div className="muted small">No failed attempts recorded.</div>
              ) : (
                <div className="stack-tight">
                  {jobDetail.data.errors?.map((error, index) => (
                    <div key={`${index}-${error.at}`} className="query-row">
                      <div className="flex">
                        <Chip tone="red">attempt {error.attempt ?? index + 1}</Chip>
                        <span className="spacer" />
                        <span className="small dim">{error.at ? formatDateTime(error.at) : ''}</span>
                      </div>
                      <div className="mt-8"><CodeBlock code={error.error ?? ""} label="Error" /></div>
                    </div>
                  ))}
                </div>
              )}
            </Card>
          </div>
        ) : null}
      </Drawer>

      <ConfirmDialog
        open={confirm !== null}
        title={confirm?.title ?? ''}
        description={confirm?.description}
        confirmLabel={confirm?.confirmLabel}
        danger={confirm?.danger}
        busy={confirmBusy}
        onCancel={() => setConfirm(null)}
        onConfirm={() => {
          if (!confirm) return
          setConfirmBusy(true)
          void confirm
            .run()
            .catch(() => undefined)
            .finally(() => {
              setConfirmBusy(false)
              setConfirm(null)
            })
        }}
      />
    </>
  )
}

function formatJobDuration(job: DevJob): string {
  if (!job.attempted_at || !job.finalized_at) return '—'
  return formatDuration(new Date(job.finalized_at).getTime() - new Date(job.attempted_at).getTime())
}


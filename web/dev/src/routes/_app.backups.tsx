import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { useMemo, useState, type MouseEvent } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import { api, apiBlob } from '../api/client'
import type {
  CursorPage,
  DevBackupCleanResult,
  DevBackupConfig,
  DevBackupRun,
  DevBackupStats,
} from '../api/types'
import { DataTable } from '../components/DataTable'
import { Select } from '../components/inputs'
import { useToast } from '../components/Toasts'
import {
  Button,
  Card,
  CardHeader,
  Chip,
  ConfirmDialog,
  Drawer,
  EmptyState,
  ErrorBox,
  LoadingBlock,
  PageHeader,
  StatCard,
} from '../components/ui'
import { formatBytes, formatDateTime, relativeTime } from '../format'

export const Route = createFileRoute('/_app/backups')({
  component: BackupsPage,
})

const statusTones: Record<string, 'green' | 'yellow' | 'red' | 'blue' | 'purple' | 'gray'> = {
  completed: 'green',
  partial: 'yellow',
  failed: 'red',
  running: 'blue',
  pending: 'purple',
}

function BackupsPage() {
  const { push } = useToast()
  const queryClient = useQueryClient()
  const [status, setStatus] = useState('')
  const [selected, setSelected] = useState<DevBackupRun | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<DevBackupRun | null>(null)
  const [cleanPreview, setCleanPreview] = useState<DevBackupCleanResult | null>(null)

  const stats = useQuery({
    queryKey: ['backups', 'stats'],
    queryFn: ({ signal }) => api<DevBackupStats>('/dev/api/backups/stats', { signal }),
  })

  const config = useQuery({
    queryKey: ['backups', 'config'],
    queryFn: ({ signal }) => api<DevBackupConfig>('/dev/api/backups/config', { signal }),
    staleTime: 60_000,
  })

  const runs = useInfiniteQuery({
    queryKey: ['backups', 'list', status],
    queryFn: ({ pageParam, signal }) =>
      api<CursorPage<DevBackupRun>>('/dev/api/backups', {
        query: { limit: 25, cursor: pageParam || undefined, status: status || undefined },
        signal,
        unwrap: false,
      }),
    initialPageParam: '',
    getNextPageParam: (lastPage) => lastPage.meta?.next_cursor || undefined,
    refetchInterval: (query) => {
      const pages = query.state.data?.pages ?? []
      const active = pages.some((page) =>
        page.data.some((run) => run.status === 'pending' || run.status === 'running'),
      )
      return active ? 3_000 : false
    },
  })

  const rows = useMemo(() => runs.data?.pages.flatMap((page) => page.data) ?? [], [runs.data])
  const lastSuccess = rows.find((run) => run.status === 'completed' || run.status === 'partial')
  const active = rows.some((run) => run.status === 'pending' || run.status === 'running')
  const enabled = config.data?.enabled ?? false

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['backups'] })
    void queryClient.invalidateQueries({ queryKey: ['actions'] })
    void queryClient.invalidateQueries({ queryKey: ['overview'] })
  }

  const create = useMutation({
    mutationFn: () => api<DevBackupRun>('/dev/api/backups', { method: 'POST' }),
    onSuccess: (run) => {
      push(
        run.status === 'completed'
          ? `Backup #${run.id} completed (${formatBytes(run.size_bytes)})`
          : `Backup #${run.id} queued`,
        'success',
      )
      invalidate()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to create a backup', 'error'),
  })

  const remove = useMutation({
    mutationFn: (id: number) => api(`/dev/api/backups/${id}`, { method: 'DELETE' }),
    onSuccess: (_result, id) => {
      push('Backup deleted', 'success')
      setConfirmDelete(null)
      if (selected?.id === id) setSelected(null)
      invalidate()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to delete the backup', 'error'),
  })

  const clean = useMutation({
    mutationFn: (dryRun: boolean) =>
      api<DevBackupCleanResult>('/dev/api/backups/clean', {
        method: 'POST',
        body: { dry_run: dryRun },
      }),
    onSuccess: (result) => {
      if (result.dry_run) {
        if (result.candidates === 0) {
          push('No backups are past the retention window', 'info')
          return
        }
        setCleanPreview(result)
        return
      }
      setCleanPreview(null)
      push(`Deleted ${result.deleted} backup${result.deleted === 1 ? '' : 's'}`, 'success')
      invalidate()
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Backup cleanup failed', 'error'),
  })

  const download = useMutation({
    mutationFn: async (run: DevBackupRun) => {
      const blob = await apiBlob(`/dev/api/backups/${run.id}/download`)
      const url = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = url
      anchor.download = run.filename
      document.body.appendChild(anchor)
      anchor.click()
      anchor.remove()
      window.setTimeout(() => URL.revokeObjectURL(url), 1_000)
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Unable to download the backup', 'error'),
  })

  const askDelete = (event: MouseEvent, run: DevBackupRun) => {
    event.stopPropagation()
    setConfirmDelete(run)
  }

  const columns = useMemo<ColumnDef<DevBackupRun, any>[]>(
    () => [
      {
        id: 'created',
        header: 'Created',
        accessorFn: (row) => row.created_at,
        enableSorting: false,
        cell: ({ row }) => (
          <span className="small dim nowrap" title={formatDateTime(row.original.created_at)}>
            {relativeTime(row.original.created_at)}
          </span>
        ),
      },
      {
        id: 'status',
        header: 'Status',
        accessorFn: (row) => row.status,
        enableSorting: false,
        cell: ({ row }) => (
          <Chip tone={statusTones[row.original.status] ?? 'gray'}>{row.original.status}</Chip>
        ),
      },
      {
        id: 'filename',
        header: 'Archive',
        accessorFn: (row) => row.filename,
        enableSorting: false,
        cell: ({ row }) => (
          <div>
            <div className="mono small">{row.original.filename}</div>
            <div className="small dim">
              {row.original.scope} · {row.original.pg_dump_version || 'pg_dump unknown'}
            </div>
          </div>
        ),
      },
      {
        id: 'trigger',
        header: 'Trigger',
        accessorFn: (row) => row.trigger,
        enableSorting: false,
        cell: ({ row }) => (
          <div>
            <div className="small">{row.original.trigger}</div>
            <div className="small dim">
              {row.original.actor_user_id ? `user #${row.original.actor_user_id}` : 'system'}
            </div>
          </div>
        ),
      },
      {
        id: 'size',
        header: 'Size',
        accessorFn: (row) => row.size_bytes,
        enableSorting: false,
        cell: ({ row }) => <span className="num small">{formatBytes(row.original.size_bytes)}</span>,
      },
      {
        id: 'destinations',
        header: 'Destinations',
        enableSorting: false,
        cell: ({ row }) => (
          <div className="flex" style={{ gap: 4, flexWrap: 'wrap' }}>
            {row.original.destinations.length === 0 ? (
              <span className="small dim">—</span>
            ) : (
              row.original.destinations.map((destination) => (
                <Chip
                  key={destination.destination}
                  tone={destination.status === 'completed' ? 'green' : 'red'}
                >
                  {destination.destination}
                </Chip>
              ))
            )}
          </div>
        ),
      },
      {
        id: 'actions',
        header: '',
        enableSorting: false,
        cell: ({ row }) => (
          <div className="flex" style={{ justifyContent: 'flex-end', gap: 4 }}>
            <Button
              size="sm"
              variant="ghost"
              icon="download"
              title="Download"
              disabled={row.original.status !== 'completed' && row.original.status !== 'partial'}
              onClick={(event) => {
                event.stopPropagation()
                download.mutate(row.original)
              }}
            />
            <Button
              size="sm"
              variant="ghost"
              icon="trash"
              title="Delete"
              disabled={row.original.status === 'running' || row.original.status === 'pending'}
              onClick={(event) => askDelete(event, row.original)}
            />
          </div>
        ),
      },
    ],
    [],
  )

  return (
    <>
      <PageHeader
        eyebrow="Operations"
        title="Backups"
        subtitle="PostgreSQL archives with local retention and optional S3/R2 copies. Every run is recorded and audited."
        actions={
          <>
            <Button
              variant="primary"
              icon="backup"
              disabled={!enabled || create.isPending}
              title={enabled ? 'Create a backup now' : 'Set BACKUP_ENABLED=true to enable backups'}
              onClick={() => create.mutate()}
            >
              {create.isPending ? 'Starting…' : 'Create backup'}
            </Button>
            <Button
              variant="danger"
              icon="trash"
              disabled={clean.isPending || !enabled}
              onClick={() => clean.mutate(true)}
            >
              Clean old backups
            </Button>
            <Button
              icon="refresh"
              onClick={() => {
                void queryClient.invalidateQueries({ queryKey: ['backups'] })
              }}
              disabled={runs.isFetching}
            >
              Refresh
            </Button>
          </>
        }
      />

      <div className="grid cols-5">
        <StatCard
          label="Last backup"
          value={stats.data?.last_success_at ? relativeTime(stats.data.last_success_at) : 'Never'}
          tone={stats.data?.status === 'failed' ? 'red' : undefined}
          hint={stats.data?.last_success_at ? formatDateTime(stats.data.last_success_at) : 'no successful run'}
        />
        <StatCard label="Last size" value={formatBytes(lastSuccess?.size_bytes)} hint="most recent successful run" />
        <StatCard label="Total stored" value={formatBytes(stats.data?.total_size ?? 0)} hint="active runs" />
        <StatCard label="Successful" value={stats.data?.successful_runs ?? 0} hint={`${stats.data?.total_runs ?? 0} total runs`} />
        <StatCard
          label="Health"
          value={
            <Chip
              tone={
                !stats.data
                  ? 'gray'
                  : stats.data.status === 'ok'
                    ? 'green'
                    : stats.data.status === 'warning'
                      ? 'yellow'
                      : 'red'
              }
            >
              {stats.data?.status ?? 'loading'}
            </Chip>
          }
          hint={stats.data?.summary ?? 'loading monitor state'}
        />
      </div>

      <div className="grid cols-2 mt-24">
        <Card>
          <CardHeader
            title="Configuration"
            sub={enabled ? 'Backups are enabled' : 'Backups are disabled'}
            actions={
              <Chip tone={enabled ? 'green' : 'red'}>{enabled ? 'enabled' : 'disabled'}</Chip>
            }
          />
          {config.isLoading ? (
            <LoadingBlock label="Loading backup configuration…" />
          ) : config.isError ? (
            <ErrorBox message={config.error instanceof Error ? config.error.message : 'Unable to load backup configuration.'} />
          ) : config.data ? (
            <dl className="kv">
              <dt>Destinations</dt>
              <dd className="flex" style={{ gap: 4, flexWrap: 'wrap' }}>
                {config.data.destinations.map((destination) => (
                  <Chip key={destination} tone={destination === 'local' ? 'blue' : 'purple'}>
                    {destination}
                  </Chip>
                ))}
              </dd>
              <dt>Local directory</dt>
              <dd className="mono small break">{config.data.directory}</dd>
              <dt>Remote prefix</dt>
              <dd className="mono small">{config.data.s3_prefix || '—'}</dd>
              <dt>pg_dump</dt>
              <dd>
                {config.data.pg_dump_available ? (
                  <span>
                    available · {config.data.pg_dump_version}{' '}
                    <span className="dim mono small">{config.data.pg_dump_path}</span>
                  </span>
                ) : (
                  <span className="danger-text">
                    unavailable ({config.data.pg_dump_path}) — install PostgreSQL client tools
                  </span>
                )}
              </dd>
              <dt>Schedule</dt>
              <dd className="mono small">
                run {config.data.run_cron} · clean {config.data.clean_cron}
              </dd>
              <dt>Retention</dt>
              <dd className="small">
                all {config.data.keep_all_days}d · daily {config.data.keep_daily_days}d · weekly{' '}
                {config.data.keep_weekly_weeks}w · monthly {config.data.keep_monthly_months}mo · yearly{' '}
                {config.data.keep_yearly_years}y
                {config.data.max_total_mb > 0 ? ` · cap ${config.data.max_total_mb} MB` : ''}
              </dd>
              <dt>Monitoring</dt>
              <dd className="small">max age {config.data.max_age_hours}h</dd>
              <dt>Files</dt>
              <dd className="small">
                {config.data.include_files
                  ? `included: ${config.data.files_paths.join(', ') || '(no paths configured)'}`
                  : 'database only'}
              </dd>
            </dl>
          ) : null}
        </Card>

        <Card>
          <CardHeader title="Retention and operations" sub="How archives are kept and removed" />
          <div className="stack-tight small">
            <div>
              <strong>DefaultStrategy:</strong> every archive for {config.data?.keep_all_days ?? 7} days, then one
              per day, week, month and year inside the configured windows.
            </div>
            <div>
              <strong>Never deleted:</strong> the newest archive and any file not past the retention policy.
            </div>
            <div>
              <strong>Cleanup:</strong> removes the archive on every destination before marking the run deleted.
              Preview first to see the candidate count.
            </div>
            <div>
              <strong>Remote stores:</strong> any S3-compatible endpoint works (AWS S3, Cloudflare R2, MinIO,
              Backblaze B2 S3) through the shared <span className="mono">S3_*</span> settings.
            </div>
          </div>
          <div className="flex mt-16">
            <Button
              icon="trash"
              disabled={clean.isPending || !enabled}
              onClick={() => clean.mutate(true)}
            >
              Preview cleanup
            </Button>
            {active ? (
              <span className="small dim flex">
                <span className="spinner" /> waiting for a running backup
              </span>
            ) : null}
          </div>
        </Card>
      </div>

      <Card className="mt-24">
        <CardHeader
          title="Backup runs"
          sub={`${rows.length} loaded${active ? ' · polling every 3s' : ''}`}
          actions={
            <label className="field">
              <span className="field-label">Status</span>
              <Select
                size="sm"
                value={status}
                onChange={setStatus}
                options={[
                  { value: '', label: 'All statuses' },
                  { value: 'completed', label: 'Completed' },
                  { value: 'partial', label: 'Partial' },
                  { value: 'failed', label: 'Failed' },
                  { value: 'running', label: 'Running' },
                  { value: 'pending', label: 'Pending' },
                ]}
              />
            </label>
          }
        />
        {runs.isLoading ? (
          <LoadingBlock label="Loading backup runs…" />
        ) : runs.isError ? (
          <ErrorBox message={runs.error instanceof Error ? runs.error.message : 'Unable to load backup runs.'} />
        ) : (
          <>
            <DataTable
              data={rows}
              columns={columns}
              onRowClick={(run) => setSelected(run)}
              empty={
                <EmptyState
                  title="No backups yet"
                  description="Create the first archive now, or enable BACKUP_ENABLED=true to schedule nightly runs."
                  icon="backup"
                  action={
                    <Button variant="primary" icon="backup" disabled={!enabled || create.isPending} onClick={() => create.mutate()}>
                      Create backup
                    </Button>
                  }
                />
              }
            />
            <div className="flex mt-16">
              {runs.hasNextPage ? (
                <Button
                  icon="chevron"
                  onClick={() => void runs.fetchNextPage()}
                  disabled={runs.isFetchingNextPage}
                >
                  {runs.isFetchingNextPage ? 'Loading…' : 'Load more'}
                </Button>
              ) : (
                <span className="small dim">{rows.length === 0 ? 'No rows' : `${rows.length} runs loaded`}</span>
              )}
              {runs.isFetching && !runs.isFetchingNextPage ? (
                <span className="small dim flex">
                  <span className="spinner" /> refreshing
                </span>
              ) : null}
            </div>
          </>
        )}
      </Card>

      <Drawer
        open={selected !== null}
        title={selected?.filename ?? 'Backup'}
        subtitle={
          selected
            ? `${selected.status} · ${formatBytes(selected.size_bytes)} · ${selected.trigger}`
            : undefined
        }
        onClose={() => setSelected(null)}
        actions={
          selected ? (
            <>
              <Button
                size="sm"
                icon="download"
                disabled={
                  download.isPending ||
                  (selected.status !== 'completed' && selected.status !== 'partial')
                }
                onClick={() => download.mutate(selected)}
              >
                Download
              </Button>
              <Button
                size="sm"
                variant="danger"
                icon="trash"
                disabled={selected.status === 'running' || selected.status === 'pending'}
                onClick={(event) => askDelete(event, selected)}
              >
                Delete
              </Button>
            </>
          ) : undefined
        }
      >
        {selected ? (
          <div className="stack">
            {selected.error ? <ErrorBox message={selected.error} /> : null}
            <dl className="kv">
              <dt>Status</dt>
              <dd>
                <Chip tone={statusTones[selected.status] ?? 'gray'}>{selected.status}</Chip>
              </dd>
              <dt>Run ID</dt>
              <dd className="mono">#{selected.id}</dd>
              <dt>Trigger</dt>
              <dd>
                {selected.trigger}
                {selected.actor_user_id ? ` · user #${selected.actor_user_id}` : ' · system'}
              </dd>
              <dt>Scope</dt>
              <dd>{selected.scope}</dd>
              <dt>Database</dt>
              <dd className="mono small">{selected.database_name || '—'}</dd>
              <dt>pg_dump</dt>
              <dd>{selected.pg_dump_version || '—'}</dd>
              <dt>Size</dt>
              <dd>{formatBytes(selected.size_bytes)}</dd>
              <dt>SHA-256</dt>
              <dd className="mono small break">{selected.checksum_sha256 || '—'}</dd>
              <dt>Created</dt>
              <dd>{formatDateTime(selected.created_at)}</dd>
              <dt>Started</dt>
              <dd>{selected.started_at ? formatDateTime(selected.started_at) : '—'}</dd>
              <dt>Finished</dt>
              <dd>{selected.finished_at ? formatDateTime(selected.finished_at) : '—'}</dd>
              <dt>UUID</dt>
              <dd className="mono small break">{selected.uuid}</dd>
            </dl>

            <CardHeader title="Destinations" sub="Each destination is attempted independently" />
            {selected.destinations.length === 0 ? (
              <div className="small dim">No destination results were recorded.</div>
            ) : (
              <div className="stack">
                {selected.destinations.map((destination) => (
                  <div key={destination.destination} className="card" style={{ padding: 14 }}>
                    <div className="flex">
                      <Chip tone={destination.status === 'completed' ? 'green' : 'red'}>
                        {destination.destination}
                      </Chip>
                      <span className="small dim">{destination.status}</span>
                      <div className="spacer" />
                      <span className="small dim">{formatBytes(destination.size_bytes)}</span>
                    </div>
                    {destination.path ? <div className="mono small break mt-8">{destination.path}</div> : null}
                    {destination.key ? <div className="mono small break mt-8">{destination.key}</div> : null}
                    {destination.error ? <div className="small danger-text mt-8">{destination.error}</div> : null}
                  </div>
                ))}
              </div>
            )}
          </div>
        ) : null}
      </Drawer>

      <ConfirmDialog
        open={confirmDelete !== null}
        title={`Delete ${confirmDelete?.filename ?? 'backup'}?`}
        description="The archive is removed from local storage and every recorded remote destination. The run remains in the audit log."
        confirmLabel="Delete backup"
        danger
        busy={remove.isPending}
        onCancel={() => setConfirmDelete(null)}
        onConfirm={() => {
          if (confirmDelete) remove.mutate(confirmDelete.id)
        }}
      />

      <ConfirmDialog
        open={cleanPreview !== null}
        title="Clean old backups"
        description={
          cleanPreview
            ? `${cleanPreview.candidates} backup(s) are past the retention window. This removes them from every destination and marks the runs deleted.`
            : undefined
        }
        confirmLabel="Delete expired backups"
        danger
        busy={clean.isPending}
        onCancel={() => setCleanPreview(null)}
        onConfirm={() => clean.mutate(false)}
      />
    </>
  )
}

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { createFileRoute, Link, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { api } from '../api/client'
import type { DevJob } from '../api/types'
import { useToast } from '../components/Toasts'
import {
  Button,
  Card,
  CardHeader,
  Chip,
  CodeBlock,
  ConfirmDialog,
  EmptyState,
  ErrorBox,
  Icon,
  JsonView,
  LoadingBlock,
  PageHeader,
  StatCard,
} from '../components/ui'
import { formatDateTime, formatDuration, stateTone } from '../format'

export const Route = createFileRoute('/_app/jobs/$id')({
  component: JobDetailPage,
})

type JobAction = 'retry' | 'cancel' | 'delete'

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
  action: JobAction
}

function JobDetailPage() {
  const { id } = Route.useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { push } = useToast()
  const [confirm, setConfirm] = useState<ConfirmState | null>(null)

  const job = useQuery({
    queryKey: ['job', id],
    queryFn: ({ signal }) => api<DevJob>(`/dev/api/queue/jobs/${id}`, { signal }),
    refetchInterval: 5000,
    retry: false,
  })

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['jobs'] })
    void queryClient.invalidateQueries({ queryKey: ['job', id] })
    void queryClient.invalidateQueries({ queryKey: ['queues'] })
    void queryClient.invalidateQueries({ queryKey: ['actions'] })
  }

  const mutation = useMutation({
    mutationFn: (action: JobAction) => {
      if (action === 'delete') return api(`/dev/api/queue/jobs/${id}`, { method: 'DELETE' })
      return api(`/dev/api/queue/jobs/${id}/${action}`, { method: 'POST' })
    },
    onSuccess: (_result, action) => {
      push(`Job #${id} ${action}`, action === 'delete' ? 'info' : 'success')
      invalidate()
      if (action === 'delete') void navigate({ to: '/queue' })
    },
    onError: (error) => push(error instanceof Error ? error.message : 'Job action failed', 'error'),
    onSettled: invalidate,
  })

  const ask = (action: JobAction) => {
    const copy: Record<JobAction, ConfirmState> = {
      retry: {
        title: 'Retry job',
        description: `Job #${id} becomes immediately available for work.`,
        confirmLabel: 'Retry job',
        action,
      },
      cancel: {
        title: 'Cancel job',
        description: `Job #${id} will not run again.`,
        confirmLabel: 'Cancel job',
        danger: true,
        action,
      },
      delete: {
        title: 'Delete job',
        description: `Job #${id} is removed permanently. Running jobs cannot be deleted.`,
        confirmLabel: 'Delete job',
        danger: true,
        action,
      },
    }
    setConfirm(copy[action])
  }

  if (job.isLoading) return <LoadingBlock label="Loading job…" />
  if (job.isError) {
    return <ErrorBox message={job.error instanceof Error ? job.error.message : 'Unable to load job.'} />
  }
  if (!job.data) return <EmptyState title="Job not found" icon="queue" />

  const item = job.data

  return (
    <>
      <div className="mb-8">
        <Link to="/queue" className="btn ghost sm">
          <Icon name="chevron" style={{ transform: 'rotate(180deg)' }} /> Back to queue
        </Link>
      </div>
      <PageHeader
        eyebrow={`Job #${item.id}`}
        title={item.kind}
        subtitle={`${item.queue} queue · ${item.river_kind ?? 'river job'}`}
        actions={
          <>
            <Button icon="refresh" onClick={() => void job.refetch()} disabled={job.isFetching}>
              Refresh
            </Button>
            {canRetry(item.state) ? (
              <Button icon="refresh" onClick={() => ask('retry')}>
                Retry
              </Button>
            ) : null}
            {canCancel(item.state) ? (
              <Button variant="danger" icon="pause" onClick={() => ask('cancel')}>
                Cancel
              </Button>
            ) : null}
            {canDelete(item.state) ? (
              <Button variant="danger" icon="trash" onClick={() => ask('delete')}>
                Delete
              </Button>
            ) : null}
          </>
        }
      />

      <div className="grid cols-4 mb-16">
        <StatCard label="State" value={<Chip tone={stateTone(item.state)}>{item.state}</Chip>} />
        <StatCard label="Attempts" value={`${item.attempt} / ${item.max_attempts}`} />
        <StatCard label="Priority" value={item.priority} />
        <StatCard label="Queue" value={<span className="mono" style={{ fontSize: 18 }}>{item.queue}</span>} />
      </div>

      <div className="row">
        <div style={{ flex: 1 }}>
          <Card tight>
            <CardHeader title="Job metadata" />
            <dl className="kv">
              <dt>ID</dt><dd>{item.id}</dd>
              <dt>Application kind</dt><dd>{item.kind}</dd>
              <dt>River kind</dt><dd>{item.river_kind ?? '—'}</dd>
              <dt>State</dt><dd>{item.state}</dd>
              <dt>Created</dt><dd>{formatDateTime(item.created_at)}</dd>
              <dt>Scheduled</dt><dd>{formatDateTime(item.scheduled_at)}</dd>
              <dt>Attempted</dt><dd>{item.attempted_at ? formatDateTime(item.attempted_at) : '—'}</dd>
              <dt>Finalized</dt><dd>{item.finalized_at ? formatDateTime(item.finalized_at) : '—'}</dd>
              <dt>Run duration</dt><dd>{jobRunDuration(item)}</dd>
              <dt>Tags</dt><dd>{(item.tags ?? []).length > 0 ? (item.tags ?? []).join(', ') : '—'}</dd>
            </dl>
          </Card>
        </div>
        <div style={{ flex: 1 }}>
          <Card tight>
            <CardHeader title="Args" sub="Sensitive values redacted where keys are known" />
            <JsonView value={item.args} label="Args" />
          </Card>
        </div>
      </div>

      <div className="section-title">Attempt history</div>
      {(item.errors?.length ?? 0) === 0 ? (
        <Card tight>
          <div className="muted small">No failed attempts recorded for this job.</div>
        </Card>
      ) : (
        <div className="stack">
          {item.errors?.map((error, index) => (
            <Card tight key={`${index}-${error.at}`}>
              <div className="flex mb-8">
                <Chip tone="red">attempt {error.attempt ?? index + 1}</Chip>
                <span className="spacer" />
                <span className="small dim">{error.at ? formatDateTime(error.at) : ''}</span>
              </div>
              <CodeBlock code={error.error ?? ''} label="Error" />
            </Card>
          ))}
        </div>
      )}

      <ConfirmDialog
        open={confirm !== null}
        title={confirm?.title ?? ''}
        description={confirm?.description}
        confirmLabel={confirm?.confirmLabel}
        danger={confirm?.danger}
        busy={mutation.isPending}
        onCancel={() => setConfirm(null)}
        onConfirm={() => {
          if (!confirm) return
          mutation.mutate(confirm.action, { onSettled: () => setConfirm(null) })
        }}
      />
    </>
  )
}

function jobRunDuration(job: DevJob): string {
  if (!job.attempted_at || !job.finalized_at) return '—'
  return formatDuration(new Date(job.finalized_at).getTime() - new Date(job.attempted_at).getTime())
}

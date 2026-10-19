import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { api } from '../api/client'
import type { HealthResults, SystemInfo } from '../api/types'
import {
  Button,
  Card,
  CardHeader,
  Chip,
  EmptyState,
  ErrorBox,
  JsonView,
  LoadingBlock,
  PageHeader,
} from '../components/ui'
import { formatDateTime, formatDuration } from '../format'

export const Route = createFileRoute('/_app/system')({
  component: SystemPage,
})

function healthTone(status: string): 'green' | 'yellow' | 'red' {
  if (status === 'ok') return 'green'
  if (status === 'warning') return 'yellow'
  return 'red'
}

async function fetchHealth(): Promise<HealthResults> {
  const response = await fetch('/api/health?fresh=1', { headers: { Accept: 'application/json' } })
  if (!response.ok) throw new Error(`Health check failed (${response.status})`)
  return (await response.json()) as HealthResults
}

function SystemPage() {
  const health = useQuery({ queryKey: ['health'], queryFn: fetchHealth, refetchInterval: 30_000 })
  const info = useQuery({
    queryKey: ['system-info'],
    queryFn: ({ signal }) => api<SystemInfo>('/api/v1/system/info', { signal }),
    retry: false,
  })

  return (
    <>
      <PageHeader
        eyebrow="Operations"
        title="System"
        subtitle="Live health checks and runtime identity for this API process."
        actions={
          <Button icon="refresh" onClick={() => void health.refetch()} disabled={health.isFetching}>
            Run checks
          </Button>
        }
      />

      <div className="row">
        <div style={{ flex: 2 }}>
          <Card tight>
            <CardHeader
              title="Health checks"
              sub={health.data ? `Finished ${formatDateTime(health.data.finishedAt * 1000)}` : 'Running the registry live'}
            />
            {health.isLoading ? (
              <LoadingBlock label="Running health checks…" />
            ) : health.isError ? (
              <ErrorBox message={health.error instanceof Error ? health.error.message : 'Unable to run health checks.'} />
            ) : (health.data?.checkResults ?? []).length === 0 ? (
              <EmptyState title="No checks registered" icon="system" />
            ) : (
              <div className="stack-tight">
                {health.data?.checkResults.map((check) => (
                  <div key={check.name} className="query-row">
                    <div className="flex">
                      <span style={{ fontWeight: 600 }}>{check.label || check.name}</span>
                      <Chip tone={healthTone(check.status)}>{check.status}</Chip>
                      <span className="spacer" />
                      <span className="small dim mono">{check.name}</span>
                    </div>
                    <div className="small muted mt-8">{check.shortSummary}</div>
                    {check.notificationMessage ? <div className="small dim mt-8">{check.notificationMessage}</div> : null}
                    {check.meta && Object.keys(check.meta).length > 0 ? (
                      <details className="mt-8">
                        <summary className="small muted" style={{ cursor: 'pointer' }}>Metadata</summary>
                        <div className="mt-8">
                          <JsonView value={check.meta} label="Metadata" maxHeight={220} />
                        </div>
                      </details>
                    ) : null}
                  </div>
                ))}
              </div>
            )}
          </Card>
        </div>

        <div style={{ flex: 1 }}>
          <Card tight>
            <CardHeader title="Runtime" sub="From /api/v1/system/info" />
            {info.isLoading ? (
              <LoadingBlock />
            ) : info.isError ? (
              <ErrorBox message="System info unavailable." />
            ) : info.data ? (
              <dl className="kv">
                <dt>Name</dt><dd>{info.data.name}</dd>
                <dt>Environment</dt><dd>{info.data.environment}</dd>
                <dt>Version</dt><dd>{info.data.version}</dd>
                <dt>Timezone</dt><dd>{info.data.timezone}</dd>
                {info.data.go_version ? <><dt>Go</dt><dd>{info.data.go_version}</dd></> : null}
                {info.data.uptime_seconds !== undefined ? (
                  <><dt>Uptime</dt><dd>{formatDuration(info.data.uptime_seconds * 1000)}</dd></>
                ) : null}
              </dl>
            ) : null}
          </Card>

          <Card tight className="mt-16">
            <CardHeader title="Telemetry" sub="Capture is flag-gated and redacted" />
            <div className="small muted">
              Requests, SQL, cache, queue and logs are captured when
              <span className="code-inline"> DEV_DASHBOARD_ENABLED </span>
              is on. Disable a category with
              <span className="code-inline"> DEV_DASHBOARD_CAPTURE </span>.
            </div>
          </Card>
        </div>
      </div>
    </>
  )
}



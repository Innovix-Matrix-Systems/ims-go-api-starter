import { Link, createFileRoute, redirect, type ErrorComponentProps } from '@tanstack/react-router'
import { getToken } from '../api/client'
import { AppShell } from '../components/AppShell'
import { Button, CodeBlock, Icon } from '../components/ui'

export const Route = createFileRoute('/_app')({
  beforeLoad: () => {
    if (!getToken()) {
      throw redirect({ to: '/login' })
    }
  },
  component: AppShell,
  errorComponent: AppErrorBoundary,
})

function AppErrorBoundary({ error, reset }: ErrorComponentProps) {
  const message = error instanceof Error ? error.message : 'Unexpected rendering error.'
  return (
    <div className="error-wrap">
      <div className="error-grid" />
      <div className="error-aura" />
      <div className="error-card card">
        <div className="flex" style={{ alignItems: 'flex-start' }}>
          <div className="error-icon">
            <Icon name="alert" />
          </div>
          <div>
            <div className="eyebrow">Developer dashboard</div>
            <h1 className="login-title" style={{ marginBottom: 4 }}>Something went wrong</h1>
            <p className="login-sub" style={{ marginBottom: 14 }}>
              The view hit an unexpected rendering error. Retry the screen — the API is usually
              fine and the error is contained to this page.
            </p>
          </div>
        </div>

        <div className="error-code">{message}</div>

        <details className="mt-16">
          <summary className="small muted" style={{ cursor: 'pointer' }}>Technical details</summary>
          <div className="mt-8"><CodeBlock code={error instanceof Error ? error.stack ?? message : String(error)} label="Stack trace" /></div>
        </details>

        <div className="modal-actions">
          <Link to="/" className="btn ghost">
            <Icon name="overview" /> Back to overview
          </Link>
          <Button variant="primary" icon="refresh" onClick={reset}>
            Retry
          </Button>
        </div>
      </div>
    </div>
  )
}


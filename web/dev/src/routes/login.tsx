import { useForm } from '@tanstack/react-form'
import { createFileRoute, redirect } from '@tanstack/react-router'
import { useState } from 'react'
import { clearSession, fetchMe, getToken, login, setCachedMe, setToken } from '../api/client'
import { ErrorBox, Icon, Spinner } from '../components/ui'

export const Route = createFileRoute('/login')({
  beforeLoad: () => {
    if (getToken()) {
      throw redirect({ to: '/' })
    }
  },
  component: LoginPage,
})

function LoginPage() {
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const form = useForm({
    defaultValues: { email: '', password: '' },
    onSubmit: async ({ value }) => {
      setError(null)
      setBusy(true)
      try {
        const result = await login(value.email.trim(), value.password)
        if (result.status !== 'AUTH_SUCCESS' || !result.token) {
          throw new Error(
            result.status === 'OTP_REQUIRED'
              ? 'This account requires OTP verification; use the API client to complete login.'
              : `Login did not complete (${result.status}).`,
          )
        }
        setToken(result.token)
        try {
          const me = await fetchMe()
          setCachedMe(me)
        } catch {
          clearSession()
          throw new Error('This account does not have access to the developer dashboard.')
        }
        window.location.assign('/dev/')
      } catch (cause) {
        setToken(null)
        setError(cause instanceof Error ? cause.message : 'Login failed')
      } finally {
        setBusy(false)
      }
    },
  })

  return (
    <div className="login-wrap">
      <div className="login-glow" />
      <div className="login-card">
        <div className="login-brand">
          <div className="brand-mark">D</div>
          <div>
            <div className="brand-title">Dev Dashboard</div>
            <div className="brand-sub">Superadmin control</div>
          </div>
        </div>
        <div className="card">
          <div className="eyebrow">Restricted surface</div>
          <h1 className="login-title">Sign in to continue</h1>
          <p className="login-sub">
            Super-admin and developer accounts only. This surface is disabled in production unless
            explicitly enabled.
          </p>

          <form
            onSubmit={(event) => {
              event.preventDefault()
              event.stopPropagation()
              void form.handleSubmit()
            }}
            className="stack"
          >
            <form.Field
              name="email"
              validators={{
                onChange: ({ value }) => (!value.trim() ? 'Email is required' : undefined),
              }}
            >
              {(field) => (
                <label className="field">
                  <span className="field-label">Email</span>
                  <input
                    className="input"
                    type="email"
                    autoComplete="username"
                    placeholder="developer@ims.com"
                    value={field.state.value}
                    onChange={(event) => field.handleChange(event.target.value)}
                    onBlur={field.handleBlur}
                  />
                  {field.state.meta.errors.length > 0 ? (
                    <span className="small" style={{ color: 'var(--red)' }}>{String(field.state.meta.errors[0])}</span>
                  ) : null}
                </label>
              )}
            </form.Field>

            <form.Field
              name="password"
              validators={{
                onChange: ({ value }) => (!value ? 'Password is required' : undefined),
              }}
            >
              {(field) => (
                <label className="field">
                  <span className="field-label">Password</span>
                  <input
                    className="input"
                    type="password"
                    autoComplete="current-password"
                    placeholder="••••••••"
                    value={field.state.value}
                    onChange={(event) => field.handleChange(event.target.value)}
                    onBlur={field.handleBlur}
                  />
                  {field.state.meta.errors.length > 0 ? (
                    <span className="small" style={{ color: 'var(--red)' }}>{String(field.state.meta.errors[0])}</span>
                  ) : null}
                </label>
              )}
            </form.Field>

            {error ? <ErrorBox message={error} /> : null}

            <button type="submit" className="btn primary" disabled={busy} style={{ marginTop: 4 }}>
              {busy ? <Spinner /> : <Icon name="logout" />}
              {busy ? 'Signing in…' : 'Sign in'}
            </button>
          </form>

          <div className="login-links">
            <span>Seeded: superadmin@ims.com · developer@ims.com</span>
            <a className="link" href="/docs" target="_blank" rel="noreferrer">
              API docs
            </a>
          </div>
        </div>
      </div>
    </div>
  )
}

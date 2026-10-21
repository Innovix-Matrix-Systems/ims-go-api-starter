import type { LoginResult, Me } from './types'

const TOKEN_KEY = 'ims.dev.token'
const ME_KEY = 'ims.dev.me'

export class ApiError extends Error {
  status: number
  code?: string

  constructor(message: string, status: number, code?: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

export function getToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_KEY)
  } catch {
    return null
  }
}

export function setToken(token: string | null): void {
  try {
    if (token) localStorage.setItem(TOKEN_KEY, token)
    else localStorage.removeItem(TOKEN_KEY)
  } catch {
    /* storage disabled */
  }
}

export function getCachedMe(): Me | null {
  try {
    const raw = localStorage.getItem(ME_KEY)
    return raw ? (JSON.parse(raw) as Me) : null
  } catch {
    return null
  }
}

export function setCachedMe(me: Me | null): void {
  try {
    if (me) localStorage.setItem(ME_KEY, JSON.stringify(me))
    else localStorage.removeItem(ME_KEY)
  } catch {
    /* storage disabled */
  }
}

export function clearSession(): void {
  setToken(null)
  setCachedMe(null)
}

export function logoutRedirect(): void {
  clearSession()
  window.location.assign('/dev/login')
}

type QueryValue = string | number | boolean | undefined | null

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE'
  body?: unknown
  query?: Record<string, QueryValue>
  signal?: AbortSignal
  /** Keep the raw {data, links, meta} envelope (cursor lists). */
  unwrap?: boolean
}

export async function api<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const url = new URL(path, window.location.origin)
  if (options.query) {
    for (const [key, value] of Object.entries(options.query)) {
      if (value === undefined || value === null || value === '') continue
      url.searchParams.set(key, String(value))
    }
  }
  const headers = new Headers()
  if (options.body !== undefined) headers.set('Content-Type', 'application/json')
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)

  const response = await fetch(url.toString(), {
    method: options.method ?? 'GET',
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
    signal: options.signal,
  })

  const isJSON = response.headers.get('content-type')?.includes('json') ?? false
  const payload = isJSON ? await response.json().catch(() => null) : null

  if (!response.ok) {
    if (response.status === 401) {
      clearSession()
      if (!window.location.pathname.endsWith('/login')) window.location.assign('/dev/login')
    }
    const detail =
      (payload as { detail?: string; title?: string; error_code?: string } | null)?.detail ??
      (payload as { title?: string } | null)?.title ??
      `Request failed (${response.status})`
    throw new ApiError(detail, response.status, (payload as { error_code?: string } | null)?.error_code)
  }

  if (payload && typeof payload === 'object' && 'data' in payload) {
    if (options.unwrap === false) return payload as T
    return (payload as { data: T }).data
  }
  return payload as T
}

export async function apiBlob(path: string, signal?: AbortSignal): Promise<Blob> {
  const response = await fetch(new URL(path, window.location.origin).toString(), {
    headers: getToken() ? { Authorization: `Bearer ${getToken()}` } : undefined,
    signal,
  })
  if (!response.ok) {
    if (response.status === 401) {
      clearSession()
      if (!window.location.pathname.endsWith('/login')) window.location.assign('/dev/login')
    }
    let detail = `Request failed (${response.status})`
    try {
      const payload = (await response.json()) as { detail?: string; title?: string; error_code?: string }
      detail = payload.detail ?? payload.title ?? detail
    } catch {
      /* non-JSON error body */
    }
    throw new ApiError(detail, response.status)
  }
  return response.blob()
}

export async function login(email: string, password: string): Promise<LoginResult> {
  const response = await fetch('/api/v1/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password, device: 'dev-dashboard' }),
  })
  const payload = await response.json().catch(() => null)
  if (!response.ok) {
    const detail =
      (payload as { detail?: string; title?: string } | null)?.detail ??
      (payload as { title?: string } | null)?.title ??
      `Login failed (${response.status})`
    throw new ApiError(detail, response.status, (payload as { error_code?: string } | null)?.error_code)
  }
  return (payload as { data: LoginResult }).data
}

export function fetchMe(): Promise<Me> {
  return api<Me>('/dev/api/me')
}

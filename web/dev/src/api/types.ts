import type { components } from './schema'

export type DevAction = components['schemas']['DevAction']
export type DevQueue = components['schemas']['DevQueue']
export type DevJob = components['schemas']['DevJob']
export type DevRequestSummary = components['schemas']['DevRequestSummary']
export type DevRequestRecord = components['schemas']['DevRequest']
export type DevRequestDetail = components['schemas']['DevRequestDetail']
export type DevLog = components['schemas']['DevLog']
export type DevException = components['schemas']['DevException']
export type DevQuery = components['schemas']['DevQuery']
export type DevCacheEvent = components['schemas']['DevCacheEvent']
export type DevQueueEvent = components['schemas']['DevQueueEvent']

export interface Me {
  user_id: number
  name: string
  email: string
  tenant_id?: number | null
  branch_id?: number | null
  is_super_admin: boolean
  permissions: string[]
}

export interface CursorPage<T> {
  data: T[]
  meta?: { next_cursor?: string }
  links?: unknown
}

export interface LoginResult {
  status: string
  token?: string
  message?: string
  user?: {
    id: number
    name: string
    email: string
    roles?: { id: number; name: string }[]
  }
}

export interface HealthCheck {
  name: string
  label: string
  status: 'ok' | 'warning' | 'failed' | 'crashed'
  shortSummary: string
  notificationMessage?: string
  meta?: Record<string, unknown>
}

export interface HealthResults {
  finishedAt: number
  checkResults: HealthCheck[]
}

export interface SystemInfo {
  name: string
  environment: string
  version: string
  timezone: string
  go_version?: string
  started_at?: string
  uptime_seconds?: number
}

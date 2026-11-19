export function formatTime(value: string | number): string {
  const date = new Date(value)
  return date.toLocaleTimeString(undefined, { hour12: false })
}

export function formatDateTime(value: string | number): string {
  const date = new Date(value)
  return date.toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })
}

export function relativeTime(value: string | number): string {
  const then = new Date(value).getTime()
  const delta = Date.now() - then
  if (Number.isNaN(delta)) return '—'
  const seconds = Math.round(delta / 1000)
  if (seconds < 5) return 'just now'
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.round(hours / 24)
  return `${days}d ago`
}

export function formatDuration(ms: number | undefined | null): string {
  if (ms === undefined || ms === null || Number.isNaN(ms)) return '—'
  if (ms < 1) return `${Math.round(ms * 1000)}µs`
  if (ms < 1000) return `${ms.toFixed(ms < 10 ? 2 : 1)}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

export function formatBytes(bytes: number | undefined | null): string {
  if (bytes === undefined || bytes === null) return '—'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`
}

export function statusTone(status: number): 'green' | 'blue' | 'yellow' | 'red' | 'gray' {
  if (status >= 500) return 'red'
  if (status >= 400) return 'yellow'
  if (status >= 300) return 'blue'
  if (status >= 200) return 'green'
  return 'gray'
}

const stateTones: Record<string, 'green' | 'blue' | 'yellow' | 'red' | 'gray' | 'purple' | 'orange'> = {
  available: 'blue',
  cancelled: 'yellow',
  completed: 'gray',
  discarded: 'red',
  pending: 'purple',
  retryable: 'orange',
  running: 'green',
  scheduled: 'purple',
}

export function stateTone(state: string): 'green' | 'blue' | 'yellow' | 'red' | 'gray' | 'purple' | 'orange' {
  return stateTones[state] ?? 'gray'
}

export function levelTone(level: string): 'green' | 'blue' | 'yellow' | 'red' | 'gray' | 'purple' | 'orange' {
  switch (level.toUpperCase()) {
    case 'ERROR':
    case 'CRITICAL':
      return 'red'
    case 'WARN':
    case 'WARNING':
      return 'yellow'
    case 'INFO':
      return 'blue'
    case 'DEBUG':
    case 'TRACE':
      return 'gray'
    default:
      return 'purple'
  }
}

export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '?'
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase()
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase()
}

export function prettyJSON(value: unknown): string {
  if (value === undefined || value === null) return '—'
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

export function shortRequestID(value: string): string {
  if (!value) return '—'
  return value.length > 12 ? value.slice(0, 12) : value
}

/** "in 3 days", "in 2 years" or "5 days ago" for dates either side of now. */
export function relativeDate(value: string | number): string {
  const delta = new Date(value).getTime() - Date.now()
  if (Number.isNaN(delta)) return '—'
  const future = delta > 0
  const days = Math.abs(delta) / 86_400_000
  let text: string
  if (days < 1) text = `${Math.max(1, Math.round(Math.abs(delta) / 3_600_000))}h`
  else if (days < 60) text = `${Math.round(days)} day${Math.round(days) === 1 ? '' : 's'}`
  else if (days < 730) text = `${Math.round(days / 30)} months`
  else text = `${Math.round(days / 365)} years`
  return future ? `in ${text}` : `${text} ago`
}

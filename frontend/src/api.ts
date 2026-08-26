const API_URL = (import.meta.env.VITE_API_URL ?? 'http://localhost:8080').replace(/\/+$/, '')

export type User = {
  id: number
  email: string
  role: string
  name: string | null
  email_verified: boolean
  has_password: boolean
  has_google: boolean
  created_at: string
}

export type Session = {
  access_token: string
  refresh_token: string
  token_type: string
  expires_in: number
  user: User
}

export type ActivityEvent = {
  id: number
  type: string
  payload: Record<string, unknown>
  occurred_at: string
  created_at: string
  idempotency_key?: string
}

export type EventPage = {
  items: ActivityEvent[]
  next_cursor?: string
}

export type Bucket = {
  bucket_start: string
  bucket_end: string
  event_count: number
  type_counts: Record<string, number>
  first_event_at: string
  last_event_at: string
}

export type Day = {
  day: string
  event_count: number
  type_counts: Record<string, number>
}

export type Stats = {
  from: string
  to: string
  bucket: string
  buckets?: Bucket[]
  days?: Day[]
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

async function call<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(`${API_URL}${path}`, {
    ...init,
    headers: { 'Content-Type': 'application/json', ...(init.headers ?? {}) },
  })

  if (!response.ok) {
    throw await apiError(response)
  }

  if (response.status === 204) {
    return undefined as T
  }

  return (await response.json()) as T
}

async function apiError(response: Response): Promise<ApiError> {
  try {
    const body = (await response.json()) as { error?: { code?: string; message?: string } }
    return new ApiError(
      response.status,
      body.error?.code ?? 'unknown',
      body.error?.message ?? `Request failed with ${response.status}`,
    )
  } catch {
    return new ApiError(response.status, 'unknown', `Request failed with ${response.status}`)
  }
}

function authorized(token: string): HeadersInit {
  return { Authorization: `Bearer ${token}` }
}

export function signIn(email: string, password: string): Promise<Session> {
  return call<Session>('/api/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify({ email, password }),
  })
}

export function signUp(email: string, password: string): Promise<Session> {
  return call<Session>('/api/v1/auth/register', {
    method: 'POST',
    body: JSON.stringify({ email, password }),
  })
}

export function refreshSession(refreshToken: string): Promise<Session> {
  return call<Session>('/api/v1/auth/refresh', {
    method: 'POST',
    body: JSON.stringify({ refresh_token: refreshToken }),
  })
}

export async function signOut(refreshToken: string): Promise<void> {
  try {
    await call('/api/v1/auth/logout', {
      method: 'POST',
      body: JSON.stringify({ refresh_token: refreshToken }),
    })
  } catch {
  }
}

export function googleSignInURL(redirectTo = '/'): string {
  return `${API_URL}/api/v1/auth/google/start?redirect_to=${encodeURIComponent(redirectTo)}`
}

export function exchangeGoogleCode(code: string): Promise<Session> {
  return call<Session>('/api/v1/auth/google/exchange', {
    method: 'POST',
    body: JSON.stringify({ code }),
  })
}

export function listEvents(
  token: string,
  params: { limit?: number; cursor?: string; from?: string; type?: string } = {},
): Promise<EventPage> {
  const query = new URLSearchParams()
  if (params.limit) query.set('limit', String(params.limit))
  if (params.cursor) query.set('cursor', params.cursor)
  if (params.from) query.set('from', params.from)
  if (params.type) query.set('type', params.type)

  return call<EventPage>(`/api/v1/events?${query}`, { headers: authorized(token) })
}

export function createEvent(
  token: string,
  input: { type: string; payload?: Record<string, unknown>; idempotency_key?: string },
): Promise<ActivityEvent> {
  return call<ActivityEvent>('/api/v1/events', {
    method: 'POST',
    headers: authorized(token),
    body: JSON.stringify(input),
  })
}

export function getStats(
  token: string,
  params: { from: string; granularity?: 'bucket' | 'day' },
): Promise<Stats> {
  const query = new URLSearchParams({ from: params.from })
  if (params.granularity === 'day') query.set('granularity', 'day')

  return call<Stats>(`/api/v1/stats/activity?${query}`, { headers: authorized(token) })
}

export async function streamEvents(
  token: string,
  onEvent: (event: ActivityEvent) => void,
  signal: AbortSignal,
): Promise<void> {
  const response = await fetch(`${API_URL}/api/v1/events/stream`, {
    headers: authorized(token),
    signal,
  })

  if (!response.ok) {
    throw await apiError(response)
  }
  if (!response.body) {
    throw new ApiError(response.status, 'stream_unsupported', 'This browser cannot read the stream')
  }

  const reader = response.body.pipeThrough(new TextDecoderStream()).getReader()
  let buffer = ''

  while (true) {
    const { done, value } = await reader.read()
    if (done) {
      return
    }

    buffer += value

    let boundary = buffer.indexOf('\n\n')
    while (boundary !== -1) {
      const frame = buffer.slice(0, boundary)
      buffer = buffer.slice(boundary + 2)
      emit(frame, onEvent)
      boundary = buffer.indexOf('\n\n')
    }
  }
}

function emit(frame: string, onEvent: (event: ActivityEvent) => void): void {
  const data = frame
    .split('\n')
    .filter((line) => line.startsWith('data:'))
    .map((line) => line.slice('data:'.length).trim())
    .join('')

  if (!data) {
    return
  }

  try {
    onEvent(JSON.parse(data) as ActivityEvent)
  } catch {
  }
}

export type RunStatus = 'succeeded' | 'failed' | 'skipped'
export type RunTrigger = 'schedule' | 'manual'

export type AggregationRun = {
  id: number
  bucket_start: string
  bucket_end: string
  status: RunStatus
  trigger: RunTrigger
  users_touched: number
  started_at: string
  finished_at?: string
  error?: string
}

export function listRuns(token: string, limit = 12): Promise<{ items: AggregationRun[] }> {
  return call<{ items: AggregationRun[] }>(`/api/v1/admin/aggregation/runs?limit=${limit}`, {
    headers: authorized(token),
  })
}

export function triggerRun(token: string, bucketStart?: string): Promise<AggregationRun> {
  return call<AggregationRun>('/api/v1/admin/aggregation/runs', {
    method: 'POST',
    headers: authorized(token),
    body: JSON.stringify(bucketStart ? { bucket_start: bucketStart } : {}),
  })
}

export type AuthMethods = {
  password: boolean
  google: boolean
}

export function getAuthMethods(): Promise<AuthMethods> {
  return call<AuthMethods>('/api/v1/auth/methods')
}

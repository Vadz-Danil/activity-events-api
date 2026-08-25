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

export function googleSignInURL(redirectTo = '/'): string {
  return `${API_URL}/api/v1/auth/google/start?redirect_to=${encodeURIComponent(redirectTo)}`
}

export function exchangeGoogleCode(code: string): Promise<Session> {
  return post<Session>('/api/v1/auth/google/exchange', { code })
}

export async function signOut(refreshToken: string): Promise<void> {
  await fetch(`${API_URL}/api/v1/auth/logout`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ refresh_token: refreshToken }),
  })
}

async function post<T>(path: string, body: unknown): Promise<T> {
  const response = await fetch(`${API_URL}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })

  if (!response.ok) {
    throw new Error(await errorMessage(response))
  }

  return (await response.json()) as T
}

async function errorMessage(response: Response): Promise<string> {
  try {
    const payload = (await response.json()) as { error?: { message?: string } }
    return payload.error?.message ?? `Request failed with ${response.status}`
  } catch {
    return `Request failed with ${response.status}`
  }
}

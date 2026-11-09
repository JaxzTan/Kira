import type { ErrorCode, ErrorResponse } from './types'

/** Typed API failure. `fields` is set for code === 'validation'. */
export class ApiError extends Error {
  readonly status: number
  readonly code: ErrorCode
  readonly fields: Record<string, string> | null

  constructor(status: number, body: Partial<ErrorResponse> & { error: string }) {
    super(body.error)
    this.name = 'ApiError'
    this.status = status
    this.code = body.code ?? 'internal'
    this.fields = body.fields ?? null
  }
}

export function isApiError(e: unknown): e is ApiError {
  return e instanceof ApiError
}

/** True when the request never reached the server (retry-able). */
export function isNetworkError(e: unknown): boolean {
  return e instanceof ApiError && e.status === 0
}

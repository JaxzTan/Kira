import { realApi } from './client'
import { createMockApi } from './mock'
import type { KiraApi } from './types'

/** Pick the implementation: VITE_MOCK_API=1 -> in-memory mock, otherwise fetch client. */
export const api: KiraApi = import.meta.env.VITE_MOCK_API === '1' ? createMockApi() : realApi

export { ApiError, isApiError } from './errors'

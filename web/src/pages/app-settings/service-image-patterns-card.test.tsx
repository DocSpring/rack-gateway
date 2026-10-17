import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ServiceImagePatternsCard } from './service-image-patterns-card'
import type { AppSettingsResponse } from './types'

const mockPut = vi.hoisted(() => vi.fn())
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return { ...actual, api: { ...actual.api, put: mockPut } }
})

const toastError = vi.hoisted(() => vi.fn())
vi.mock('@/components/ui/use-toast', () => ({
  toast: { error: toastError, success: vi.fn() },
}))

function renderCard() {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <ServiceImagePatternsCard app="web" disabled={false} settings={{} as AppSettingsResponse} />
    </QueryClientProvider>
  )
}

function save(json: string) {
  fireEvent.change(screen.getByLabelText(/JSON object/), { target: { value: json } })
  fireEvent.click(screen.getByRole('button', { name: 'Save' }))
}

describe('ServiceImagePatternsCard', () => {
  beforeEach(() => {
    mockPut.mockReset()
    toastError.mockReset()
  })

  it('reports invalid JSON without calling the API', async () => {
    renderCard()
    save('{not json')
    await waitFor(() => expect(toastError).toHaveBeenCalledWith('Invalid JSON format'))
    expect(mockPut).not.toHaveBeenCalled()
  })

  // Only the parse is about the JSON: a failed request must show the server's error, not "Invalid JSON format".
  it("shows the API's error when saving valid JSON fails", async () => {
    mockPut.mockRejectedValue(new Error('admin role required'))
    renderCard()
    save('{"*": "docker\\\\.io/acme/app:{{GIT_COMMIT}}"}')
    await waitFor(() => expect(toastError).toHaveBeenCalledWith('admin role required'))
    expect(mockPut).toHaveBeenCalledWith('/api/v1/apps/web/settings/service-image-patterns', {
      '*': 'docker\\.io/acme/app:{{GIT_COMMIT}}',
    })
    expect(toastError).not.toHaveBeenCalledWith('Invalid JSON format')
  })
})

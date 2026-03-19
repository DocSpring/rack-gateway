import { render } from '@testing-library/react'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AppPage } from './app-page'

const mockNavigate = vi.fn()
const mockUseLocation = vi.fn()

vi.mock('@tanstack/react-router', () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="/">{children}</a>,
  Outlet: () => <div data-testid="app-page-outlet" />,
  useLocation: () => mockUseLocation(),
  useNavigate: () => mockNavigate,
  useParams: () => ({ app: 'docspring' }),
}))

vi.mock('../components/page-layout', () => ({
  PageLayout: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}))

describe('AppPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('redirects the base app route to services', () => {
    mockUseLocation.mockReturnValue({ pathname: '/apps/docspring' })

    render(<AppPage />)

    expect(mockNavigate).toHaveBeenCalledWith({
      to: '/apps/$app/services',
      params: { app: 'docspring' },
      replace: true,
    })
  })

  it('does not redirect when already on a child route', () => {
    mockUseLocation.mockReturnValue({ pathname: '/apps/docspring/processes' })

    render(<AppPage />)

    expect(mockNavigate).not.toHaveBeenCalled()
  })
})

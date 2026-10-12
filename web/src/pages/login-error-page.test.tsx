import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { LoginErrorPage } from './login-error-page'

function renderWithSearch(search: string) {
  window.history.replaceState(null, '', `/app/auth/error${search}`)
  return render(<LoginErrorPage />)
}

describe('LoginErrorPage', () => {
  afterEach(() => {
    window.history.replaceState(null, '', '/')
  })

  it('explains a known reason code and offers to try again', () => {
    renderWithSearch('?error=unauthorized')
    expect(screen.getByRole('heading', { name: 'Account not authorized' })).toBeInTheDocument()
    expect(
      screen.getByText(
        'You are not authorized to access this application. Please contact your administrator.'
      )
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Try again/ })).toHaveAttribute('href', '/app/login')
  })

  it('explains a canceled sign-in', () => {
    renderWithSearch('?error=access_denied')
    expect(screen.getByRole('heading', { name: 'Sign-in canceled' })).toBeInTheDocument()
  })

  it('never shows text from the URL', () => {
    renderWithSearch(
      '?error=unauthorized&message=Call+555-0100+to+restore+access&reason=Call+555-0100'
    )
    expect(screen.getByRole('heading', { name: 'Account not authorized' })).toBeInTheDocument()
    expect(screen.queryByText(/555-0100/)).toBeNull()
  })

  it('shows the generic message for an unknown code without echoing it', () => {
    renderWithSearch('?error=Call+555-0100+to+restore+access')
    expect(screen.getByRole('heading', { name: 'Unable to sign in' })).toBeInTheDocument()
    expect(
      screen.getByText('Something went wrong while completing your login. Please try again.')
    ).toBeInTheDocument()
    expect(screen.queryByText(/555-0100/)).toBeNull()
  })

  it('does not treat object prototype keys as known codes', () => {
    renderWithSearch('?error=constructor')
    expect(screen.getByRole('heading', { name: 'Unable to sign in' })).toBeInTheDocument()
  })

  it('shows the generic message when no code is given', () => {
    renderWithSearch('')
    expect(screen.getByRole('heading', { name: 'Unable to sign in' })).toBeInTheDocument()
  })
})

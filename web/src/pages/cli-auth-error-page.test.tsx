import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { CLIAuthErrorPage } from './cli-auth-error-page'

function renderWithSearch(search: string) {
  window.history.replaceState(null, '', `/app/cli/auth/error${search}`)
  return render(<CLIAuthErrorPage />)
}

describe('CLIAuthErrorPage', () => {
  afterEach(() => {
    window.history.replaceState(null, '', '/')
  })

  it('explains a known error code and how to retry', () => {
    renderWithSearch('?error=canceled')
    expect(screen.getByRole('heading', { name: 'Login canceled' })).toBeInTheDocument()
    expect(screen.getByText('This CLI login was canceled.')).toBeInTheDocument()
    expect(screen.getByText('rack-gateway login')).toBeInTheDocument()
  })

  it('explains errors raised by the CLI itself', () => {
    renderWithSearch('?error=cli_incomplete')
    expect(screen.getByRole('heading', { name: 'Login not completed' })).toBeInTheDocument()
  })

  it('shows the generic message for an unknown code without echoing it', () => {
    renderWithSearch('?error=Call+555-0100+to+restore+access')
    expect(screen.getByRole('heading', { name: 'CLI login failed' })).toBeInTheDocument()
    expect(screen.queryByText(/555-0100/)).toBeNull()
  })

  it('does not treat object prototype keys as known codes', () => {
    renderWithSearch('?error=constructor')
    expect(screen.getByRole('heading', { name: 'CLI login failed' })).toBeInTheDocument()
  })

  it('shows the generic message when no code is given', () => {
    renderWithSearch('')
    expect(screen.getByRole('heading', { name: 'CLI login failed' })).toBeInTheDocument()
  })
})

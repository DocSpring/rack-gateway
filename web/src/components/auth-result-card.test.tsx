import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import { Button } from '@/components/ui/button'
import { AuthResultCard } from './auth-result-card'

vi.mock('@/components/ui/button', () => ({
  Button: ({ children, ...props }: React.ComponentProps<'button'>) => (
    <button type="button" {...props}>
      {children}
    </button>
  ),
}))

describe('AuthResultCard', () => {
  it('renders success styling by default', () => {
    render(<AuthResultCard status="success" title="All good" />)

    expect(screen.getByRole('heading', { name: 'All good' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'All good' }).closest('.text-2xl')).toBeTruthy()
  })

  it('renders children when provided', () => {
    render(
      <AuthResultCard status="success" title="Done">
        <Button>Continue</Button>
      </AuthResultCard>
    )

    expect(screen.getByRole('button', { name: 'Continue' })).toBeInTheDocument()
  })

  it('renders description when provided', () => {
    render(<AuthResultCard status="error" title="Oops" description="Something failed" />)

    expect(screen.getByText('Something failed')).toBeInTheDocument()
  })
})

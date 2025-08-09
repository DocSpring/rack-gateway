import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { UserEditModal } from './UserEditModal'

describe('UserEditModal', () => {
  const mockOnSave = vi.fn()
  const mockOnClose = vi.fn()

  const defaultProps = {
    email: 'test@example.com',
    user: { name: 'Test User', roles: ['viewer'] },
    isNew: false,
    onSave: mockOnSave,
    onClose: mockOnClose,
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders edit mode correctly', () => {
    render(<UserEditModal {...defaultProps} />)

    expect(screen.getByText('Edit User')).toBeInTheDocument()
    expect(screen.getByDisplayValue('test@example.com')).toBeDisabled()
    expect(screen.getByDisplayValue('Test User')).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: /viewer/i })).toBeChecked()
  })

  it('renders add mode correctly', () => {
    render(<UserEditModal {...defaultProps} isNew={true} email="" />)

    expect(screen.getByRole('heading', { name: 'Add User' })).toBeInTheDocument()
    expect(screen.getByLabelText('Email')).not.toBeDisabled()
  })

  it('validates required fields', async () => {
    render(<UserEditModal {...defaultProps} isNew={true} email="" user={{ name: '', roles: [] }} />)

    const saveButton = screen.getByRole('button', { name: 'Add User' })
    fireEvent.click(saveButton)

    await waitFor(() => {
      expect(screen.getByText('Email is required')).toBeInTheDocument()
      expect(screen.getByText('Name is required')).toBeInTheDocument()
      expect(screen.getByText('At least one role is required')).toBeInTheDocument()
    })

    expect(mockOnSave).not.toHaveBeenCalled()
  })

  it('validates email format', async () => {
    render(
      <UserEditModal
        {...defaultProps}
        isNew={true}
        email=""
        user={{ name: 'Test', roles: ['viewer'] }}
      />,
    )

    // Type invalid email
    const emailInput = screen.getByLabelText('Email')
    fireEvent.change(emailInput, { target: { value: 'invalid-email' } })

    // Submit form by clicking button
    const form = emailInput.closest('form')!
    fireEvent.submit(form)

    // Check for validation error
    await waitFor(() => {
      const errorElement = screen.getByText('Invalid email format')
      expect(errorElement).toBeInTheDocument()
    })

    expect(mockOnSave).not.toHaveBeenCalled()
  })

  it('calls onSave with correct data', async () => {
    render(<UserEditModal {...defaultProps} isNew={true} email="" user={{ name: '', roles: [] }} />)

    const emailInput = screen.getByLabelText('Email')
    const nameInput = screen.getByLabelText('Name')
    const adminCheckbox = screen.getByRole('checkbox', { name: /admin/i })

    await userEvent.type(emailInput, 'new@example.com')
    await userEvent.type(nameInput, 'New User')
    await userEvent.click(adminCheckbox)

    const saveButton = screen.getByRole('button', { name: 'Add User' })
    fireEvent.click(saveButton)

    await waitFor(() => {
      expect(mockOnSave).toHaveBeenCalledWith('new@example.com', {
        name: 'New User',
        roles: ['admin'],
      })
    })
  })

  it('calls onClose when cancel is clicked', () => {
    render(<UserEditModal {...defaultProps} />)

    const cancelButton = screen.getByText('Cancel')
    fireEvent.click(cancelButton)

    expect(mockOnClose).toHaveBeenCalled()
  })

  it('toggles roles correctly', async () => {
    render(<UserEditModal {...defaultProps} />)

    const adminCheckbox = screen.getByRole('checkbox', { name: /admin/i })
    const viewerCheckbox = screen.getByRole('checkbox', { name: /viewer/i })

    expect(viewerCheckbox).toBeChecked()
    expect(adminCheckbox).not.toBeChecked()

    await userEvent.click(adminCheckbox)
    expect(adminCheckbox).toBeChecked()

    await userEvent.click(viewerCheckbox)
    expect(viewerCheckbox).not.toBeChecked()
  })
})

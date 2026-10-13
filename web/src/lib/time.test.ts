import { describe, expect, it } from 'vitest'
import { trustLabel } from './time'

describe('trustLabel', () => {
  it('shows the trusted device TTL', () => {
    expect(trustLabel('device', 14)).toBe('Trust this device for 14 days')
    expect(trustLabel('browser', 1)).toBe('Trust this browser for 1 day')
  })

  it('leaves the duration out until it is known', () => {
    expect(trustLabel('device')).toBe('Trust this device')
    expect(trustLabel('device', 0)).toBe('Trust this device')
  })
})

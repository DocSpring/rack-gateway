import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { CommandDetails, scriptListing } from './command-details'

const scriptDetails = JSON.stringify({
  command: 'rails runner count.rb',
  justification: 'Count pro accounts for the pricing review',
  script: "accounts = Account.where(plan: 'pro')\nputs accounts.count\n",
  script_name: 'count.rb',
  script_sha256: 'a'.repeat(64),
  method: 'POST',
})

describe('CommandDetails', () => {
  // A script command shows its justification, the command as typed, and the script's lines, numbered.
  it('shows a script command readably', () => {
    render(<CommandDetails details={scriptDetails} />)
    expect(screen.getByTestId('audit-command-justification')).toHaveTextContent(
      'Justification: Count pro accounts for the pricing review'
    )
    expect(screen.getByTestId('audit-command')).toHaveTextContent('Command: rails runner count.rb')
    const script = screen.getByTestId('audit-command-script')
    expect(script).toHaveTextContent(`Script: count.rb, 2 lines, SHA-256 ${'a'.repeat(64)}`)
    expect(script.querySelector('pre')?.textContent).toBe(
      "1 | accounts = Account.where(plan: 'pro')\n2 | puts accounts.count"
    )
  })

  // A plain command has no script block, and an entry without a command shows nothing.
  it('shows only what the entry has', () => {
    const { container, rerender } = render(
      <CommandDetails details={JSON.stringify({ command: 'ls -la' })} />
    )
    expect(screen.getByTestId('audit-command')).toHaveTextContent('Command: ls -la')
    expect(screen.queryByTestId('audit-command-justification')).toBeNull()
    expect(screen.queryByTestId('audit-command-script')).toBeNull()

    for (const details of ['{"method":"GET"}', 'not json', '"a string"', null]) {
      rerender(<CommandDetails details={details} />)
      expect(container).toBeEmptyDOMElement()
    }
  })

  // Characters that would make the script read differently from what ran are written out and counted, with a
  // warning. CRLF line endings aren't.
  it('writes out hidden characters', () => {
    expect(scriptListing('ok = true\r\nif ok ‮} {⁦\r\nputs​ 1\rputs 2\n')).toEqual({
      listing: '1 | ok = true\n2 | if ok <U+202E>} {<U+2066>\n3 | puts<U+200B> 1<U+000D>puts 2',
      lines: 3,
      hidden: 4,
    })
    render(
      <CommandDetails
        details={JSON.stringify({ command: 'sh x.sh', script: 'echo​ 1', script_name: 'x.sh' })}
      />
    )
    expect(screen.getByTestId('audit-command-script')).toHaveTextContent(
      'Script: x.sh, 1 line, SHA-256 The script has 1 invisible or direction-changing characters'
    )
  })
})

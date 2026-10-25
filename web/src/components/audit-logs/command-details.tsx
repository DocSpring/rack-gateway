import { useMemo } from 'react'

// CommandAuditDetails are the audit details of an exec or process start's command.
interface CommandAuditDetails {
  command: string
  justification?: string
  script?: string
  scriptName?: string
  scriptSHA256?: string
}

function stringField(record: Record<string, unknown>, key: string): string | undefined {
  const value = record[key]
  return typeof value === 'string' && value !== '' ? value : undefined
}

// parseCommandDetails reads the command fields from an audit entry's details JSON, or returns null when it has no
// command.
function parseCommandDetails(details: string | null | undefined): CommandAuditDetails | null {
  let parsed: unknown
  try {
    parsed = JSON.parse(details ?? '')
  } catch {
    return null
  }
  if (typeof parsed !== 'object' || parsed === null) {
    return null
  }
  const record = parsed as Record<string, unknown>
  const command = stringField(record, 'command')
  if (!command) {
    return null
  }
  return {
    command,
    justification: stringField(record, 'justification'),
    script: stringField(record, 'script'),
    scriptName: stringField(record, 'script_name'),
    scriptSHA256: stringField(record, 'script_sha256'),
  }
}

// hiddenCharacter matches a character that doesn't show as itself: a control character (tab and line feed are
// handled apart), a format character (zero-width and bidirectional controls), or a line or paragraph separator.
const hiddenCharacter = /[\p{Cc}\p{Cf}\p{Zl}\p{Zp}]/u

function codePoint(char: string): string {
  return `<U+${(char.codePointAt(0) ?? 0).toString(16).toUpperCase().padStart(4, '0')}>`
}

// scriptListing returns the script with numbered lines and every character that wouldn't show as itself written
// as <U+XXXX>, as the CLI shows it for review, with the number of lines and of such characters. CRLF line endings
// count as line breaks.
export function scriptListing(text: string): { listing: string; lines: number; hidden: number } {
  const unified = text.replaceAll('\r\n', '\n')
  const lines = (unified.endsWith('\n') ? unified.slice(0, -1) : unified).split('\n')
  const width = String(lines.length).length
  let hidden = 0
  const numbered = lines.map((line, i) => {
    const shown = Array.from(line, (char) => {
      if (char === '\t' || !hiddenCharacter.test(char)) {
        return char
      }
      hidden += 1
      return codePoint(char)
    })
    return `${String(i + 1).padStart(width)} | ${shown.join('')}`
  })
  return { listing: numbered.join('\n'), lines: lines.length, hidden }
}

function ScriptListing({ details }: { details: CommandAuditDetails }) {
  const { listing, lines, hidden } = useMemo(
    () => scriptListing(details.script ?? ''),
    [details.script]
  )
  return (
    <div data-testid="audit-command-script">
      <div className="break-all">
        <span className="text-muted-foreground">Script:</span> {details.scriptName}, {lines}{' '}
        {lines === 1 ? 'line' : 'lines'}, SHA-256 <code>{details.scriptSHA256}</code>
      </div>
      {hidden > 0 && (
        <div className="text-destructive">
          The script has {hidden} invisible or direction-changing characters, shown as
          &lt;U+XXXX&gt;.
        </div>
      )}
      <pre className="mt-2 max-h-96 overflow-auto rounded bg-muted p-2 text-xs">{listing}</pre>
    </div>
  )
}

// CommandDetails shows the command an exec or process start ran, from its audit details: its justification, the
// command, and a script's text with numbered lines (the details JSON would show it as one escaped string).
export function CommandDetails({ details }: { details: string | null | undefined }) {
  const command = useMemo(() => parseCommandDetails(details), [details])
  if (!command) {
    return null
  }
  return (
    <>
      {command.justification && (
        <div className="break-words" data-testid="audit-command-justification">
          <span className="text-muted-foreground">Justification:</span> {command.justification}
        </div>
      )}
      <div className="break-all" data-testid="audit-command">
        <span className="text-muted-foreground">Command:</span>{' '}
        <code className="rounded border bg-secondary px-1 py-0.5">{command.command}</code>
      </div>
      {command.script && <ScriptListing details={command} />}
    </>
  )
}

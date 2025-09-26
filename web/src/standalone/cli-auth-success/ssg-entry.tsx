import { renderToStaticMarkup } from 'react-dom/server'
import { CLIAuthSuccessDocument } from './document'

export function render({ cssHrefs }: { cssHrefs: string[] }) {
  const html = renderToStaticMarkup(<CLIAuthSuccessDocument cssHrefs={cssHrefs} />)
  return `<!DOCTYPE html>${html}`
}

import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { pathToFileURL } from 'node:url'

type ManifestEntry = {
  css?: string[]
}

const distDir = path.resolve(process.cwd(), 'dist')
const manifestPath = path.resolve(distDir, '.vite/manifest.json')
const ssrEntryPath = path.resolve(distDir, 'ssr/cli-auth-success/ssg-entry.js')

async function main() {
  const manifest = JSON.parse(readFileSync(manifestPath, 'utf-8')) as Record<string, ManifestEntry>
  const entryWithCss = Object.values(manifest).find(
    (entry) => Array.isArray(entry.css) && entry.css.length > 0
  )

  if (!entryWithCss?.css) {
    throw new Error('Unable to locate main stylesheet in manifest')
  }

  const cssHrefs = entryWithCss.css.map((href) => `/.gateway/web/${href}`)
  const { render } = (await import(pathToFileURL(ssrEntryPath).href)) as {
    render: (args: { cssHrefs: string[] }) => string
  }
  const html = render({ cssHrefs })
  const outDir = path.resolve(distDir, 'cli/auth/success')
  mkdirSync(outDir, { recursive: true })
  writeFileSync(path.join(outDir, 'index.html'), html)
}

main().catch((error) => {
  console.error('[prerender-cli-auth-success] failed:', error)
  process.exitCode = 1
})

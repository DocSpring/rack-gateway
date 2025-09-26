import { CLIAuthSuccessContent } from '@/pages/cli-auth-success-content'

type DocumentProps = {
  cssHrefs: string[]
}

export function CLIAuthSuccessDocument({ cssHrefs }: DocumentProps) {
  return (
    <html className="dark" lang="en">
      {/* biome-ignore lint/style/noHeadElement: static standalone document */}
      <head>
        <meta charSet="utf-8" />
        <meta content="width=device-width, initial-scale=1" name="viewport" />
        <title>Authentication Complete</title>
        <link href="/.gateway/web/favicon.ico" rel="icon" />
        <link href="/.gateway/web/favicon-96x96.png" rel="icon" sizes="96x96" type="image/png" />
        <link href="/.gateway/web/favicon.svg" rel="icon" type="image/svg+xml" />
        <link href="/.gateway/web/apple-touch-icon.png" rel="apple-touch-icon" sizes="180x180" />
        <link href="/.gateway/web/site.webmanifest" rel="manifest" />
        {cssHrefs.map((href) => (
          <link href={href} key={href} rel="stylesheet" />
        ))}
      </head>
      <body className="bg-background text-foreground antialiased">
        <div id="root">
          <CLIAuthSuccessContent />
        </div>
      </body>
    </html>
  )
}

export default CLIAuthSuccessDocument

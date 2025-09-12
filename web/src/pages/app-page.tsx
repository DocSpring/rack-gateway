import { Link, Outlet, useLocation, useParams } from '@tanstack/react-router'
import { useEffect } from 'react'

export function AppPage() {
  const { app } = useParams({ from: '/apps/$app' }) as { app: string }
  const location = useLocation()

  // Redirect /apps/:app to processes subpage
  useEffect(() => {
    const base = `/apps/${app}`
    if (location.pathname === base) {
      window.history.replaceState(null, '', `${base}/processes`)
    }
  }, [app, location.pathname])

  const tabs = [
    { key: 'processes', name: 'Processes', to: '/apps/$app/processes' as const },
    { key: 'builds', name: 'Builds', to: '/apps/$app/builds' as const },
    { key: 'releases', name: 'Releases', to: '/apps/$app/releases' as const },
  ]

  return (
    <div className="mx-auto max-w-6xl p-6">
      <h2 className="mb-4 font-semibold text-2xl">App: {app}</h2>
      <div className="mb-4 flex gap-2">
        {tabs.map((t) => (
          <Link
            className="rounded-md border px-3 py-1 text-sm hover:bg-accent hover:text-accent-foreground"
            key={t.key}
            params={{ app }}
            to={t.to}
          >
            {t.name}
          </Link>
        ))}
      </div>
      <Outlet />
    </div>
  )
}

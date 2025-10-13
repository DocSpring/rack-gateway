import { Link, Outlet, useLocation, useParams } from '@tanstack/react-router'
import { useEffect } from 'react'
import { PageLayout } from '../components/page-layout'

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
    {
      key: 'processes',
      name: 'Processes',
      to: '/apps/$app/processes' as const,
    },
    { key: 'builds', name: 'Builds', to: '/apps/$app/builds' as const },
    { key: 'releases', name: 'Releases', to: '/apps/$app/releases' as const },
    { key: 'env', name: 'Environment', to: '/apps/$app/env' as const },
    { key: 'settings', name: 'Settings', to: '/apps/$app/settings' as const },
  ]

  return (
    <PageLayout title={`App: ${app}`}>
      <div className="mb-4 flex gap-2">
        {tabs.map((t) => (
          <Link
            activeOptions={{ exact: true }}
            activeProps={{
              className: 'bg-accent text-white hover:bg-accent/90',
            }}
            className="rounded-md border px-3 py-1 text-muted-foreground text-sm hover:bg-accent hover:text-accent-foreground"
            data-testid={`app-tab-${t.key}`}
            key={t.key}
            params={{ app }}
            to={t.to}
          >
            {t.name}
          </Link>
        ))}
      </div>
      <Outlet />
    </PageLayout>
  )
}

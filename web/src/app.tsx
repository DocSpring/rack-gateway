import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { Layout } from './components/layout'
import { Toaster } from './components/ui/toaster'
import { ApiErrorProvider } from './contexts/api-error-provider'
import { AuthProvider } from './contexts/auth-context'
import { HttpClientProvider } from './contexts/http-client-context'
import { StepUpProvider } from './contexts/step-up-context'
import { AccountSecurityPage } from './pages/account-security-page'
import { AllBuildsPage } from './pages/all-builds-page'
import { AllProcessesPage } from './pages/all-processes-page'
import { AllReleasesPage } from './pages/all-releases-page'
import { AppBuildsPage } from './pages/app-builds-page'
import { AppEnvPage } from './pages/app-env-page'
import { AppPage } from './pages/app-page'
import { AppProcessesPage } from './pages/app-processes-page'
import { AppReleasesPage } from './pages/app-releases-page'
import { AppSettingsPage } from './pages/app-settings-page'
import { AppsListPage } from './pages/apps-list-page'
import { AuditPage } from './pages/audit-page'
import { CallbackPage } from './pages/callback-page'
import { CLIAuthSuccessPage } from './pages/cli-auth-success-page'
import { DeployApprovalRequestDetailPage } from './pages/deploy-approval-request-detail-page'
import { DeployApprovalRequestsPage } from './pages/deploy-approval-requests-page'
import { InstancesPage } from './pages/instances-page'
import { IntegrationsPage } from './pages/integrations-page'
import { JobsPage } from './pages/jobs-page/index'
import { LoginErrorPage } from './pages/login-error-page'
import { LoginPage } from './pages/login-page'
import { MFAChallengePage } from './pages/mfa-challenge-page'
import { RackPage } from './pages/rack-page'
import { SettingsPage } from './pages/settings-page'
import { TokensPage } from './pages/tokens-page/index'
import { UserAuditPage } from './pages/user-audit-page'
import { UserPage } from './pages/user-page'
import { UsersPage } from './pages/users-page'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 60 * 1000, // 1 minute
      retry: 1,
    },
  },
})

function buildRouteTree() {
  const rootRoute = createRootRoute({
    component: () => <Outlet />,
  })

  // Public routes
  const loginRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: 'login',
    component: LoginPage,
  })
  const callbackRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: 'auth/callback',
    component: CallbackPage,
  })
  const loginErrorRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: 'auth/error',
    component: LoginErrorPage,
  })

  const mfaChallengeRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: 'auth/mfa/challenge',
    component: MFAChallengePage,
  })
  const cliAuthSuccessRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: 'cli/auth/success',
    component: CLIAuthSuccessPage,
  })

  // App layout route with nested pages
  const layoutRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/',
    component: Layout,
  })

  // No separate index route; default redirect handled by Layout component

  const usersRoute = createRoute({
    getParentRoute: () => layoutRoute,
    path: 'users',
    component: UsersPage,
  })
  const userRoute = createRoute({
    getParentRoute: () => layoutRoute,
    path: 'users/$email',
    component: UserPage,
  })
  const userAuditRoute = createRoute({
    getParentRoute: () => layoutRoute,
    path: 'users/$email/audit-logs',
    component: UserAuditPage,
  })

  const tokensRoute = createRoute({
    getParentRoute: () => layoutRoute,
    path: 'api-tokens',
    component: TokensPage,
  })

  const jobsRoute = createRoute({
    getParentRoute: () => layoutRoute,
    path: 'jobs',
    component: JobsPage,
  })

  const deployApprovalRequestsRoute = createRoute({
    getParentRoute: () => layoutRoute,
    path: 'deploy-approval-requests',
    component: DeployApprovalRequestsPage,
  })

  const deployApprovalRequestDetailRoute = createRoute({
    getParentRoute: () => layoutRoute,
    path: 'deploy-approval-requests/$id',
    component: DeployApprovalRequestDetailPage,
  })

  const accountSecurityRoute = createRoute({
    getParentRoute: () => layoutRoute,
    path: 'account/security',
    component: AccountSecurityPage,
  })

  const auditRoute = createRoute({
    getParentRoute: () => layoutRoute,
    path: 'audit-logs',
    component: AuditPage,
  })

  const settingsRoute = createRoute({
    getParentRoute: () => layoutRoute,
    path: 'settings',
    component: SettingsPage,
  })

  const integrationsRoute = createRoute({
    getParentRoute: () => layoutRoute,
    path: 'integrations',
    component: IntegrationsPage,
  })

  const rackRoute = createRoute({
    getParentRoute: () => layoutRoute,
    path: 'rack',
    component: RackPage,
  })

  const appsListRoute = createRoute({
    getParentRoute: () => layoutRoute,
    path: 'apps',
    component: AppsListPage,
  })
  const instancesRoute = createRoute({
    getParentRoute: () => layoutRoute,
    path: 'instances',
    component: InstancesPage,
  })
  const allBuildsRoute = createRoute({
    getParentRoute: () => layoutRoute,
    path: 'builds',
    component: AllBuildsPage,
  })
  const allReleasesRoute = createRoute({
    getParentRoute: () => layoutRoute,
    path: 'releases',
    component: AllReleasesPage,
  })
  const allProcessesRoute = createRoute({
    getParentRoute: () => layoutRoute,
    path: 'processes',
    component: AllProcessesPage,
  })

  const appRoute = createRoute({
    getParentRoute: () => layoutRoute,
    path: 'apps/$app',
    component: AppPage,
  })

  const appProcsRoute = createRoute({
    getParentRoute: () => appRoute,
    path: 'processes',
    component: AppProcessesPage,
  })
  const appBuildsRoute = createRoute({
    getParentRoute: () => appRoute,
    path: 'builds',
    component: AppBuildsPage,
  })
  const appReleasesRoute = createRoute({
    getParentRoute: () => appRoute,
    path: 'releases',
    component: AppReleasesPage,
  })
  const appEnvRoute = createRoute({
    getParentRoute: () => appRoute,
    path: 'env',
    component: AppEnvPage,
  })
  const appSettingsRoute = createRoute({
    getParentRoute: () => appRoute,
    path: 'settings',
    component: AppSettingsPage,
  })

  const layoutChildren = [
    rackRoute,
    instancesRoute,
    allBuildsRoute,
    allReleasesRoute,
    allProcessesRoute,
    appsListRoute,
    appRoute.addChildren([
      appProcsRoute,
      appBuildsRoute,
      appReleasesRoute,
      appEnvRoute,
      appSettingsRoute,
    ]),
    usersRoute,
    userRoute,
    userAuditRoute,
    tokensRoute,
    jobsRoute,
    deployApprovalRequestsRoute,
    deployApprovalRequestDetailRoute,
    auditRoute,
    settingsRoute,
    integrationsRoute,
    accountSecurityRoute,
  ]

  return rootRoute.addChildren([
    loginRoute,
    callbackRoute,
    loginErrorRoute,
    mfaChallengeRoute,
    cliAuthSuccessRoute,
    layoutRoute.addChildren(layoutChildren),
  ])
}

const TRAILING_SLASH_RE = /\/$/

export function detectBasepath() {
  // Avoid any by narrowing to a minimal env shape
  const envBase = (import.meta as { env?: { BASE_URL?: string } }).env?.BASE_URL
  const base = envBase && envBase !== '' ? envBase : '/'
  if (base === '/' && typeof window !== 'undefined') {
    try {
      const p = window.location.pathname || '/'
      if (p.startsWith('/web')) {
        return '/web'
      }
    } catch {
      // Ignore errors in non-browser test environments
    }
  }
  return base.replace(TRAILING_SLASH_RE, '')
}

function createAppRouter(basepath?: string) {
  return createRouter({
    routeTree: buildRouteTree(),
    basepath: basepath ?? detectBasepath(),
  })
}

function getSingletonRouter() {
  // Reuse a single router instance across HMR to avoid double route registration
  try {
    const w = window as unknown as {
      __APP_ROUTER__?: ReturnType<typeof createAppRouter>
    }
    if (w.__APP_ROUTER__) {
      return w.__APP_ROUTER__
    }
    const r = createAppRouter()
    w.__APP_ROUTER__ = r
    return r
  } catch {
    // Non-browser (tests): create a fresh router
    return createAppRouter()
  }
}

function App() {
  const router = getSingletonRouter()
  return (
    <HttpClientProvider>
      <ApiErrorProvider>
        <QueryClientProvider client={queryClient}>
          <AuthProvider>
            <StepUpProvider>
              <RouterProvider router={router} />
              <Toaster />
            </StepUpProvider>
          </AuthProvider>
        </QueryClientProvider>
      </ApiErrorProvider>
    </HttpClientProvider>
  )
}

export default App

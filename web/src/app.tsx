import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Navigate, Route, BrowserRouter as Router, Routes } from 'react-router-dom'
import { Layout } from './components/layout'
import { ProtectedRoute } from './components/protected-route'
import { AuthProvider } from './contexts/auth-context'
import { CallbackPage } from './pages/callback-page'
import { LoginPage } from './pages/login-page'
import { UsersPage } from './pages/users-page'
import { TokensPage } from './pages/tokens-page'
import { AuditPage } from './pages/audit-page'
import { Toaster } from './components/ui/sonner'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 60 * 1000, // 1 minute
      retry: 1,
    },
  },
})

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <Router>
          <Routes>
            <Route element={<LoginPage />} path="/login" />
            <Route element={<CallbackPage />} path="/auth/callback" />

            <Route element={<ProtectedRoute />}>
              <Route element={<Layout />}>
                <Route element={<Navigate replace to="/users" />} path="/" />
                <Route element={<UsersPage />} path="/users" />
                <Route element={<TokensPage />} path="/tokens" />
                <Route element={<AuditPage />} path="/audit" />
              </Route>
            </Route>
          </Routes>
        </Router>
        <Toaster position="top-right" />
      </AuthProvider>
    </QueryClientProvider>
  )
}

export default App

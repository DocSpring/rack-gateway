import { Loader2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { GoogleIcon } from '@/components/google-icon'
import { ModeToggle } from '@/components/mode-toggle'
import { Button } from '@/components/ui/button'
import { Card, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { toast } from '@/components/ui/use-toast'
import { useAuth } from '@/contexts/auth-context'

export function LoginPage() {
  const { login } = useAuth()
  const [isLoading, setIsLoading] = useState(false)

  // Show any persisted auth error (from 401 redirects)
  useEffect(() => {
    try {
      const msg = sessionStorage.getItem('auth_error')
      if (msg) {
        toast.error(msg)
        sessionStorage.removeItem('auth_error')
      }
    } catch (_e) {
      /* ignore */
    }
  }, [])

  const handleLogin = async () => {
    setIsLoading(true)
    try {
      await login()
    } catch (_error) {
      setIsLoading(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background">
      <div className="absolute top-4 right-4">
        <ModeToggle />
      </div>
      <div className="w-full max-w-md">
        <div className="mb-8 text-center">
          <h1 className="mb-2 font-bold text-4xl text-foreground tracking-tight">Rack Gateway</h1>
          <p className="text-muted-foreground">Secure access management for your Convox rack</p>
        </div>

        <Card>
          <CardHeader className="space-y-1">
            <CardTitle className="text-center text-2xl">Sign in</CardTitle>
            <CardDescription className="text-center">
              Use your company Google account to continue
            </CardDescription>
          </CardHeader>
          <CardFooter className="my-2">
            <Button
              className="w-full text-white"
              data-testid="login-cta"
              disabled={isLoading}
              onClick={handleLogin}
              size="lg"
            >
              {isLoading ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  Signing in...
                </>
              ) : (
                <>
                  <GoogleIcon className="mr-2 h-5 w-5" />
                  {import.meta.env.MODE === 'development'
                    ? 'Continue with Mock OAuth'
                    : 'Continue with Google'}
                </>
              )}
            </Button>
          </CardFooter>
        </Card>
      </div>
    </div>
  )
}

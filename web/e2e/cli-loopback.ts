import { createHash, randomBytes } from 'node:crypto'
import { createServer } from 'node:http'
import type { AddressInfo } from 'node:net'
import type { APIRequestContext } from '@playwright/test'
import { APIRoute } from '@/lib/routes'

export type CliLoopbackLogin = {
  /** Identity provider URL the CLI would open in the browser. */
  authUrl: string
  /** Resolves with the single-use login code the gateway delivers to the loopback listener. */
  waitForLoginCode: () => Promise<string>
  /** Redeems the login code with the PKCE verifier, as the CLI does. */
  complete: (request: APIRequestContext) => Promise<{ status: number; token?: string }>
  close: () => Promise<void>
}

const base64url = (buf: Buffer): string => buf.toString('base64url')

/**
 * Plays the rack-gateway CLI in an RFC 8252 loopback login: listens on 127.0.0.1, starts the login
 * with a PKCE challenge, and captures the login code the browser is redirected back with.
 */
export async function startCliLoopbackLogin(request: APIRequestContext): Promise<CliLoopbackLogin> {
  const verifier = base64url(randomBytes(64))
  const challenge = base64url(createHash('sha256').update(verifier).digest())
  const state = base64url(randomBytes(32))

  let deliverCode: ((code: string) => void) | null = null
  const codePromise = new Promise<string>((resolve) => {
    deliverCode = resolve
  })

  const server = createServer((req, res) => {
    const url = new URL(req.url ?? '/', 'http://127.0.0.1')
    const code = url.searchParams.get('code')
    if (url.pathname === '/callback' && url.searchParams.get('state') === state && code) {
      deliverCode?.(code)
      res.writeHead(200, { 'Content-Type': 'text/html' })
      res.end('<h1>Login approved</h1>')
      return
    }
    res.writeHead(400)
    res.end('unexpected login redirect')
  })
  await new Promise<void>((resolve) => {
    server.listen(0, '127.0.0.1', resolve)
  })
  const { port } = server.address() as AddressInfo

  const response = await request.post(APIRoute('auth/cli/start'), {
    data: {
      code_challenge: challenge,
      code_challenge_method: 'S256',
      redirect_uri: `http://127.0.0.1:${port}/callback`,
      state,
      device_name: 'e2e-cli',
    },
  })
  if (!response.ok()) {
    server.close()
    throw new Error(`auth/cli/start failed: ${response.status()} ${await response.text()}`)
  }
  const { auth_url: authUrl } = (await response.json()) as { auth_url: string }

  return {
    authUrl,
    waitForLoginCode: () => codePromise,
    complete: async (api) => {
      const loginCode = await codePromise
      const completion = await api.post(APIRoute('auth/cli/complete'), {
        data: { login_code: loginCode, code_verifier: verifier, device_name: 'e2e-cli' },
      })
      const body = (await completion.json().catch(() => ({}))) as { token?: string }
      return { status: completion.status(), token: body.token }
    },
    close: () =>
      new Promise<void>((resolve) => {
        server.close(() => resolve())
      }),
  }
}

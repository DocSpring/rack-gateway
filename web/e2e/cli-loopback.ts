import { createHash, randomBytes } from 'node:crypto'
import { createServer } from 'node:http'
import type { AddressInfo } from 'node:net'
import type { APIRequestContext } from '@playwright/test'
import { APIRoute, WebRoute } from '@/lib/routes'

type CliCompletion = { status: number; token?: string }

export type CliLoopbackLogin = {
  /** Identity provider URL the CLI would open in the browser. */
  authUrl: string
  /** Resolves once the CLI has redeemed the login code the browser delivered to its loopback listener. */
  completion: Promise<CliCompletion>
  /** Redeems the login code again with the PKCE verifier (login codes are single use). */
  redeemAgain: (request: APIRequestContext) => Promise<CliCompletion>
  close: () => Promise<void>
}

const base64url = (buf: Buffer): string => buf.toString('base64url')

/**
 * Plays the rack-gateway CLI in an RFC 8252 loopback login: listens on 127.0.0.1, starts the login
 * with a PKCE challenge, and when the browser brings back the login code, redeems it and sends the
 * browser on to the gateway's CLI login result page, as the CLI does.
 */
export async function startCliLoopbackLogin(request: APIRequestContext): Promise<CliLoopbackLogin> {
  const verifier = base64url(randomBytes(64))
  const challenge = base64url(createHash('sha256').update(verifier).digest())
  const state = base64url(randomBytes(32))
  let gatewayOrigin = ''
  let loginCode: string | null = null

  const redeem = async (api: APIRequestContext, code: string): Promise<CliCompletion> => {
    const response = await api.post(APIRoute('auth/cli/complete'), {
      data: { login_code: code, code_verifier: verifier, device_name: 'e2e-cli' },
    })
    const body = (await response.json().catch(() => ({}))) as { token?: string }
    return { status: response.status(), token: body.token }
  }

  let resolveCompletion: ((result: CliCompletion) => void) | null = null
  const completion = new Promise<CliCompletion>((resolve) => {
    resolveCompletion = resolve
  })

  const server = createServer(async (req, res) => {
    const url = new URL(req.url ?? '/', 'http://127.0.0.1')
    const code = url.searchParams.get('code')
    if (url.pathname !== '/callback' || url.searchParams.get('state') !== state || !code) {
      res.writeHead(400)
      res.end('unexpected login redirect')
      return
    }
    loginCode = code
    const result = await redeem(request, code).catch(() => ({ status: 0 }))
    resolveCompletion?.(result)
    const page =
      result.status === 200
        ? WebRoute('cli/auth/success')
        : WebRoute('cli/auth/error?error=cli_incomplete')
    res.writeHead(303, {
      Location: `${gatewayOrigin}${page}`,
      'Referrer-Policy': 'no-referrer',
      'Cache-Control': 'no-store',
    })
    res.end()
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
  gatewayOrigin = new URL(response.url()).origin
  const { auth_url: authUrl } = (await response.json()) as { auth_url: string }

  return {
    authUrl,
    completion,
    redeemAgain: async (api) => {
      await completion
      return redeem(api, loginCode ?? '')
    },
    close: () =>
      new Promise<void>((resolve) => {
        server.closeAllConnections()
        server.close(() => resolve())
      }),
  }
}

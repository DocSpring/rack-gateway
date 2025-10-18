/**
 * WebAuthn encoding/decoding utilities for browser API compatibility
 */

/**
 * Convert base64url string to ArrayBuffer for WebAuthn API
 */
function base64urlToArrayBuffer(base64url: string): ArrayBuffer {
  const base64 = base64url.replace(/-/g, '+').replace(/_/g, '/')
  const binaryString = atob(base64)
  const bytes = new Uint8Array(binaryString.length)
  for (let i = 0; i < binaryString.length; i++) {
    bytes[i] = binaryString.charCodeAt(i)
  }
  return bytes.buffer
}

/**
 * Convert ArrayBuffer to base64url string for JSON serialization
 */
function arrayBufferToBase64url(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer)
  let binary = ''
  for (const byte of bytes) {
    binary += String.fromCharCode(byte)
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '')
}

/**
 * Convert PublicKeyCredentialCreationOptions from server format to browser format
 * Converts base64url strings to ArrayBuffers where required by the WebAuthn API
 */
export function prepareCreationOptions(options: unknown): PublicKeyCredentialCreationOptions {
  const opts = options as { publicKey?: unknown }
  const publicKeyOptions = (opts.publicKey || options) as {
    challenge: string
    user: { id: string; [key: string]: unknown }
    [key: string]: unknown
  }

  return {
    ...publicKeyOptions,
    challenge: base64urlToArrayBuffer(publicKeyOptions.challenge),
    user: {
      ...publicKeyOptions.user,
      id: base64urlToArrayBuffer(publicKeyOptions.user.id),
    },
  } as PublicKeyCredentialCreationOptions
}

/**
 * Convert PublicKeyCredentialRequestOptions from server format to browser format
 * Converts base64url strings to ArrayBuffers where required by the WebAuthn API
 */
export function prepareRequestOptions(options: unknown): PublicKeyCredentialRequestOptions {
  const opts = options as { publicKey?: unknown }
  const publicKeyOptions = (opts.publicKey || options) as {
    challenge: string
    allowCredentials?: Array<{ id: string; [key: string]: unknown }>
    [key: string]: unknown
  }

  return {
    ...publicKeyOptions,
    challenge: base64urlToArrayBuffer(publicKeyOptions.challenge),
    allowCredentials: publicKeyOptions.allowCredentials?.map(
      (cred: { id: string; [key: string]: unknown }) => ({
        ...cred,
        id: base64urlToArrayBuffer(cred.id),
      })
    ),
  } as PublicKeyCredentialRequestOptions
}

type SerializedCredential = {
  id: string
  rawId: string
  type: string
  response: {
    clientDataJSON: string
    attestationObject?: string
    authenticatorData?: string
    signature?: string
    userHandle?: string
  }
}

/**
 * Serialize PublicKeyCredential (registration) to JSON for backend
 */
export function serializeRegistrationCredential(
  credential: PublicKeyCredential
): SerializedCredential {
  const response = credential.response as AuthenticatorAttestationResponse

  return {
    id: credential.id,
    rawId: arrayBufferToBase64url(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: arrayBufferToBase64url(response.clientDataJSON),
      attestationObject: arrayBufferToBase64url(response.attestationObject),
    },
  }
}

/**
 * Serialize PublicKeyCredential (assertion) to JSON for backend
 */
export function serializeAssertionCredential(
  credential: PublicKeyCredential
): SerializedCredential {
  const response = credential.response as AuthenticatorAssertionResponse

  return {
    id: credential.id,
    rawId: arrayBufferToBase64url(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: arrayBufferToBase64url(response.clientDataJSON),
      authenticatorData: arrayBufferToBase64url(response.authenticatorData),
      signature: arrayBufferToBase64url(response.signature),
      userHandle: response.userHandle ? arrayBufferToBase64url(response.userHandle) : undefined,
    },
  }
}

/**
 * Creates a base mock credential object for E2E testing
 */
function createBaseMockCredential() {
  return {
    id: 'mock-credential-id',
    type: 'public-key',
    rawId: new Uint8Array([1, 2, 3, 4]).buffer,
    response: {
      clientDataJSON: new Uint8Array([5, 6, 7, 8]).buffer,
    },
  }
}

/**
 * Call navigator.credentials.get with E2E test mode support
 * In E2E mode, returns a mock credential to prevent triggering real hardware
 */
export function getCredential(options: CredentialRequestOptions): Promise<Credential | null> {
  // biome-ignore lint/suspicious/noExplicitAny: window.__e2e_test_mode__ is set by test environment
  if ((window as any).__e2e_test_mode__) {
    // Return a mock PublicKeyCredential that will pass serialization
    const mockCredential = {
      ...createBaseMockCredential(),
      response: {
        ...createBaseMockCredential().response,
        authenticatorData: new Uint8Array([9, 10, 11, 12]).buffer,
        signature: new Uint8Array([13, 14, 15, 16]).buffer,
        userHandle: null,
      },
    } as unknown as Credential
    return Promise.resolve(mockCredential)
  }
  return navigator.credentials.get(options)
}

/**
 * Call navigator.credentials.create with E2E test mode support
 * In E2E mode, returns a mock credential to prevent triggering real hardware
 */
export function createCredential(options: CredentialCreationOptions): Promise<Credential | null> {
  // biome-ignore lint/suspicious/noExplicitAny: window.__e2e_test_mode__ is set by test environment
  if ((window as any).__e2e_test_mode__) {
    // Return a mock PublicKeyCredential that will pass serialization
    const mockCredential = {
      ...createBaseMockCredential(),
      response: {
        ...createBaseMockCredential().response,
        attestationObject: new Uint8Array([9, 10, 11, 12]).buffer,
      },
    } as unknown as Credential
    return Promise.resolve(mockCredential)
  }
  return navigator.credentials.create(options)
}

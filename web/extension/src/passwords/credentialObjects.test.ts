import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  base64UrlToBuffer,
  bufferToBase64Url,
  buildAssertionCredential,
  buildAttestationCredential,
  copyCreationOptions,
  copyRequestOptions,
} from './credentialObjects'
import { installPasskeyOverride, PASSKEY_CHANNEL, PasskeyReply } from './passkeyOverride'

describe('base64url helpers', () => {
  it('round trips bytes, with no padding and the url alphabet', () => {
    const bytes = new Uint8Array([251, 255, 254, 0, 1])
    const text = bufferToBase64Url(bytes)
    expect(text).toBe('-__-AAE')
    expect(new Uint8Array(base64UrlToBuffer(text))).toEqual(bytes)
  })

  it('reads a view of part of a buffer', () => {
    const whole = new Uint8Array([1, 2, 3, 4, 5])
    expect(bufferToBase64Url(whole.subarray(1, 3))).toBe(bufferToBase64Url(new Uint8Array([2, 3])))
  })
})

describe('copying the page options', () => {
  it('copies binary values as base64url and nothing else the extension does not read', () => {
    const copied = copyCreationOptions({
      rp: { id: 'example.test', name: 'Example', icon: 'https://x.test/i.png' },
      user: { id: new Uint8Array([1, 2]), name: 'ada', displayName: 'Ada' },
      challenge: new Uint8Array([3, 4]).buffer,
      pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
      excludeCredentials: [{ type: 'public-key', id: new Uint8Array([5]) }],
      authenticatorSelection: { authenticatorAttachment: 'platform', residentKey: 'required' },
      extensions: { credProps: true },
    })
    expect(copied).toEqual({
      rp: { id: 'example.test', name: 'Example' },
      user: { id: 'AQI', name: 'ada', displayName: 'Ada' },
      challenge: 'AwQ',
      pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
      excludeCredentials: [{ type: 'public-key', id: 'BQ' }],
      authenticatorSelection: {
        authenticatorAttachment: 'platform',
        residentKey: 'required',
        userVerification: undefined,
      },
      attestation: undefined,
    })
  })

  it('copies a value that is not binary as null, to be refused over there', () => {
    const copied = copyRequestOptions({
      rpId: 'example.test',
      challenge: 'a string',
      allowCredentials: [{ type: 'public-key', id: 7 }],
    })
    expect(copied.challenge).toBeNull()
    expect(copied.allowCredentials).toEqual([{ type: 'public-key', id: null }])
  })
})

const attestation = {
  id: 'AQID',
  rawId: 'AQID',
  clientDataJSON: 'e30',
  attestationObject: 'oA',
  authenticatorData: 'AAEC',
  publicKey: 'BAUG',
  publicKeyAlgorithm: -7,
  transports: ['internal', 'hybrid'],
}

describe('the credential given to the page', () => {
  beforeEach(() => {
    class FakePublicKeyCredential {}
    class FakeAttestationResponse {}
    class FakeAssertionResponse {}
    vi.stubGlobal('PublicKeyCredential', FakePublicKeyCredential)
    vi.stubGlobal('AuthenticatorAttestationResponse', FakeAttestationResponse)
    vi.stubGlobal('AuthenticatorAssertionResponse', FakeAssertionResponse)
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('looks like a PublicKeyCredential from a registration', () => {
    const credential = buildAttestationCredential(attestation) as any
    expect(credential).toBeInstanceOf((globalThis as any).PublicKeyCredential)
    expect(credential.type).toBe('public-key')
    expect(credential.id).toBe('AQID')
    expect(credential.authenticatorAttachment).toBe('platform')
    expect(new Uint8Array(credential.rawId)).toEqual(new Uint8Array([1, 2, 3]))
    expect(credential.rawId).toBeInstanceOf(ArrayBuffer)
    expect(credential.response).toBeInstanceOf((globalThis as any).AuthenticatorAttestationResponse)
    expect(new TextDecoder().decode(credential.response.clientDataJSON)).toBe('{}')
    expect(credential.response.getTransports()).toEqual(['internal', 'hybrid'])
    expect(credential.response.getPublicKeyAlgorithm()).toBe(-7)
    expect(new Uint8Array(credential.response.getPublicKey())).toEqual(new Uint8Array([4, 5, 6]))
    expect(new Uint8Array(credential.response.getAuthenticatorData())).toEqual(new Uint8Array([0, 1, 2]))
    expect(credential.getClientExtensionResults()).toEqual({})
  })

  it('gives a toJSON a site can send as it is', () => {
    const credential = buildAttestationCredential(attestation) as any
    expect(credential.toJSON()).toEqual({
      id: 'AQID',
      rawId: 'AQID',
      type: 'public-key',
      authenticatorAttachment: 'platform',
      response: {
        clientDataJSON: 'e30',
        attestationObject: 'oA',
        authenticatorData: 'AAEC',
        publicKey: 'BAUG',
        publicKeyAlgorithm: -7,
        transports: ['internal', 'hybrid'],
      },
      clientExtensionResults: {},
    })
  })

  it('looks like a PublicKeyCredential from a sign-in, with and without a user handle', () => {
    const json = {
      id: 'AQID',
      rawId: 'AQID',
      clientDataJSON: 'e30',
      authenticatorData: 'AAEC',
      signature: 'BAUG',
      userHandle: 'Bw',
    }
    const credential = buildAssertionCredential(json) as any
    expect(credential.response).toBeInstanceOf((globalThis as any).AuthenticatorAssertionResponse)
    expect(new Uint8Array(credential.response.signature)).toEqual(new Uint8Array([4, 5, 6]))
    expect(new Uint8Array(credential.response.userHandle)).toEqual(new Uint8Array([7]))
    expect(credential.toJSON().response.userHandle).toBe('Bw')
    const without = buildAssertionCredential({ ...json, userHandle: null }) as any
    expect(without.response.userHandle).toBeNull()
    expect('userHandle' in without.toJSON().response).toBe(false)
  })

  it('works where the browser has no such classes', () => {
    vi.unstubAllGlobals()
    const credential = buildAttestationCredential(attestation) as any
    expect(credential.id).toBe('AQID')
  })
})

// A stand-in for the content script: it answers the pings and requests the
// override posts to the window, the way content.ts does.
const contentScripts: ((event: MessageEvent) => void)[] = []

function stubContentScript(reply: PasskeyReply | null, seen: Record<string, unknown>[] = []) {
  const listener = (event: MessageEvent) => {
    const data = event.data
    if (!data || data.channel !== PASSKEY_CHANNEL) return
    if (data.direction === 'ping') {
      window.dispatchEvent(
        new MessageEvent('message', {
          source: window,
          data: { channel: PASSKEY_CHANNEL, direction: 'pong', id: data.id },
        }),
      )
    } else if (data.direction === 'request') {
      seen.push(data)
      if (reply) {
        window.dispatchEvent(
          new MessageEvent('message', {
            source: window,
            data: { channel: PASSKEY_CHANNEL, direction: 'reply', id: data.id, reply },
          }),
        )
      }
    }
  }
  contentScripts.push(listener)
  window.addEventListener('message', listener)
}

describe('installPasskeyOverride', () => {
  let originalCreate: ReturnType<typeof vi.fn>
  let originalGet: ReturnType<typeof vi.fn>
  let credentials: { create: any; get: any }

  beforeEach(() => {
    originalCreate = vi.fn(async () => 'browser-created')
    originalGet = vi.fn(async () => 'browser-got')
    credentials = { create: originalCreate, get: originalGet }
    Object.defineProperty(window.navigator, 'credentials', { value: credentials, configurable: true })
  })
  afterEach(() => {
    for (const listener of contentScripts.splice(0)) window.removeEventListener('message', listener)
    delete (window.navigator as any).credentials
    vi.unstubAllGlobals()
  })

  const publicKey = { challenge: new Uint8Array([1]), rp: { name: 'x' }, user: { id: new Uint8Array([1]), name: 'a' } }

  it('leaves everything but publicKey requests to the browser', async () => {
    installPasskeyOverride(window)
    expect(await credentials.create({ password: { id: 'x' } })).toBe('browser-created')
    expect(await credentials.get({ password: true })).toBe('browser-got')
    expect(await credentials.get()).toBe('browser-got')
    expect(originalCreate).toHaveBeenCalledTimes(1)
  })

  it('passes conditional mediation through to the browser', async () => {
    installPasskeyOverride(window)
    stubContentScript({ replyOutcome: 'error', errorName: 'NotAllowedError', errorMessage: 'no' })
    expect(await credentials.get({ publicKey: { challenge: new Uint8Array([1]) }, mediation: 'conditional' })).toBe(
      'browser-got',
    )
  })

  it('passes silent mediation, and a conditional create, through to the browser', async () => {
    installPasskeyOverride(window)
    stubContentScript({ replyOutcome: 'error', errorName: 'NotAllowedError', errorMessage: 'no' })
    expect(await credentials.get({ publicKey: { challenge: new Uint8Array([1]) }, mediation: 'silent' })).toBe(
      'browser-got',
    )
    expect(await credentials.create({ publicKey, mediation: 'conditional' })).toBe('browser-created')
    expect(await credentials.create({ publicKey, mediation: 'silent' })).toBe('browser-created')
  })

  it('falls back to the browser when nobody is listening', async () => {
    installPasskeyOverride(window)
    expect(await credentials.create({ publicKey })).toBe('browser-created')
  })

  it('falls back when the extension says to use the browser instead', async () => {
    installPasskeyOverride(window)
    stubContentScript({ replyOutcome: 'fallback' })
    expect(await credentials.create({ publicKey })).toBe('browser-created')
    expect(await credentials.get({ publicKey: { challenge: new Uint8Array([1]) } })).toBe('browser-got')
  })

  it('gives the page the credential the extension made, and sends only plain data', async () => {
    const seen: Record<string, unknown>[] = []
    installPasskeyOverride(window)
    stubContentScript({ replyOutcome: 'credential', credential: attestation }, seen)
    const credential = (await credentials.create({ publicKey })) as any
    expect(credential.id).toBe('AQID')
    expect(originalCreate).not.toHaveBeenCalled()
    expect(seen[0]).toMatchObject({
      ceremonyKind: 'create',
      options: { challenge: 'AQ', user: { id: 'AQ', name: 'a' } },
    })
  })

  it('throws what the extension says to throw', async () => {
    installPasskeyOverride(window)
    stubContentScript({ replyOutcome: 'error', errorName: 'SecurityError', errorMessage: 'not this site' })
    await expect(credentials.create({ publicKey })).rejects.toMatchObject({
      name: 'SecurityError',
      message: 'not this site',
    })
  })

  it('throws a TypeError for a malformed request', async () => {
    installPasskeyOverride(window)
    stubContentScript({ replyOutcome: 'error', errorName: 'TypeError', errorMessage: 'bad' })
    await expect(credentials.get({ publicKey: { challenge: new Uint8Array([1]) } })).rejects.toBeInstanceOf(TypeError)
  })

  it('does not install itself where there are no credentials', () => {
    delete (window.navigator as any).credentials
    expect(() => installPasskeyOverride(window)).not.toThrow()
  })

  it('stops at once for a request that was already cancelled', async () => {
    installPasskeyOverride(window)
    stubContentScript({ replyOutcome: 'fallback' })
    const controller = new AbortController()
    controller.abort()
    await expect(credentials.create({ publicKey, signal: controller.signal })).rejects.toMatchObject({
      name: 'AbortError',
    })
  })
})

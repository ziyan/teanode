import { describe, expect, it } from 'vitest'
import { decodeBase64Url, encodeBase64Url } from '../../../src/passwords/crypto'
import { readPasskeyPublicKey, verifyAssertion } from '../../../src/passwords/passkey'
import {
  buildClientDataJson,
  encodeSpkiPublicKey,
  makePasskey,
  PageContext,
  parseAssertionRequest,
  parseCreationRequest,
  signWithPasskey,
} from './webauthn'

const context: PageContext = {
  pageUrl: 'https://login.example.test/start',
  origin: 'https://login.example.test',
  isCrossOrigin: false,
}
const challenge = encodeBase64Url(new Uint8Array([1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16]))
const userHandle = encodeBase64Url(new Uint8Array([9, 9, 9, 9]))

function creationOptions(overrides: Record<string, unknown> = {}) {
  return {
    rp: { id: 'example.test', name: 'Example' },
    user: { id: userHandle, name: 'ada', displayName: 'Ada' },
    challenge,
    pubKeyCredParams: [
      { type: 'public-key', alg: -7 },
      { type: 'public-key', alg: -257 },
    ],
    excludeCredentials: [],
    authenticatorSelection: { residentKey: 'preferred' },
    ...overrides,
  }
}

describe('parseCreationRequest', () => {
  it('reads a request for the page it comes from', () => {
    const parsed = parseCreationRequest(creationOptions(), context)
    expect(parsed).toEqual({
      parseOutcome: 'ok',
      request: {
        relyingPartyId: 'example.test',
        relyingPartyName: 'Example',
        userHandle,
        userName: 'ada',
        userDisplayName: 'Ada',
        challenge,
        excludedCredentialIds: [],
        userVerification: 'preferred',
      },
    })
  })

  it('reads how much the site insists on the person being verified', () => {
    for (const [asked, wanted] of [
      ['required', 'required'],
      ['discouraged', 'discouraged'],
      ['preferred', 'preferred'],
      [undefined, 'preferred'],
      ['something else', 'preferred'],
    ]) {
      const parsed = parseCreationRequest(
        creationOptions({ authenticatorSelection: { userVerification: asked } }),
        context,
      )
      expect(parsed, String(asked)).toMatchObject({ request: { userVerification: wanted } })
    }
    expect(
      parseAssertionRequest({ rpId: 'example.test', challenge, userVerification: 'required' }, context),
    ).toMatchObject({
      request: { userVerification: 'required' },
    })
    expect(parseAssertionRequest({ rpId: 'example.test', challenge }, context)).toMatchObject({
      request: { userVerification: 'preferred' },
    })
  })

  it('takes the page host when the relying party names no id', () => {
    const parsed = parseCreationRequest(creationOptions({ rp: { name: 'Example' } }), context)
    expect(parsed).toMatchObject({ parseOutcome: 'ok', request: { relyingPartyId: 'login.example.test' } })
  })

  it('refuses a relying party id that is not the page, as a browser would', () => {
    for (const id of ['evil.test', 'example.test.evil.test', 'test', 'login.example.test.']) {
      const parsed = parseCreationRequest(creationOptions({ rp: { id, name: 'x' } }), context)
      if (id === 'login.example.test.') expect(parsed.parseOutcome).toBe('ok')
      else expect(parsed, id).toMatchObject({ parseOutcome: 'error', errorName: 'SecurityError' })
    }
  })

  it('refuses a page at an address from naming a domain', () => {
    const address: PageContext = { pageUrl: 'https://192.0.2.7/', origin: 'https://192.0.2.7', isCrossOrigin: false }
    expect(parseCreationRequest(creationOptions({ rp: { id: '2.7', name: 'x' } }), address)).toMatchObject({
      parseOutcome: 'error',
      errorName: 'SecurityError',
    })
    // An address is not a relying party at all.
    expect(parseCreationRequest(creationOptions({ rp: { id: '192.0.2.7', name: 'x' } }), address)).toMatchObject({
      parseOutcome: 'error',
      errorName: 'SecurityError',
    })
  })

  it('leaves a page that is not secure to the browser', () => {
    const plain: PageContext = { pageUrl: 'http://example.test/', origin: 'http://example.test', isCrossOrigin: false }
    expect(parseCreationRequest(creationOptions(), plain)).toMatchObject({ parseOutcome: 'fallback' })
  })

  it('leaves to the browser what this authenticator cannot make', () => {
    expect(
      parseCreationRequest(creationOptions({ pubKeyCredParams: [{ type: 'public-key', alg: -257 }] }), context)
        .parseOutcome,
    ).toBe('fallback')
    expect(
      parseCreationRequest(
        creationOptions({ authenticatorSelection: { authenticatorAttachment: 'cross-platform' } }),
        context,
      ).parseOutcome,
    ).toBe('fallback')
  })

  it('reads an empty list of parameters as the default, which includes ES256', () => {
    expect(parseCreationRequest(creationOptions({ pubKeyCredParams: [] }), context).parseOutcome).toBe('ok')
  })

  it('refuses a request with a missing or malformed part', () => {
    expect(parseCreationRequest(null, context)).toMatchObject({ parseOutcome: 'error', errorName: 'TypeError' })
    expect(parseCreationRequest(creationOptions({ challenge: null }), context)).toMatchObject({
      parseOutcome: 'error',
      errorName: 'TypeError',
    })
    expect(parseCreationRequest(creationOptions({ challenge: 'not base64 url!' }), context)).toMatchObject({
      parseOutcome: 'error',
    })
    expect(parseCreationRequest(creationOptions({ user: { id: userHandle, name: '' } }), context)).toMatchObject({
      parseOutcome: 'error',
    })
    expect(
      parseCreationRequest(
        creationOptions({ user: { id: encodeBase64Url(new Uint8Array(65)), name: 'ada' } }),
        context,
      ),
    ).toMatchObject({ parseOutcome: 'error' })
    expect(parseCreationRequest(creationOptions({ excludeCredentials: [{ id: null }] }), context)).toMatchObject({
      parseOutcome: 'error',
    })
  })

  it('collects the credentials the site wants left out', () => {
    const excluded = encodeBase64Url(new Uint8Array([7, 7, 7]))
    const parsed = parseCreationRequest(
      creationOptions({ excludeCredentials: [{ type: 'public-key', id: excluded }] }),
      context,
    )
    expect(parsed).toMatchObject({ parseOutcome: 'ok', request: { excludedCredentialIds: [excluded] } })
  })
})

describe('parseAssertionRequest', () => {
  it('reads a sign-in request', () => {
    const allowed = encodeBase64Url(new Uint8Array([4, 5, 6]))
    const parsed = parseAssertionRequest(
      { rpId: 'example.test', challenge, allowCredentials: [{ type: 'public-key', id: allowed }] },
      context,
    )
    expect(parsed).toEqual({
      parseOutcome: 'ok',
      request: {
        relyingPartyId: 'example.test',
        challenge,
        allowedCredentialIds: [allowed],
        userVerification: 'preferred',
      },
    })
  })

  it('takes the page host when no id is named, and refuses another site', () => {
    expect(parseAssertionRequest({ challenge }, context)).toMatchObject({
      parseOutcome: 'ok',
      request: { relyingPartyId: 'login.example.test' },
    })
    expect(parseAssertionRequest({ rpId: 'evil.test', challenge }, context)).toMatchObject({
      parseOutcome: 'error',
      errorName: 'SecurityError',
    })
  })

  it('refuses a request without a usable challenge', () => {
    expect(parseAssertionRequest({ rpId: 'example.test' }, context)).toMatchObject({
      parseOutcome: 'error',
      errorName: 'TypeError',
    })
    expect(parseAssertionRequest('nonsense', context)).toMatchObject({ parseOutcome: 'error' })
  })
})

describe('buildClientDataJson', () => {
  it('writes the four members a site expects, with the origin the browser reported', () => {
    expect(
      buildClientDataJson({
        type: 'webauthn.create',
        challenge: 'AQID',
        origin: 'https://example.test',
        isCrossOrigin: false,
      }),
    ).toBe('{"type":"webauthn.create","challenge":"AQID","origin":"https://example.test","crossOrigin":false}')
    expect(
      JSON.parse(
        buildClientDataJson({ type: 'webauthn.get', challenge: 'AQID', origin: 'https://a.test', isCrossOrigin: true }),
      ),
    ).toEqual({ type: 'webauthn.get', challenge: 'AQID', origin: 'https://a.test', crossOrigin: true })
  })
})

describe('encodeSpkiPublicKey', () => {
  it('makes a key WebCrypto reads back as the same point', async () => {
    const pair = await crypto.subtle.generateKey({ name: 'ECDSA', namedCurve: 'P-256' }, true, ['sign', 'verify'])
    const raw = new Uint8Array(await crypto.subtle.exportKey('raw', pair.publicKey))
    const spki = encodeSpkiPublicKey(raw.slice(1, 33), raw.slice(33, 65))
    expect(spki).toEqual(new Uint8Array(await crypto.subtle.exportKey('spki', pair.publicKey)))
    await crypto.subtle.importKey('spki', new Uint8Array(spki), { name: 'ECDSA', namedCurve: 'P-256' }, true, [
      'verify',
    ])
  })
})

describe('makePasskey and signWithPasskey', () => {
  it('makes the registration a site verifies and keeps what a login stores', async () => {
    const parsed = parseCreationRequest(creationOptions(), context)
    if (parsed.parseOutcome !== 'ok') throw new Error('request refused')
    const made = await makePasskey(parsed.request, context, true, new Date('2026-10-20T10:00:00Z'))

    const credentialId = decodeBase64Url(made.credential.rawId)
    expect(credentialId.length).toBe(16)
    expect(made.credential.id).toBe(made.credential.rawId)
    expect(JSON.parse(new TextDecoder().decode(decodeBase64Url(made.credential.clientDataJSON)))).toEqual({
      type: 'webauthn.create',
      challenge,
      origin: 'https://login.example.test',
      crossOrigin: false,
    })
    const authenticatorData = decodeBase64Url(made.credential.authenticatorData)
    const rpIdHash = new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode('example.test')))
    expect(authenticatorData.slice(0, 32)).toEqual(rpIdHash)
    // UP, UV, BE, BS and attested credential data.
    expect(authenticatorData[32]).toBe(0x5d)
    expect(new TextDecoder().decode(authenticatorData.slice(37, 53))).toBe('TEANODEPASSWORDS')
    expect(made.credential.publicKeyAlgorithm).toBe(-7)
    expect(made.credential.transports).toContain('internal')
    // The attestation object has format "none" and carries the same authenticator data.
    const attestation = decodeBase64Url(made.credential.attestationObject)
    expect(new TextDecoder('latin1').decode(attestation)).toContain('none')
    expect(attestation.length).toBeGreaterThan(authenticatorData.length)

    expect(made.passkeyFields).toMatchObject({
      relyingPartyId: 'example.test',
      userName: 'ada',
      userDisplayName: 'Ada',
      createdAt: '2026-10-20T10:00:00.000Z',
    })
    // Stored as standard base64 with padding, not base64url.
    expect(made.passkeyFields.credentialId).toMatch(/^[A-Za-z0-9+/]+={0,2}$/)
    expect(atob(made.passkeyFields.userHandle).length).toBe(4)
  })

  it('signs a sign-in the public key verifies, over the client data hash', async () => {
    const parsed = parseCreationRequest(creationOptions(), context)
    if (parsed.parseOutcome !== 'ok') throw new Error('request refused')
    const made = await makePasskey(parsed.request, context, true)
    const assertionRequest = parseAssertionRequest({ rpId: 'example.test', challenge }, context)
    if (assertionRequest.parseOutcome !== 'ok') throw new Error('request refused')

    const answer = await signWithPasskey(assertionRequest.request, made.passkeyFields, context, true)
    expect(answer.rawId).toBe(made.credential.rawId)
    expect(answer.userHandle).toBe(userHandle)

    const authenticatorData = decodeBase64Url(answer.authenticatorData)
    expect(authenticatorData.length).toBe(37)
    expect(authenticatorData[32]).toBe(0x1d)
    const clientDataBytes = decodeBase64Url(answer.clientDataJSON)
    expect(JSON.parse(new TextDecoder().decode(clientDataBytes)).type).toBe('webauthn.get')
    const clientDataHash = new Uint8Array(await crypto.subtle.digest('SHA-256', new Uint8Array(clientDataBytes)))
    const privateKey = Uint8Array.from(atob(made.passkeyFields.privateKey), (character) => character.charCodeAt(0))
    const publicKey = await readPasskeyPublicKey(privateKey)
    expect(
      await verifyAssertion(
        publicKey.publicKeyX,
        publicKey.publicKeyY,
        authenticatorData,
        clientDataHash,
        decodeBase64Url(answer.signature),
      ),
    ).toBe(true)
    // A different challenge does not verify.
    const otherHash = new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode('other')))
    expect(
      await verifyAssertion(
        publicKey.publicKeyX,
        publicKey.publicKeyY,
        authenticatorData,
        otherHash,
        decodeBase64Url(answer.signature),
      ),
    ).toBe(false)
  })

  it('says the person was only present, not verified, unless they were', async () => {
    const parsed = parseCreationRequest(creationOptions(), context)
    if (parsed.parseOutcome !== 'ok') throw new Error('request refused')
    const present = await makePasskey(parsed.request, context, false)
    // UP, BE, BS and attested credential data, and not UV.
    expect(decodeBase64Url(present.credential.authenticatorData)[32]).toBe(0x59)
    const assertionRequest = parseAssertionRequest({ rpId: 'example.test', challenge }, context)
    if (assertionRequest.parseOutcome !== 'ok') throw new Error('request refused')
    const answer = await signWithPasskey(assertionRequest.request, present.passkeyFields, context, false)
    expect(decodeBase64Url(answer.authenticatorData)[32]).toBe(0x19)
  })

  it('answers with a null user handle when the login has none', async () => {
    const parsed = parseCreationRequest(creationOptions(), context)
    if (parsed.parseOutcome !== 'ok') throw new Error('request refused')
    const made = await makePasskey(parsed.request, context, true)
    const assertionRequest = parseAssertionRequest({ rpId: 'example.test', challenge }, context)
    if (assertionRequest.parseOutcome !== 'ok') throw new Error('request refused')
    const answer = await signWithPasskey(
      assertionRequest.request,
      { ...made.passkeyFields, userHandle: '' },
      context,
      true,
    )
    expect(answer.userHandle).toBeNull()
  })
})

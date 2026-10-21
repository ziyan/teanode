// The page-facing half of the passkey override, kept apart from the script
// that installs it so it can be tested. It runs in the page's own world, so it
// imports nothing from the rest of the extension: the bundle that carries it
// should hold only this.
//
// Two jobs: copy the options a page passed to navigator.credentials into
// plain data (binary values as base64url) for the extension, and make the
// object the page expects out of the answer: something that looks and works
// like a PublicKeyCredential.

export function bufferToBase64Url(buffer: ArrayBuffer | ArrayBufferView): string {
  const bytes = ArrayBuffer.isView(buffer)
    ? new Uint8Array(buffer.buffer, buffer.byteOffset, buffer.byteLength)
    : new Uint8Array(buffer)
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

export function base64UrlToBuffer(text: string): ArrayBuffer {
  const standard = text.replace(/-/g, '+').replace(/_/g, '/')
  const binary = atob(standard + '='.repeat((4 - (standard.length % 4)) % 4))
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index++) bytes[index] = binary.charCodeAt(index)
  return bytes.buffer
}

function isBufferSource(value: unknown): value is ArrayBuffer | ArrayBufferView {
  return value instanceof ArrayBuffer || ArrayBuffer.isView(value)
}

function encodeBinary(value: unknown): string | null {
  return isBufferSource(value) ? bufferToBase64Url(value) : null
}

function copyCredentialList(list: unknown): { type: string; id: string | null }[] | undefined {
  if (!Array.isArray(list)) return undefined
  return list.map((entry) => ({ type: String(entry?.type ?? ''), id: encodeBinary(entry?.id) }))
}

// copyCreationOptions is publicKey from create(): only what the extension
// reads, as plain data. A value of the wrong type is passed on as null and
// refused there.
export function copyCreationOptions(publicKey: any): Record<string, unknown> {
  return {
    rp: { id: publicKey?.rp?.id, name: publicKey?.rp?.name },
    user: {
      id: encodeBinary(publicKey?.user?.id),
      name: publicKey?.user?.name,
      displayName: publicKey?.user?.displayName,
    },
    challenge: encodeBinary(publicKey?.challenge),
    pubKeyCredParams: Array.isArray(publicKey?.pubKeyCredParams)
      ? publicKey.pubKeyCredParams.map((entry: any) => ({ type: entry?.type, alg: entry?.alg }))
      : [],
    excludeCredentials: copyCredentialList(publicKey?.excludeCredentials),
    authenticatorSelection: {
      authenticatorAttachment: publicKey?.authenticatorSelection?.authenticatorAttachment,
      residentKey: publicKey?.authenticatorSelection?.residentKey,
      userVerification: publicKey?.authenticatorSelection?.userVerification,
    },
    attestation: publicKey?.attestation,
  }
}

export function copyRequestOptions(publicKey: any): Record<string, unknown> {
  return {
    rpId: publicKey?.rpId,
    challenge: encodeBinary(publicKey?.challenge),
    allowCredentials: copyCredentialList(publicKey?.allowCredentials),
    userVerification: publicKey?.userVerification,
  }
}

// What the extension answers: the fields below with binary values as base64url.
export interface AttestationJson {
  id: string
  rawId: string
  clientDataJSON: string
  attestationObject: string
  authenticatorData: string
  publicKey: string
  publicKeyAlgorithm: number
  transports: string[]
}

export interface AssertionJson {
  id: string
  rawId: string
  clientDataJSON: string
  authenticatorData: string
  signature: string
  userHandle: string | null
}

const AUTHENTICATOR_ATTACHMENT = 'platform'

function defineValues(target: object, values: Record<string, unknown>): void {
  for (const [name, value] of Object.entries(values)) {
    Object.defineProperty(target, name, { value, enumerable: true, configurable: true })
  }
}

// prototypeOf is a built-in class's prototype when the browser has it, so the
// object passes `instanceof PublicKeyCredential`; the own properties set on
// it shadow the prototype's getters, which only work on a real one.
function prototypeOf(name: string): object {
  const constructor = (globalThis as any)[name]
  return constructor && constructor.prototype ? constructor.prototype : Object.prototype
}

function noExtensionResults() {
  return {}
}

export function buildAttestationCredential(json: AttestationJson): unknown {
  const response = Object.create(prototypeOf('AuthenticatorAttestationResponse'))
  const authenticatorData = base64UrlToBuffer(json.authenticatorData)
  const publicKey = base64UrlToBuffer(json.publicKey)
  defineValues(response, {
    clientDataJSON: base64UrlToBuffer(json.clientDataJSON),
    attestationObject: base64UrlToBuffer(json.attestationObject),
    getTransports: () => [...json.transports],
    getAuthenticatorData: () => authenticatorData.slice(0),
    getPublicKey: () => publicKey.slice(0),
    getPublicKeyAlgorithm: () => json.publicKeyAlgorithm,
  })
  const credential = Object.create(prototypeOf('PublicKeyCredential'))
  defineValues(credential, {
    id: json.id,
    rawId: base64UrlToBuffer(json.rawId),
    type: 'public-key',
    authenticatorAttachment: AUTHENTICATOR_ATTACHMENT,
    response,
    getClientExtensionResults: noExtensionResults,
    toJSON: () => ({
      id: json.id,
      rawId: json.rawId,
      type: 'public-key',
      authenticatorAttachment: AUTHENTICATOR_ATTACHMENT,
      response: {
        clientDataJSON: json.clientDataJSON,
        attestationObject: json.attestationObject,
        authenticatorData: json.authenticatorData,
        publicKey: json.publicKey,
        publicKeyAlgorithm: json.publicKeyAlgorithm,
        transports: json.transports,
      },
      clientExtensionResults: {},
    }),
  })
  return credential
}

export function buildAssertionCredential(json: AssertionJson): unknown {
  const response = Object.create(prototypeOf('AuthenticatorAssertionResponse'))
  defineValues(response, {
    clientDataJSON: base64UrlToBuffer(json.clientDataJSON),
    authenticatorData: base64UrlToBuffer(json.authenticatorData),
    signature: base64UrlToBuffer(json.signature),
    userHandle: json.userHandle === null ? null : base64UrlToBuffer(json.userHandle),
  })
  const credential = Object.create(prototypeOf('PublicKeyCredential'))
  defineValues(credential, {
    id: json.id,
    rawId: base64UrlToBuffer(json.rawId),
    type: 'public-key',
    authenticatorAttachment: AUTHENTICATOR_ATTACHMENT,
    response,
    getClientExtensionResults: noExtensionResults,
    toJSON: () => ({
      id: json.id,
      rawId: json.rawId,
      type: 'public-key',
      authenticatorAttachment: AUTHENTICATOR_ATTACHMENT,
      response: {
        clientDataJSON: json.clientDataJSON,
        authenticatorData: json.authenticatorData,
        signature: json.signature,
        ...(json.userHandle === null ? {} : { userHandle: json.userHandle }),
      },
      clientExtensionResults: {},
    }),
  })
  return credential
}

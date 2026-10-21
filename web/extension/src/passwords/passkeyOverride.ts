// The part of passkey support that runs in the page's own world. There is no
// Chrome API for an extension to be a passkey provider, so, as other password
// managers do, this replaces navigator.credentials.create and .get for
// publicKey requests only; every other request goes to the saved originals
// untouched. A request is copied into plain data and handed to the content
// script (which adds the origin and the frame facts itself, from the
// browser, never from here), which passes it to the extension's service
// worker. The answer is one of: a credential to give the page, "use the
// browser instead" (the saved original runs), or an error to throw.
//
// Nothing here is trusted by the extension: a script on the page can send the
// same messages this one does. What the extension protects is what it signs
// for: the relying party is checked against the page by the service worker,
// and every ceremony is confirmed by the person in a window the page cannot
// reach.

import {
  AssertionJson,
  AttestationJson,
  buildAssertionCredential,
  buildAttestationCredential,
  copyCreationOptions,
  copyRequestOptions,
} from './credentialObjects'

export const PASSKEY_CHANNEL = 'teanode-passkeys'

// How long the content script has to say it is listening before the request
// goes to the browser instead. It is there from document_start; this only
// matters when the extension was reloaded under an open page.
const HANDSHAKE_MILLISECONDS = 400

export type PasskeyReply =
  | { replyOutcome: 'credential'; credential: AttestationJson | AssertionJson }
  | { replyOutcome: 'fallback' }
  | { replyOutcome: 'error'; errorName: string; errorMessage: string }

interface RelayMessage {
  channel: typeof PASSKEY_CHANNEL
  direction: 'request' | 'reply' | 'cancel' | 'ping' | 'pong'
  id: string
  ceremonyKind?: 'create' | 'get'
  options?: Record<string, unknown>
  reply?: PasskeyReply
}

function makeId(win: Window): string {
  const values = new Uint32Array(4)
  win.crypto.getRandomValues(values)
  return Array.from(values, (value) => value.toString(16)).join('')
}

function isRelayMessage(data: unknown, direction: RelayMessage['direction'], id: string): data is RelayMessage {
  return (
    typeof data === 'object' &&
    data !== null &&
    (data as RelayMessage).channel === PASSKEY_CHANNEL &&
    (data as RelayMessage).direction === direction &&
    (data as RelayMessage).id === id
  )
}

function abortReason(signal: AbortSignal): unknown {
  return signal.reason ?? new DOMException('The operation was aborted.', 'AbortError')
}

export function installPasskeyOverride(win: Window): void {
  const credentials = win.navigator.credentials
  if (!credentials || typeof credentials.create !== 'function' || typeof credentials.get !== 'function') return
  // A sandboxed or opaque page has no origin to post to: leave the browser alone.
  const targetOrigin = win.location.origin
  if (targetOrigin === 'null' || targetOrigin === '') return
  const originalCreate = credentials.create.bind(credentials)
  const originalGet = credentials.get.bind(credentials)
  let isRelayKnown = false

  // waitForReply listens for one reply with this id on the window itself.
  const waitForReply = (
    id: string,
    direction: 'reply' | 'pong',
    timeoutMilliseconds?: number,
  ): Promise<RelayMessage | null> =>
    new Promise((resolve) => {
      let timer: ReturnType<typeof setTimeout> | null = null
      const listener = (event: MessageEvent) => {
        if (event.source !== win || !isRelayMessage(event.data, direction, id)) return
        win.removeEventListener('message', listener)
        if (timer !== null) clearTimeout(timer)
        resolve(event.data)
      }
      win.addEventListener('message', listener)
      if (timeoutMilliseconds !== undefined) {
        timer = setTimeout(() => {
          win.removeEventListener('message', listener)
          resolve(null)
        }, timeoutMilliseconds)
      }
    })

  const isContentScriptListening = async (): Promise<boolean> => {
    if (isRelayKnown) return true
    const id = makeId(win)
    const pong = waitForReply(id, 'pong', HANDSHAKE_MILLISECONDS)
    win.postMessage({ channel: PASSKEY_CHANNEL, direction: 'ping', id } satisfies RelayMessage, targetOrigin)
    isRelayKnown = (await pong) !== null
    return isRelayKnown
  }

  const ask = async (
    ceremonyKind: 'create' | 'get',
    options: Record<string, unknown>,
    signal: AbortSignal | undefined,
  ): Promise<PasskeyReply> => {
    if (!(await isContentScriptListening())) return { replyOutcome: 'fallback' }
    const id = makeId(win)
    const reply = waitForReply(id, 'reply')
    win.postMessage(
      { channel: PASSKEY_CHANNEL, direction: 'request', id, ceremonyKind, options } satisfies RelayMessage,
      targetOrigin,
    )
    if (!signal) return (await reply)?.reply ?? { replyOutcome: 'fallback' }
    return new Promise<PasskeyReply>((resolve, reject) => {
      const onAbort = () => {
        win.postMessage({ channel: PASSKEY_CHANNEL, direction: 'cancel', id } satisfies RelayMessage, targetOrigin)
        reject(abortReason(signal))
      }
      signal.addEventListener('abort', onAbort, { once: true })
      void reply.then((message) => {
        signal.removeEventListener('abort', onAbort)
        resolve(message?.reply ?? { replyOutcome: 'fallback' })
      })
    })
  }

  const settle = (reply: PasskeyReply, ceremonyKind: 'create' | 'get'): unknown => {
    if (reply.replyOutcome === 'credential') {
      return ceremonyKind === 'create'
        ? buildAttestationCredential(reply.credential as AttestationJson)
        : buildAssertionCredential(reply.credential as AssertionJson)
    }
    if (reply.replyOutcome === 'error') {
      if (reply.errorName === 'TypeError') throw new TypeError(reply.errorMessage)
      throw new DOMException(reply.errorMessage, reply.errorName)
    }
    return undefined
  }

  const create = async function create(this: unknown, options?: CredentialCreationOptions) {
    if (!options || typeof options.publicKey !== 'object' || options.publicKey === null) return originalCreate(options)
    // A passkey the browser offers to make without a question (conditional)
    // or no question at all (silent): not ours to ask about.
    // (The type of a create request does not list mediation yet; browsers read it.)
    const createMediation = (options as { mediation?: string }).mediation
    if (createMediation === 'conditional' || createMediation === 'silent') return originalCreate(options)
    if (options.signal?.aborted) throw abortReason(options.signal)
    let copied: Record<string, unknown>
    try {
      copied = copyCreationOptions(options.publicKey)
    } catch {
      return originalCreate(options)
    }
    const reply = await ask('create', copied, options.signal)
    if (reply.replyOutcome === 'fallback') return originalCreate(options)
    return settle(reply, 'create')
  }

  const get = async function get(this: unknown, options?: CredentialRequestOptions) {
    if (!options || typeof options.publicKey !== 'object' || options.publicKey === null) return originalGet(options)
    // The browser's own list of passkeys, shown under the field (conditional),
    // or a sign-in meant to happen with no window at all (silent): not replaced.
    if (options.mediation === 'conditional' || options.mediation === 'silent') return originalGet(options)
    if (options.signal?.aborted) throw abortReason(options.signal)
    let copied: Record<string, unknown>
    try {
      copied = copyRequestOptions(options.publicKey)
    } catch {
      return originalGet(options)
    }
    const reply = await ask('get', copied, options.signal)
    if (reply.replyOutcome === 'fallback') return originalGet(options)
    return settle(reply, 'get')
  }

  Object.defineProperty(credentials, 'create', { value: create, writable: true, configurable: true })
  Object.defineProperty(credentials, 'get', { value: get, writable: true, configurable: true })
}

// Calling the server's GraphQL endpoint with the token the options page
// signed in with. The token is not a tap (the dashboard's or the iPhone's
// own session): it reads and saves items and unlocks, and cannot set up,
// share or grant the agent.

import { serverAddressProblem } from '../serverAddress'

// The sentences the server writes for a person, which are shown as they are.
// Anything else it says (a validation message, a rate limit, an internal
// error) is put in the words below, so a person is not shown what is meant
// for a log.
const SERVER_SENTENCES_TO_SHOW = new Set([
  'That passphrase is not right.',
  'That recovery key is not right.',
  'This item changed on another device. Open it again.',
  'Passwords is not set up yet.',
  'Passwords is already set up.',
])

// friendlyServerWords turns what the server answered (the messages of a
// GraphQL error, or an HTTP status when there are none) into one sentence.
export function friendlyServerWords(messages: string[], status = 200): string {
  const shown = messages.find((message) => SERVER_SENTENCES_TO_SHOW.has(message.trim()))
  if (shown) return shown.trim()
  const text = messages.join(' ').toLowerCase()
  if (status === 401 || /unauthori[sz]ed|unauthenticated|not signed in|invalid token|token (has )?expired/.test(text)) {
    return 'Sign in to TeaNode from the extension options again.'
  }
  if (status === 429 || /rate.?limit|too many|try again later|slow down/.test(text)) {
    return 'Too many tries. Wait a minute and try again.'
  }
  if (text.includes('approve it on the dashboard')) {
    return 'That is done on the Passwords page of the dashboard, not from the extension.'
  }
  if (status === 403 || /permission|forbidden|not allowed|denied/.test(text)) {
    return 'The server does not let this sign-in do that.'
  }
  if (status === 404 || /not found|no such/.test(text)) return 'The server did not know that item.'
  if (status >= 500) return 'The server had a problem. Try again in a moment.'
  return 'The server could not do that. Try again in a moment.'
}

export class ServerCallError extends Error {
  readonly isSignedOut: boolean

  constructor(message: string, isSignedOut = false) {
    super(message)
    this.name = 'ServerCallError'
    this.isSignedOut = isSignedOut
  }
}

export async function readServerOrigin(): Promise<string> {
  const { server } = await chrome.storage.sync.get('server')
  return typeof server === 'string' ? server.replace(/\/+$/, '') : ''
}

export async function readToken(): Promise<string> {
  const { token } = await chrome.storage.local.get('token')
  return typeof token === 'string' ? token : ''
}

// isSignedIn: the extension has a server address and a token.
export async function isSignedIn(): Promise<boolean> {
  return (await readServerOrigin()) !== '' && (await readToken()) !== ''
}

export async function callServer(query: string, variables: Record<string, unknown> = {}): Promise<any> {
  const origin = await readServerOrigin()
  const token = await readToken()
  if (!/^https?:\/\//i.test(origin) || token === '') {
    throw new ServerCallError('Sign in to TeaNode from the extension options first.', true)
  }
  // The token is never sent in the clear to a server on another computer.
  const addressProblem = serverAddressProblem(origin)
  if (addressProblem !== '') throw new ServerCallError(addressProblem)
  let response: Response
  try {
    response = await fetch(`${origin}/api/v1/graphql`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
      body: JSON.stringify({ query, variables }),
    })
  } catch {
    throw new ServerCallError('The server could not be reached.')
  }
  if (response.status === 401) throw new ServerCallError(friendlyServerWords([], 401), true)
  if (!response.ok) throw new ServerCallError(friendlyServerWords([], response.status))
  const body = await response.json()
  if (body.errors && body.errors.length > 0) {
    const messages = body.errors.map((error: { message?: unknown }) => String(error.message ?? ''))
    const words = friendlyServerWords(messages)
    throw new ServerCallError(words, words === friendlyServerWords([], 401))
  }
  return body.data
}

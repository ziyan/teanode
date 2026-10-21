// Calling the server's GraphQL endpoint with the token the options page
// signed in with. The token is not a tap (the dashboard's or the iPhone's
// own session): it reads and saves items and unlocks, and cannot set up,
// share or grant the agent.

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
  if (response.status === 401) throw new ServerCallError('Sign in to TeaNode from the extension options again.', true)
  if (!response.ok) throw new ServerCallError(`The server answered ${response.status}.`)
  const body = await response.json()
  if (body.errors && body.errors.length > 0) {
    throw new ServerCallError(body.errors.map((error: { message: string }) => error.message).join('; '))
  }
  return body.data
}

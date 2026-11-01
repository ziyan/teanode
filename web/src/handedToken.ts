// The browser extension's side panel frames the drawer, and the drawer needs
// the person's token to sign in. The token is not in the frame's address,
// where it would be written into logs and history. The side panel posts it as
// a message once the frame has loaded, with the server's origin as the target
// origin, so only a document of the server receives it.
//
// The drawer takes a token from a message only when every one of these holds,
// because a message is something any window that holds a reference to this one
// can send:
//
//   - the page is framed, and has exactly one ancestor;
//   - that ancestor's origin is a chrome-extension:// origin. A website can
//     never have one, so the only possible parent is a page of an extension.
//     (The server also answers /drawer with frame-ancestors chrome-extension:,
//     so the browser refuses to frame it anywhere else to begin with);
//   - the message came from that parent window, and its origin is the
//     ancestor's origin;
//   - it says it is a token, and the token is a non-empty string;
//   - no token was taken before: the first is kept and every later one is
//     refused, so a second extension page cannot swap the account.
//
// ancestorOrigins is a Chrome feature, which is the only browser the
// extension runs in. Without it, nothing is accepted.
const EXTENSION_ORIGIN_PREFIX = 'chrome-extension://'

let handedToken = ''
const waiting = new Set<(token: string) => void>()

// peekHandedToken is the token the extension handed over before anybody was
// waiting for it, or ''. It stays until forgetHandedToken, so that a render
// that runs twice finds it twice.
export function peekHandedToken(): string {
  return handedToken
}

export function forgetHandedToken() {
  handedToken = ''
}

// whenTokenIsHanded calls back with the token the moment the extension hands
// it over, and says how to stop waiting.
export function whenTokenIsHanded(callback: (token: string) => void): () => void {
  waiting.add(callback)
  return () => {
    waiting.delete(callback)
  }
}

// The parts of the window the checks read, so that a test can give another.
type HandingWindow = Pick<Window, 'addEventListener' | 'parent' | 'top'> & {
  location: { ancestorOrigins?: ArrayLike<string> }
}

// listenForHandedToken starts taking the one token the extension's side panel
// posts, in a framed drawer only. Call it as soon as the bundle runs: the
// panel posts after the frame's load event.
export function listenForHandedToken(target: HandingWindow = window): void {
  let isTaken = false
  target.addEventListener('message', (event: MessageEvent) => {
    if (isTaken) return
    if (target === target.top) return
    const ancestorOrigins = target.location.ancestorOrigins
    if (!ancestorOrigins || ancestorOrigins.length !== 1) return
    const parentOrigin = ancestorOrigins[0]
    if (!parentOrigin.startsWith(EXTENSION_ORIGIN_PREFIX)) return
    if (event.source !== target.parent || event.origin !== parentOrigin) return
    const message: unknown = event.data
    if (typeof message !== 'object' || message === null) return
    const { teanode, token } = message as { teanode?: unknown; token?: unknown }
    if (teanode !== 'token' || typeof token !== 'string' || token === '') return
    isTaken = true
    if (waiting.size === 0) handedToken = token
    else for (const callback of [...waiting]) callback(token)
  })
}

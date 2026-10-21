// The browser extension's panel frames the drawer on whatever site the
// person is on, and the drawer needs the person's token to sign in. The
// token is not in the frame's address, where an element's src attribute
// would keep it and the DevTools protocol would read it back through the
// shadow root the panel sits in. The extension's worker hands it over
// instead, by calling this function in the frame's own world: a page around
// the frame cannot call into a frame of another origin, and the token is
// taken once.
//
// web/extension/src/reach.js names the function the same way, and a test
// keeps the two equal.
export const HAND_TOKEN_FUNCTION = '__teanodeHandToken'

type HandTokenWindow = Window & { [HAND_TOKEN_FUNCTION]?: (token: string) => boolean }

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

// offerHandedToken puts the function on the window, in a framed drawer only.
// The worker calls it, from the frame's own world and only after checking that
// this is the drawer it means, and it takes one token: afterwards it refuses.
// It is defined so that nothing on the page can replace it before then, or
// after: not writable, not configurable.
export function offerHandedToken(target: HandTokenWindow = window as HandTokenWindow) {
  if (HAND_TOKEN_FUNCTION in target) return
  let isTaken = false
  Object.defineProperty(target, HAND_TOKEN_FUNCTION, {
    value: (token: string) => {
      if (isTaken || typeof token !== 'string' || token === '') return false
      isTaken = true
      if (waiting.size === 0) handedToken = token
      else for (const callback of [...waiting]) callback(token)
      return true
    },
    writable: false,
    configurable: false,
    enumerable: false,
  })
}

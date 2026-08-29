// The browser's half of a WebAuthn ceremony.
//
// The server speaks the specification's JSON encoding, where the challenge and
// the credential identifiers are base64url strings; the browser API wants
// ArrayBuffers. parseRequestOptionsFromJSON and toJSON do that conversion,
// which is why there is no hand-rolled base64 here — getting it subtly wrong
// produces a challenge mismatch that reads as a server bug and is not one.

// isPasskeySupported reports whether this browser can do any of it. Checked
// before offering the button: one that cannot work is worse than none, because
// somebody commits to it before finding out.
export function isPasskeySupported(): boolean {
  return (
    typeof window !== 'undefined' &&
    typeof window.PublicKeyCredential !== 'undefined' &&
    typeof PublicKeyCredential.parseRequestOptionsFromJSON === 'function' &&
    typeof PublicKeyCredential.parseCreationOptionsFromJSON === 'function'
  )
}

// getAssertion runs the sign-in ceremony and returns what the server needs to
// verify, serialised.
export async function getAssertion(options: string): Promise<string> {
  const parsed = JSON.parse(options)
  const request = PublicKeyCredential.parseRequestOptionsFromJSON(parsed.publicKey ?? parsed)
  const credential = await navigator.credentials.get({ publicKey: request })
  if (!credential) {
    throw new Error('no passkey was returned')
  }
  return JSON.stringify((credential as PublicKeyCredential).toJSON())
}

// createCredential runs the registration ceremony.
export async function createCredential(options: string): Promise<string> {
  const parsed = JSON.parse(options)
  const creation = PublicKeyCredential.parseCreationOptionsFromJSON(parsed.publicKey ?? parsed)
  const credential = await navigator.credentials.create({ publicKey: creation })
  if (!credential) {
    throw new Error('no passkey was created')
  }
  return JSON.stringify((credential as PublicKeyCredential).toJSON())
}

// cancelled reports whether a ceremony ended because the person dismissed the
// browser's prompt rather than because anything went wrong.
//
// Worth telling apart: somebody who closed the dialog does not want to be told
// their passkey failed, and an error banner for a deliberate cancel is the
// kind of thing that makes people stop trusting the banner.
export function cancelled(error: unknown): boolean {
  return error instanceof DOMException && (error.name === 'NotAllowedError' || error.name === 'AbortError')
}

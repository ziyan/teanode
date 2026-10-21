// What the Passwords code throws itself is a code, and each screen looks up the
// words for it (web/src/pages/passwords/passwordsErrors.ts), so a toast in
// another language is never English text from a thrown Error. This file has
// no imports so the browser extension can use the same errors without the
// dashboard's screens.

export type PasswordsErrorCode =
  | 'locked'
  | 'notSetUp'
  | 'iterationCountOutOfRange'
  | 'itemUnreadable'
  | 'itemUnknown'
  | 'ownerOnly'
  | 'publicKeyMismatch'

export class PasswordsError extends Error {
  readonly code: PasswordsErrorCode

  constructor(code: PasswordsErrorCode) {
    super(code)
    this.name = 'PasswordsError'
    this.code = code
  }
}

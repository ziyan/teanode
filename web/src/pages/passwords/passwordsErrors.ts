import { APIError } from '../../api'
import { Key, useTranslation } from '../../i18n/i18n'
import { useToast } from '../../components/toast'
import { PasswordsError, PasswordsErrorCode } from '../../passwords/passwordsError'

// What the Passwords screens throw themselves is a code, and the words for it
// are looked up here, so a toast in Chinese or Japanese is never English text
// from a thrown Error.

export { PasswordsError } from '../../passwords/passwordsError'
export type { PasswordsErrorCode } from '../../passwords/passwordsError'

// PasswordsAccessError is what the session throws when it will not give an item
// a new key by itself: its sharing could not be checked (accessNotVerified), or
// automatic new keys are used up for the moment (rekeyLimited), or the server
// answered a change of sharing with a record older than the one just written
// (sharingNotKept). The first needs the person to set sharing again, the second
// a tap on Try again, the third another try.
export type PasswordsAccessErrorCode = 'accessNotVerified' | 'rekeyLimited' | 'sharingNotKept'

export class PasswordsAccessError extends Error {
  readonly code: PasswordsAccessErrorCode

  constructor(code: PasswordsAccessErrorCode) {
    super(code)
    this.name = 'PasswordsAccessError'
    this.code = code
  }
}

const ACCESS_ERROR_WORDS: Record<PasswordsAccessErrorCode, Key> = {
  accessNotVerified: 'passwords.errorAccessNotVerified',
  rekeyLimited: 'passwords.errorRekeyLimited',
  sharingNotKept: 'passwords.errorSharingNotKept',
}

const ERROR_WORDS: Record<PasswordsErrorCode, Key> = {
  locked: 'passwords.errorLocked',
  notSetUp: 'passwords.errorNotSetUp',
  iterationCountOutOfRange: 'passwords.errorIterationCount',
  itemUnreadable: 'passwords.errorItemUnreadable',
  itemUnknown: 'passwords.errorItemUnknown',
  ownerOnly: 'passwords.errorOwnerOnly',
  publicKeyMismatch: 'passwords.errorPublicKeyMismatch',
}

// failureText is what a toast says about a failure: the words for one of our
// own codes, the server's own words for a refusal from the server, and the
// general fallback for anything else (a crypto library's English message, a
// network error).
export function failureText(
  failure: unknown,
  t: (key: Key, values?: Record<string, string | number>) => string,
  fallback: string,
): string {
  if (failure instanceof PasswordsError) return t(ERROR_WORDS[failure.code])
  if (failure instanceof PasswordsAccessError) return t(ACCESS_ERROR_WORDS[failure.code])
  if (failure instanceof APIError && failure.message) return failure.message
  return fallback
}

// useFailureToast gives the function the screens call in a catch.
export function useFailureToast(): (failure: unknown, fallback: string) => void {
  const { t } = useTranslation()
  const toast = useToast()
  return (failure, fallback) => toast.failed(failureText(failure, t, fallback))
}

import { APIError } from '../../api'
import { Key, useTranslation } from '../../i18n/i18n'
import { useToast } from '../../components/toast'

// What the Passwords screens throw themselves is a code, and the words for it
// are looked up here, so a toast in Chinese or Japanese is never English text
// from a thrown Error.

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
  if (failure instanceof APIError && failure.message) return failure.message
  return fallback
}

// useFailureToast gives the function the screens call in a catch.
export function useFailureToast(): (failure: unknown, fallback: string) => void {
  const { t } = useTranslation()
  const toast = useToast()
  return (failure, fallback) => toast.failed(failureText(failure, t, fallback))
}

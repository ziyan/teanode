import { RelativeTime } from './relativeTime'
import { formatTime } from './common'
import { useTranslation } from '../i18n/i18n'

// A person with three passwords called "iPhone" has to decide which one to
// revoke from what the list says about each, so every password list shows
// the same three facts: when it was made, when it last signed anything in,
// and whether it looks abandoned. Nothing is ever revoked from here; the
// hint only says a password may be safe to delete.

// A password nobody has used within this long of being made was probably
// replaced by another one made right after it.
const NEVER_USED_GRACE_MS = 60 * 60 * 1000
// A password silent this long is more likely forgotten than occasional.
const LONG_UNUSED_MS = 90 * 24 * 60 * 60 * 1000

export type PasswordTimes = { createdAt: string; lastUsedAt?: string | null }

// sortNewestFirst orders by creation time, so a freshly made password is at
// the top where the person who just made it looks for it.
export function sortNewestFirst<T extends PasswordTimes>(passwords: T[]): T[] {
  return [...passwords].sort((left, right) => Date.parse(right.createdAt) - Date.parse(left.createdAt))
}

// isMaybeSafeToDelete reports a password never used more than an hour after
// it was made, or not used for a long time.
export function isMaybeSafeToDelete(password: PasswordTimes, now = Date.now()): boolean {
  const createdAtMS = Date.parse(password.createdAt)
  if (!password.lastUsedAt) {
    return Number.isFinite(createdAtMS) && now - createdAtMS > NEVER_USED_GRACE_MS
  }
  const lastUsedAtMS = Date.parse(password.lastUsedAt)
  return Number.isFinite(lastUsedAtMS) && now - lastUsedAtMS > LONG_UNUSED_MS
}

// PasswordUsage is the muted line under a password's name.
export function PasswordUsage({ password }: { password: PasswordTimes }) {
  const { t } = useTranslation()
  return (
    <>
      {t('passwordUsage.created', { time: formatTime(password.createdAt) })}
      {' · '}
      {password.lastUsedAt ? (
        <>
          {t('passwordUsage.lastUsed')} <RelativeTime value={password.lastUsedAt} />
        </>
      ) : (
        t('passwordUsage.neverUsed')
      )}
      {isMaybeSafeToDelete(password) ? ` · ${t('passwordUsage.maybeSafeToDelete')}` : null}
    </>
  )
}

import { useState } from 'react'
import { Link } from 'react-router-dom'

import { endImpersonation } from '../api'
import { LanguageItems, useTranslation } from '../i18n/i18n'
import { formatClock } from './common'
import { ConfirmDialog } from './dialog'
import {
  ArrowLeftIcon,
  ChevronDownIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  GridIcon,
  LogoutIcon,
  SettingsIcon,
  ShieldIcon,
} from './icons'
import { MenuButton } from './menuButton'
import { ThemeItems } from './theme'
import { SETTINGS_LANDING } from '../pages/settings/nav'
import { useSession } from '../session'
import { firstManagementPath } from './sidebar'
import { useToast } from './toast'

// AccountMenu is who you are, at the foot of the rail, and everything that
// belongs to you rather than to a page: which language, light or dark, the way
// into your own settings, and the way out.
//
// At the foot rather than in the corner because that is where a person looks
// for their own name, and because the corner is for things about the page
// while this is about the session. The account's settings are here rather than
// in the rail for the same reason: nobody scanning a list of mail domains is
// looking for where to change their own password.
//
// Four short groups with a rule between, not a list of nine: which language,
// then light or dark, then settings, then the way out. The account's own pages
// are behind that one Settings row rather than listed here, because opening
// them swaps the rail for them — they are a place to be in, and a menu is not
// a place.
export function AccountMenu({
  username,
  name,
  onLogout,
  collapsed,
  onToggleSidebar,
}: {
  username: string

  // What this person asked to be called. The row greets them by it; the menu
  // header still says which account that is, because the name is not what you
  // sign in with and two people can choose the same one.
  name?: string

  onLogout: () => void

  // Whether the rail is narrowed, and how to change it. Absent where there is
  // nothing to narrow — the drawer on a phone opens and closes by other
  // means.
  collapsed?: boolean
  onToggleSidebar?: () => void
}) {
  const { t } = useTranslation()
  const session = useSession()
  const managePath = firstManagementPath(session.permissions)
  const [signingOut, setSigningOut] = useState(false)
  const [returning, setReturning] = useState(false)
  const toast = useToast()
  const displayed = name?.trim() || username
  // An operator signed in as this account: said by a mark on the avatar,
  // which is on screen on every page without taking a line of any of them,
  // and in the menu the avatar opens, with the way back.
  const operator = session.impersonatorUsername
  const signedInAs = operator
    ? session.impersonationEndsAt
      ? t('impersonation.noteUntil', { username, operator, time: formatClock(session.impersonationEndsAt) })
      : t('impersonation.note', { username, operator })
    : ''

  return (
    <>
      <MenuButton
        className="account-button"
        label={operator ? `${t('nav.account')}: ${signedInAs}` : t('nav.account')}
        placement="above"
        icon={
          <>
            <span className={operator ? 'avatar avatar-impersonated' : 'avatar'} aria-hidden="true">
              {initial(displayed)}
              {operator && (
                <span className="avatar-badge">
                  <ShieldIcon size={10} />
                </span>
              )}
            </span>
            <span className="sidebar-label account-name">{displayed}</span>
            {/* A single arrow that turns when the menu opens. Two arrows
                pointing apart is what a sortable column header wears, and it
                said "this reorders something" on a button that opens a
                menu. */}
            <span className="sidebar-label account-chevron" aria-hidden="true">
              <ChevronDownIcon size={14} className="chevron" />
            </span>
          </>
        }
        render={(close) => (
          <>
            {/* Above everything while signed in as somebody else: whose
                account this is, who is looking, and the way back. */}
            {operator && (
              <>
                <div className="menu-header menu-impersonation">{signedInAs}</div>
                <button
                  type="button"
                  role="menuitem"
                  disabled={returning}
                  onClick={async () => {
                    setReturning(true)
                    try {
                      await endImpersonation()
                      // Everything on the page was the person's: start
                      // again as the operator rather than leave any of it.
                      window.location.assign('/')
                    } catch (caught) {
                      setReturning(false)
                      close()
                      toast.failure(caught, t('impersonation.returnFailed'))
                    }
                  }}
                >
                  <ArrowLeftIcon />
                  {t('impersonation.return')}
                </button>
                <div className="menu-separator" role="separator" />
              </>
            )}
            {/* First, because it is the thing done most often here and the
                only one about the window rather than about the account. It
                was a row at the foot of the rail, where it was one of the
                things it was hiding. */}
            {onToggleSidebar && (
              <>
                <button
                  type="button"
                  role="menuitem"
                  onClick={onToggleSidebar}
                >
                  {/* Which way the rail is about to go: left to narrow it,
                      right to bring it back. One arrow for both said the
                      control did the same thing twice. */}
                  {collapsed ? <ChevronRightIcon /> : <ChevronLeftIcon />}
                  {collapsed ? t('nav.expand') : t('nav.collapse')}
                </button>
                <div className="menu-separator" role="separator" />
              </>
            )}
            <LanguageItems />
            <div className="menu-separator" role="separator" />
            <ThemeItems />
            <div className="menu-separator" role="separator" />
            <Link to={SETTINGS_LANDING} role="menuitem" onClick={close}>
              <SettingsIcon />
              {t('nav.settings')}
            </Link>
            {/* The way into the management side, beside the account's own
                settings because the two are the same kind of thing: places
                that are not the mailbox, entered on purpose. It was a row at
                the foot of the rail, where it sat among the mailbox's folders
                looking like one more of them. Only for somebody who has
                anything to manage. */}
            {managePath && (
              <Link to={managePath} role="menuitem" onClick={close}>
                <GridIcon />
                {t('nav.manage')}
              </Link>
            )}
            <div className="menu-separator" role="separator" />
            <button
              type="button"
              role="menuitem"
              onClick={() => {
                close()
                setSigningOut(true)
              }}
            >
              <LogoutIcon />
              {t('nav.logOut')}
            </button>
          </>
        )}
      />

      {/* Asked rather than done. Signing out is one click away from things
          somebody opened this menu to reach, and getting back in costs a
          password. It takes nothing away, so it is not colored as though it
          did. */}
      {signingOut && (
        <ConfirmDialog
          title={t('nav.logOutTitle')}
          body={t('nav.logOutBody')}
          confirmLabel={t('nav.logOut')}
          destructive={false}
          onConfirm={() => {
            setSigningOut(false)
            onLogout()
          }}
          onClose={() => setSigningOut(false)}
        />
      )}
    </>
  )
}

// initial is the first character of what is displayed, which is enough to tell
// one account from another and does not pretend to be a picture nobody
// uploaded.
//
// Array.from rather than charAt, so a name beginning with an emoji or a
// character outside the basic plane is not cut in half.
function initial(value: string): string {
  return (Array.from(value.trim())[0] ?? '?').toUpperCase()
}

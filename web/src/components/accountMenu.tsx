import { useState } from 'react'
import { Link } from 'react-router-dom'

import { LanguageItems, useTranslation } from '../i18n/i18n'
import { ConfirmDialog } from './dialog'
import { LogoutIcon, SettingsIcon, SortIcon } from './icons'
import { MenuButton } from './menuButton'
import { ThemeItems } from './theme'
import { SETTINGS_LANDING } from '../pages/settings/nav'

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
}: {
  username: string

  // What this person asked to be called. The row greets them by it; the menu
  // header still says which account that is, because the name is not what you
  // sign in with and two people can choose the same one.
  name?: string

  onLogout: () => void
}) {
  const { t } = useTranslation()
  const [signingOut, setSigningOut] = useState(false)
  const displayed = name?.trim() || username

  return (
    <>
      <MenuButton
        className="account-button"
        label={t('nav.account')}
        placement="above"
        icon={
          <>
            <span className="avatar" aria-hidden="true">
              {initial(displayed)}
            </span>
            <span className="sidebar-label account-name">{displayed}</span>
            <span className="sidebar-label account-chevron" aria-hidden="true">
              <SortIcon size={14} />
            </span>
          </>
        }
        render={(close) => (
          <>
            <div className="menu-header">{t('nav.signedInAs', { username })}</div>
            <LanguageItems close={close} />
            <div className="menu-separator" role="separator" />
            <ThemeItems close={close} />
            <div className="menu-separator" role="separator" />
            <Link to={SETTINGS_LANDING} role="menuitem" onClick={close}>
              <SettingsIcon />
              {t('nav.settings')}
            </Link>
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
          password. It takes nothing away, so it is not coloured as though it
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

import { useCallback, useSyncExternalStore } from 'react'

import { Key, useTranslation } from '../i18n/i18n'
import { AutoThemeIcon, MoonIcon, SunIcon } from './icons'
import { MenuButton } from './menuButton'

// Theme is what the reader chose, not what they are currently seeing.
// "system" means follow the operating system, which is the default and what
// most people want; the other two are for when it is wrong.
export type Theme = 'system' | 'light' | 'dark'

const STORAGE_KEY = 'teanode.theme'

function isTheme(value: string | null): value is Theme {
  return value === 'system' || value === 'light' || value === 'dark'
}

// storedTheme reads the choice, tolerating a browser that refuses local
// storage — private browsing does, and the dashboard should still work.
function storedTheme(): Theme {
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY)
    return isTheme(stored) ? stored : 'system'
  } catch {
    return 'system'
  }
}

// applyTheme stamps the choice on the document. "system" stamps nothing, so
// the prefers-color-scheme rules in the stylesheet decide.
export function applyTheme(theme: Theme) {
  const root = document.documentElement
  if (theme === 'system') {
    root.removeAttribute('data-theme')
    return
  }
  root.setAttribute('data-theme', theme)
}

// initializeTheme applies the stored choice before React renders, so the page
// does not paint in one theme and then switch to the other.
export function initializeTheme() {
  applyTheme(storedTheme())
}

// One choice, not one per component.
//
// This was useState inside the hook, which meant every caller had a copy: the
// icon on the toggle held one and the list of options inside its menu held
// another. Two bugs came out of that. The icon did not change when the list
// set the theme, because it was watching its own copy — and worse, choosing an
// option applied nothing at all, because the list applied the theme from an
// effect and choosing an option closes the menu, which unmounts the list
// before the effect runs. The choice was written to storage, so the next time
// the menu opened it mounted, read it back and applied it then: light and dark
// appeared to need two clicks.
//
// So: the value lives here, applied the moment it is set rather than as a
// consequence of a render, and every component reads the same one.
let current: Theme = storedTheme()
const listeners = new Set<() => void>()

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function useTheme(): [Theme, (theme: Theme) => void] {
  const theme = useSyncExternalStore(subscribe, () => current)

  const setTheme = useCallback((next: Theme) => {
    current = next
    // Applied here rather than from an effect. An effect belonging to a
    // component that the same click unmounts is an effect that never runs.
    applyTheme(next)
    try {
      window.localStorage.setItem(STORAGE_KEY, next)
    } catch {
      // Not being able to remember the choice is not a reason to ignore it
      // for this visit.
    }
    for (const listener of listeners) {
      listener()
    }
  }, [])

  return [theme, setTheme]
}

// The system theme has to be resolved to know what is on screen, and it can
// change under a running page — the OS switching at dusk — so this subscribes
// to the media query rather than reading it once.
const darkQuery = typeof window !== 'undefined' ? window.matchMedia('(prefers-color-scheme: dark)') : null

function subscribeSystem(listener: () => void): () => void {
  darkQuery?.addEventListener('change', listener)
  return () => darkQuery?.removeEventListener('change', listener)
}

// useResolvedTheme is the theme as rendered: "light" or "dark", never
// "system". For anything that has to write a colour rather than use a token —
// the message frame builds its document as a string and cannot use var().
export function useResolvedTheme(): 'light' | 'dark' {
  const [theme] = useTheme()
  const systemDark = useSyncExternalStore(subscribeSystem, () => darkQuery?.matches ?? false)
  if (theme === 'system') {
    return systemDark ? 'dark' : 'light'
  }
  return theme
}

const LABELS: Record<Theme, Key> = {
  system: 'theme.system',
  light: 'theme.light',
  dark: 'theme.dark',
}

// ThemeToggle cycles through the three states. Three radio buttons would say
// more, and take more room than a setting this small deserves; the label
// always says what is in effect, so nothing is hidden.
export function ThemeToggle() {
  const [theme] = useTheme()
  const { t } = useTranslation()

  return (
    <MenuButton
      className="icon-button"
      label={t('theme.label', { theme: t(LABELS[theme]) })}
      icon={<ThemeIcon theme={theme} />}
      render={(close) => <ThemeItems close={close} />}
    />
  )
}

// The same options as rows, for a menu that has other things in it too.
export function ThemeItems({ close }: { close: () => void }) {
  const [theme, setTheme] = useTheme()
  const { t } = useTranslation()
  const order: Theme[] = ['system', 'light', 'dark']

  return (
    <>
      {order.map((option) => (
        <button
          key={option}
          type="button"
          role="menuitemradio"
          aria-checked={option === theme}
          className={option === theme ? 'selected' : undefined}
          onClick={() => {
            setTheme(option)
            close()
          }}
        >
          <ThemeIcon theme={option} />
          {t(LABELS[option])}
        </button>
      ))}
    </>
  )
}

function ThemeIcon({ theme }: { theme: Theme }) {
  if (theme === 'light') {
    return <SunIcon />
  }
  if (theme === 'dark') {
    return <MoonIcon />
  }
  return <AutoThemeIcon />
}

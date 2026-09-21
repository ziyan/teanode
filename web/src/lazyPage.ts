import { ComponentType, LazyExoticComponent, lazy } from 'react'

// A page that is not in the first download arrives as a file of its own, with
// the build's name on it. After an upgrade those files are gone -- the new
// build named its own -- so a tab left open across one asks the server for
// something that no longer exists.
//
// That is not a failure worth showing anybody: the page they are looking at is
// the old build, and the fix is to load the new one. Once, though. A fetch
// that fails for any other reason -- the network, a proxy -- would otherwise
// reload forever, so the second time the error is left to be thrown.
const RELOADED = 'teanode.reloadedForANewBuild'

function remember(value: string | null) {
  try {
    if (value === null) {
      window.sessionStorage.removeItem(RELOADED)
    } else {
      window.sessionStorage.setItem(RELOADED, value)
    }
  } catch {
    // Private browsing refuses session storage. Then the guard against a
    // second reload is gone, and the reload below happens at most once
    // anyway: the page it loads is the new build, whose files are there.
  }
}

function reloadedAlready(): boolean {
  try {
    return window.sessionStorage.getItem(RELOADED) === '1'
  } catch {
    return false
  }
}

export function lazyPage<P>(load: () => Promise<{ default: ComponentType<P> }>): LazyExoticComponent<ComponentType<P>> {
  return lazy(() =>
    load().then(
      (module) => {
        remember(null)
        return module
      },
      (error: unknown) => {
        if (reloadedAlready()) {
          throw error
        }
        remember('1')
        window.location.reload()
        // The page is going away; resolving with anything would draw it.
        return new Promise<{ default: ComponentType<P> }>(() => {})
      },
    ),
  )
}

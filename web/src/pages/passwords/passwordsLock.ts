// The one way the rest of the dashboard locks Passwords. This file imports
// nothing on purpose: app.tsx imports it, and anything it imported would come
// into the first download with the shell. The Passwords session registers its
// own lock here when the page's code is loaded; until then there is nothing
// open, so locking does nothing.

let lockHandler: (() => void) | null = null

// registerLock says what lockPasswords does.
export function registerLock(handler: () => void) {
  lockHandler = handler
}

// lockPasswords closes Passwords if it is open: on signing out, and when the
// signed-in person changes. Before the page's code is loaded (signing out
// right after a reload), the copy of the keys kept for a reload is deleted
// here, by its database's name, which must stay the one in
// passwordsKeptUnlock.ts.
export function lockPasswords() {
  if (lockHandler !== null) {
    lockHandler()
    return
  }
  try {
    if (typeof indexedDB !== 'undefined') indexedDB.deleteDatabase('teanode-passwords')
  } catch {
    // Storage the browser refuses holds nothing to delete.
  }
}

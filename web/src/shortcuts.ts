import { useEffect, useSyncExternalStore } from 'react'

// Keys that act on the page in front of you, and keys that go somewhere else.
//
// The letters are the ones a person coming from another mail program already
// has in their fingers — e to archive, r to reply, j and k to move through a
// list, g then i to go to the inbox — because a shortcut nobody can guess is
// a shortcut nobody uses.
//
// Two rules make the difference between a shortcut and a trap. A key pressed
// while typing is a letter, not a command: anything from a field, a text area
// or an editable element is left alone, which is why writing "eventually" in a
// reply does not archive the thread. And a key pressed with a modifier belongs
// to the browser or the system — Ctrl+R reloads the page, and taking that over
// is how a program earns being turned off.

export type Shortcut = {
  // The key as the keyboard reports it, lower case for letters.
  key: string
  // Held Shift, for the ones that are punctuation on most layouts.
  shift?: boolean
  // The key pressed just before, for the two-key ones: g then i. A letter
  // that is a shortcut on its own is never a prefix, so g is free for this.
  prefix?: string
  // False when there was nothing to do, so the key keeps what the browser
  // does with it: / with no search box on the page is the browser's find.
  run: () => void | boolean
  // What to call it in the list of shortcuts.
  label: string
}

// Where a set of shortcuts belongs in the list: going somewhere, anywhere on
// the dashboard, or on the page being read.
export type ShortcutSection = 'go' | 'anywhere' | 'page'

type Registration = { shortcuts: Shortcut[]; section: ShortcutSection }

// How long a prefix waits for its second key. Long enough to find the key,
// short enough that a g pressed and forgotten does not turn the next j into
// something else a minute later.
const PREFIX_WAIT_MS = 1500

// Every set mounted now, the newest first: a page's own keys are asked before
// the dashboard's, so a page can mean something by a letter.
let registrations: Registration[] = []
const listeners = new Set<() => void>()
let pendingPrefix = ''
let pendingTimer: number | undefined

function changed() {
  for (const listener of listeners) {
    listener()
  }
}

// typing says whether what has focus is somewhere a keystroke is text.
function typing(target: EventTarget | null): boolean {
  const element = target as HTMLElement | null
  if (!element || !element.tagName) {
    return false
  }
  if (element.isContentEditable) {
    return true
  }
  return /^(input|textarea|select)$/i.test(element.tagName)
}

function matches(shortcut: Shortcut, key: string, event: KeyboardEvent): boolean {
  if (shortcut.key !== key) {
    return false
  }
  return shortcut.key.length !== 1 || (shortcut.shift ?? false) === event.shiftKey
}

// One listener for every set, so that a prefix is one state: with a listener
// per set, the i after a g would also reach a page that means i itself.
//
// Nothing while a dialog is open: a question on the screen is the only thing
// being answered, and archiving the message behind it because the answer
// began with "e" would be a surprise nobody could undo.
function onKeyDown(event: KeyboardEvent) {
  if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey) {
    return
  }
  if (typing(event.target)) {
    return
  }
  if (document.querySelector('.dialog-scrim')) {
    return
  }
  const key = event.key.length === 1 ? event.key.toLowerCase() : event.key
  if (key === 'Shift') {
    return
  }
  const prefix = pendingPrefix
  pendingPrefix = ''
  window.clearTimeout(pendingTimer)
  for (const registration of registrations) {
    for (const shortcut of registration.shortcuts) {
      if ((shortcut.prefix ?? '') !== prefix || !matches(shortcut, key, event)) {
        continue
      }
      if (shortcut.run() === false) {
        return
      }
      event.preventDefault()
      return
    }
  }
  if (prefix) {
    // A prefix followed by a key it does not go with: swallowed, rather than
    // read as the key on its own. g then x meant "go somewhere", not x.
    event.preventDefault()
    return
  }
  const startsSequence = registrations.some((registration) =>
    registration.shortcuts.some((shortcut) => shortcut.prefix === key),
  )
  if (startsSequence && !event.shiftKey) {
    event.preventDefault()
    pendingPrefix = key
    pendingTimer = window.setTimeout(() => {
      pendingPrefix = ''
    }, PREFIX_WAIT_MS)
  }
}

function register(registration: Registration): () => void {
  if (registrations.length === 0) {
    document.addEventListener('keydown', onKeyDown)
  }
  registrations = [registration, ...registrations]
  changed()
  return () => {
    registrations = registrations.filter((other) => other !== registration)
    if (registrations.length === 0) {
      document.removeEventListener('keydown', onKeyDown)
      pendingPrefix = ''
      window.clearTimeout(pendingTimer)
    }
    changed()
  }
}

// useShortcuts binds a set of keys for as long as the component is mounted
// and enabled, and lists them under section in the list ? shows.
export function useShortcuts(shortcuts: Shortcut[], enabled = true, section: ShortcutSection = 'page') {
  useEffect(() => {
    if (!enabled || shortcuts.length === 0) {
      return
    }
    return register({ shortcuts, section })
  }, [shortcuts, enabled, section])
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

function snapshot(): Registration[] {
  return registrations
}

// useMountedShortcuts is every key that works right now, for the list of
// them. The page's own first within each section, as they are matched.
export function useMountedShortcuts(): Registration[] {
  return useSyncExternalStore(subscribe, snapshot)
}

// The name a shortcut is shown under: its key, or its two keys.
export function shortcutKeys(shortcut: Shortcut): string[] {
  return shortcut.prefix ? [shortcut.prefix, shortcut.key] : [shortcut.key]
}

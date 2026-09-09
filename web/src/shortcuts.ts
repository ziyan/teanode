import { useEffect } from 'react'

// Keys that act on the message in front of you.
//
// The letters are the ones a person coming from another mail program already
// has in their fingers — e to archive, r to reply, j and k to move through a
// list — because a shortcut nobody can guess is a shortcut nobody uses.
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
  run: () => void
  // What to call it in the list of shortcuts.
  label: string
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

// useShortcuts binds a set of keys for as long as the component is mounted.
//
// Disabled while a dialog is open: a question on the screen is the only thing
// being answered, and archiving the message behind it because the answer began
// with "e" would be a surprise nobody could undo.
export function useShortcuts(shortcuts: Shortcut[], enabled = true) {
  useEffect(() => {
    if (!enabled) {
      return
    }
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
      for (const shortcut of shortcuts) {
        if (shortcut.key !== key) {
          continue
        }
        if ((shortcut.shift ?? false) !== event.shiftKey && shortcut.key.length === 1) {
          continue
        }
        event.preventDefault()
        shortcut.run()
        return
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [shortcuts, enabled])
}

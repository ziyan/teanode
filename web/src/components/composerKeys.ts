import type { KeyboardEvent } from 'react'

// isComposingText is true while an input method is building a character, as
// when typing Chinese or Japanese. Enter then picks a candidate and is not
// the person asking to send. keyCode 229 covers browsers that report the
// composition's last key after isComposing has already cleared.
export function isComposingText(event: KeyboardEvent): boolean {
  return event.nativeEvent.isComposing || event.keyCode === 229
}

// isSendKey is the key every message box sends on: Enter, with Shift for a
// new line instead, and never while text is being composed.
export function isSendKey(event: KeyboardEvent): boolean {
  return event.key === 'Enter' && !event.shiftKey && !isComposingText(event)
}

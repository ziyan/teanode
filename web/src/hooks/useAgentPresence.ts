import { useEffect } from 'react'
import { graphql, signedInAsSomebodyElse } from '../api'
import { newRandomId } from '../randomId'

const REPORT = `
  mutation ($isVisible: Boolean!, $idleSeconds: Int!, $reporterId: String, $conversationId: String) {
    ReportAgentPresence(isVisible: $isVisible, idleSeconds: $idleSeconds, reporterId: $reporterId, conversationId: $conversationId)
  }`

// This tab's name in its reports, so the server can tell it from the
// person's other tabs and their phone: one going out of sight hides only
// itself.
const REPORTER_ID = newRandomId('tab')

// How often a tab says it is still there. The server counts a person as
// present while a visible tab reported within two minutes, so one report
// may be missed.
const REPORT_EVERY_MS = 60_000

// The inputs that say the person is at the keyboard.
const INPUT_EVENTS = ['keydown', 'pointerdown', 'wheel', 'scroll', 'touchstart'] as const

// useAgentPresence tells the server whether the person has this tab in front
// of them, and how long since they last typed, clicked or scrolled in it:
// every minute, and at once when the tab is shown or hidden. The agent
// starts a conversation on its own only with somebody who is there to read
// it, and offers an idea only to somebody who has gone quiet; a message in a
// chat is sent to a phone unless somebody is looking at that chat.
// conversationId is the conversation the drawer shows, empty when it is
// closed or shows none: a report goes out at once when it changes, since
// moving to another chat is moving away from the first. Nothing is sent
// while there is neither an agent nor a chat to tell.
export function useAgentPresence(isAvailable: boolean, conversationId: string) {
  useEffect(() => {
    if (!isAvailable) return
    let lastInputAt = Date.now()
    const onInput = () => {
      lastInputAt = Date.now()
    }
    const report = () => {
      // Not while an operator is signed in as the person: they are not
      // here, and the agent would start a conversation with nobody.
      if (signedInAsSomebodyElse()) return
      const idleSeconds = Math.max(0, Math.round((Date.now() - lastInputAt) / 1000))
      void graphql(REPORT, {
        isVisible: document.visibilityState === 'visible',
        idleSeconds,
        reporterId: REPORTER_ID,
        conversationId,
      }).catch(() => undefined)
    }
    for (const name of INPUT_EVENTS) window.addEventListener(name, onInput, { passive: true, capture: true })
    document.addEventListener('visibilitychange', report)
    report()
    const timer = window.setInterval(report, REPORT_EVERY_MS)
    return () => {
      for (const name of INPUT_EVENTS) window.removeEventListener(name, onInput, { capture: true })
      document.removeEventListener('visibilitychange', report)
      window.clearInterval(timer)
    }
  }, [isAvailable, conversationId])
}

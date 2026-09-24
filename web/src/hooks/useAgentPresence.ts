import { useEffect } from 'react'
import { graphql } from '../api'

const REPORT = `
  mutation ($isVisible: Boolean!, $idleSeconds: Int!) {
    ReportAgentPresence(isVisible: $isVisible, idleSeconds: $idleSeconds)
  }`

// How often a tab says it is still there. The server counts a person as
// present while a visible tab reported within two minutes, so one report
// may be missed.
const REPORT_EVERY_MS = 60_000

// The inputs that say the person is at the keyboard.
const INPUT_EVENTS = ['keydown', 'pointerdown', 'wheel', 'scroll', 'touchstart'] as const

// useAgentPresence tells the agent whether the person has this tab in front
// of them, and how long since they last typed, clicked or scrolled in it:
// every minute, and at once when the tab is shown or hidden. The agent
// starts a conversation on its own only with somebody who is there to read
// it, and gives a tip only to somebody who has gone quiet. Nothing is sent
// while there is no agent to tell.
export function useAgentPresence(isAvailable: boolean) {
  useEffect(() => {
    if (!isAvailable) return
    let lastInputAt = Date.now()
    const onInput = () => {
      lastInputAt = Date.now()
    }
    const report = () => {
      const idleSeconds = Math.max(0, Math.round((Date.now() - lastInputAt) / 1000))
      void graphql(REPORT, { isVisible: document.visibilityState === 'visible', idleSeconds }).catch(() => undefined)
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
  }, [isAvailable])
}

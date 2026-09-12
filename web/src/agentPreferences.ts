import { useEffect, useState } from 'react'

// What a person chose about how their agent's work is shown: whether the
// tool calls are drawn, and whether each turn says what it cost. Set once
// in the account menu, kept in the browser, read by the drawer. A change
// is announced on the window so that the drawer, which is a different
// component, follows it at once.

export interface AgentPreferences {
  showTools: boolean
  showUsage: boolean
}

const TOOLS_KEY = 'teanode.agent.showTools'
const USAGE_KEY = 'teanode.agent.showUsage'
const CHANGED = 'teanode:agent-preferences'

function remembered(key: string): string {
  try {
    return localStorage.getItem(key) ?? ''
  } catch {
    return ''
  }
}

function remember(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {
    // A browser that keeps nothing forgets, which is fine.
  }
}

export function readAgentPreferences(): AgentPreferences {
  return { showTools: remembered(TOOLS_KEY) !== '0', showUsage: remembered(USAGE_KEY) === '1' }
}

export function writeAgentPreferences(next: Partial<AgentPreferences>) {
  if (next.showTools !== undefined) remember(TOOLS_KEY, next.showTools ? '1' : '0')
  if (next.showUsage !== undefined) remember(USAGE_KEY, next.showUsage ? '1' : '0')
  window.dispatchEvent(new Event(CHANGED))
}

// Whether there is an agent to show anything for, told by the drawer once
// it knows, so that the menu offers the switches only where they mean
// something.
let agentAvailable = false
const AVAILABLE = 'teanode:agent-available'

export function announceAgentAvailable(available: boolean) {
  if (agentAvailable === available) return
  agentAvailable = available
  window.dispatchEvent(new Event(AVAILABLE))
}

export function useAgentPreferences(): [AgentPreferences, (next: Partial<AgentPreferences>) => void, boolean] {
  const [preferences, setPreferences] = useState(readAgentPreferences)
  const [available, setAvailable] = useState(agentAvailable)
  useEffect(() => {
    const changed = () => setPreferences(readAgentPreferences())
    const availability = () => setAvailable(agentAvailable)
    window.addEventListener(CHANGED, changed)
    window.addEventListener(AVAILABLE, availability)
    return () => {
      window.removeEventListener(CHANGED, changed)
      window.removeEventListener(AVAILABLE, availability)
    }
  }, [])
  return [preferences, writeAgentPreferences, available]
}

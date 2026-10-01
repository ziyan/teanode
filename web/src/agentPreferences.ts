import { useEffect, useState } from 'react'

// What a person chose about how their agent's work is shown: whether the
// tool calls are drawn, and whether each turn says what it cost. Switched
// in the drawer's usage dropdown, kept in the browser, read by the drawer.
// A change is announced on the window so that every drawer open, framed or
// not, follows it at once.

export interface AgentPreferences {
  showTools: boolean
  showUsage: boolean
  // The agent's working notes: why it looked harder, an approval given
  // after its turn, the turns it started on its own. Useful for seeing
  // what it did; off unless asked for.
  showWorkingNotes: boolean
}

const TOOLS_KEY = 'teanode.agent.showTools'
const USAGE_KEY = 'teanode.agent.showUsage'
const NOTES_KEY = 'teanode.agent.showWorkingNotes'
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
  return {
    showTools: remembered(TOOLS_KEY) !== '0',
    showUsage: remembered(USAGE_KEY) === '1',
    showWorkingNotes: remembered(NOTES_KEY) === '1',
  }
}

export function writeAgentPreferences(next: Partial<AgentPreferences>) {
  if (next.showTools !== undefined) remember(TOOLS_KEY, next.showTools ? '1' : '0')
  if (next.showUsage !== undefined) remember(USAGE_KEY, next.showUsage ? '1' : '0')
  if (next.showWorkingNotes !== undefined) remember(NOTES_KEY, next.showWorkingNotes ? '1' : '0')
  window.dispatchEvent(new Event(CHANGED))
}

// Whether there is an agent to show anything for, told by the drawer once
// it knows, so that the menu offers the switches only where they mean
// something. The agent's name comes with it, for the rail's row that opens
// its conversations: the drawer has already asked, and asking again from
// the rail would be a second request for the same answer.
let agentAvailable = false
let agentName = ''
const AVAILABLE = 'teanode:agent-available'

export function announceAgentAvailable(available: boolean, name = '') {
  if (agentAvailable === available && agentName === name) return
  agentAvailable = available
  agentName = name
  window.dispatchEvent(new Event(AVAILABLE))
}

// useAgentIdentity is whether the person has an agent to talk to, and what
// it is called; the name is empty until the drawer has heard it, or when the
// agent has none.
export function useAgentIdentity(): { isAvailable: boolean; name: string } {
  const [identity, setIdentity] = useState(() => ({ isAvailable: agentAvailable, name: agentName }))
  useEffect(() => {
    const changed = () => setIdentity({ isAvailable: agentAvailable, name: agentName })
    window.addEventListener(AVAILABLE, changed)
    // Told before this mounted, between the first render and now.
    changed()
    return () => window.removeEventListener(AVAILABLE, changed)
  }, [])
  return identity
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

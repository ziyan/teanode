import { describe, expect, it } from 'vitest'
import { AGENT_QUIET_MILLISECONDS, AgentActivity, PERSON_HOLD_MILLISECONDS } from './agentActivity'

function memoryStorage() {
  const contents: Record<string, unknown> = {}
  return {
    contents,
    get: async (key: string) => (key in contents ? { [key]: structuredClone(contents[key]) } : {}),
    set: async (items: Record<string, unknown>) => void Object.assign(contents, structuredClone(items)),
  }
}

const START = 1_000_000

describe('AgentActivity', () => {
  it('takes a tab as the agent’s from its command until a minute of quiet', async () => {
    const activity = new AgentActivity(() => memoryStorage())
    expect(await activity.isAgentDriving(5, START)).toBe(false)
    activity.noteAgentCommand(5, START)
    expect(await activity.isAgentDriving(5, START + 1)).toBe(true)
    expect(await activity.isAgentDriving(5, START + AGENT_QUIET_MILLISECONDS - 1)).toBe(true)
    expect(await activity.isAgentDriving(5, START + AGENT_QUIET_MILLISECONDS)).toBe(false)
    // Another tab is not touched.
    expect(await activity.isAgentDriving(6, START + 1)).toBe(false)
  })

  it('lets the person hold the tab with input newer than the agent’s last command, for a few seconds', async () => {
    const activity = new AgentActivity(() => memoryStorage())
    activity.noteAgentCommand(5, START)
    activity.notePersonInput(5, START + 2000)
    expect(await activity.isAgentDriving(5, START + 2001)).toBe(false)
    expect(await activity.isAgentDriving(5, START + 2000 + PERSON_HOLD_MILLISECONDS - 1)).toBe(false)
    expect(await activity.isAgentDriving(5, START + 2000 + PERSON_HOLD_MILLISECONDS)).toBe(true)
    // A command after the person's input takes the tab back at once.
    activity.noteAgentCommand(5, START + 3000)
    expect(await activity.isAgentDriving(5, START + 3001)).toBe(true)
    // Input older than the command, or at the same moment, is not a hold.
    activity.notePersonInput(5, START + 2500)
    expect(await activity.isAgentDriving(5, START + 3002)).toBe(true)
  })

  it('forgets a tab that is gone', async () => {
    const activity = new AgentActivity(() => memoryStorage())
    activity.noteAgentCommand(5, Date.now())
    activity.forget(5)
    expect(await activity.isAgentDriving(5)).toBe(false)
  })

  it('remembers across a worker that was stopped, from the session storage', async () => {
    const storage = memoryStorage()
    const first = new AgentActivity(() => storage)
    first.noteAgentCommand(5, START)
    first.notePersonInput(7, START)
    await new Promise((resolve) => setTimeout(resolve, 5))
    const second = new AgentActivity(() => storage)
    expect(await second.isAgentDriving(5, START + 1000)).toBe(true)
    expect(await second.isAgentDriving(7, START + 1000)).toBe(false)
  })

  it('works without a storage', async () => {
    const activity = new AgentActivity(() => undefined)
    activity.noteAgentCommand(5, START)
    expect(await activity.isAgentDriving(5, START + 1)).toBe(true)
  })
})

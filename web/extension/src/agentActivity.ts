// Which tabs the agent has just been working in, and which the person is
// holding from the dashboard.
//
// The agent's real input (the DevTools protocol's mouse events) is
// indistinguishable, to a page, from the person's: it can click a field, open
// Passwords' suggestion list in a frame of the page and click a row of it. The
// fill that follows is the person's own login, read back by the agent from the
// field. So the worker does not answer the page for Passwords (suggest, fill,
// fill a code, generate, save) in a tab the agent has been working in, unless
// the person is holding that tab.
//
// The agent's every command in a tab is noted here before it is done, so any
// input it sends is noted before the page can ask for a fill because of it.
// The person's input is what arrives from the dashboard while they hold a tab
// they took over; the server passes it on only then.

// AGENT_QUIET_MILLISECONDS is how long after its last command in a tab the
// agent is taken to be still working there.
export const AGENT_QUIET_MILLISECONDS = 60 * 1000
// PERSON_HOLD_MILLISECONDS is how long the person's last input keeps the tab
// theirs. Pointer moves arrive all the while they use it.
export const PERSON_HOLD_MILLISECONDS = 10 * 1000

export const AGENT_ACTIVITY_KEY = 'agentActivity'

interface StoredActivity {
  agentCommandAt: Record<string, number>
  personInputAt: Record<string, number>
}

interface StorageArea {
  get(key: string): Promise<Record<string, unknown>>
  set(items: Record<string, unknown>): Promise<void>
}

export class AgentActivity {
  private readonly agentCommandAt = new Map<number, number>()
  private readonly personInputAt = new Map<number, number>()
  private readonly restored: Promise<void>

  // The times are kept in the browser session's storage as well, because the
  // worker is stopped and started freely and a minute of quiet is longer than
  // it lives idle.
  constructor(private readonly area: () => StorageArea | undefined) {
    this.restored = this.restore()
  }

  private async restore(): Promise<void> {
    try {
      const stored = (await this.area()?.get(AGENT_ACTIVITY_KEY))?.[AGENT_ACTIVITY_KEY] as StoredActivity | undefined
      if (!stored) return
      for (const [tabId, at] of Object.entries(stored.agentCommandAt ?? {})) {
        if (!this.agentCommandAt.has(Number(tabId))) this.agentCommandAt.set(Number(tabId), at)
      }
      for (const [tabId, at] of Object.entries(stored.personInputAt ?? {})) {
        if (!this.personInputAt.has(Number(tabId))) this.personInputAt.set(Number(tabId), at)
      }
    } catch {
      // Nothing kept: the tabs are taken as quiet only for what is noted from now.
    }
  }

  private async save(now: number): Promise<void> {
    // Times older than anything that matters are not kept.
    for (const [times, quiet] of [
      [this.agentCommandAt, AGENT_QUIET_MILLISECONDS],
      [this.personInputAt, PERSON_HOLD_MILLISECONDS],
    ] as const) {
      for (const [tabId, at] of times) if (now - at > quiet) times.delete(tabId)
    }
    const stored: StoredActivity = {
      agentCommandAt: Object.fromEntries(this.agentCommandAt),
      personInputAt: Object.fromEntries(this.personInputAt),
    }
    try {
      await this.area()?.set({ [AGENT_ACTIVITY_KEY]: stored })
    } catch {
      // Kept in memory all the same.
    }
  }

  // noteAgentCommand is called before the agent's command is done in the tab.
  noteAgentCommand(tabId: number, now = Date.now()): void {
    this.agentCommandAt.set(tabId, now)
    void this.save(now)
  }

  // notePersonInput is called when the person's input arrives for the tab.
  notePersonInput(tabId: number, now = Date.now()): void {
    this.personInputAt.set(tabId, now)
    void this.save(now)
  }

  forget(tabId: number): void {
    this.agentCommandAt.delete(tabId)
    this.personInputAt.delete(tabId)
    void this.save(Date.now())
  }

  // isAgentDriving says the agent has worked in the tab within the last
  // minute and the person has not held it since: what the page asks of
  // Passwords may then be the agent's doing.
  async isAgentDriving(tabId: number, now = Date.now()): Promise<boolean> {
    await this.restored
    const commandAt = this.agentCommandAt.get(tabId)
    if (commandAt === undefined || now - commandAt >= AGENT_QUIET_MILLISECONDS) return false
    const inputAt = this.personInputAt.get(tabId)
    // The person holds the tab when their input is the newer and recent.
    return !(inputAt !== undefined && inputAt > commandAt && now - inputAt < PERSON_HOLD_MILLISECONDS)
  }
}

// agentActivity is the one record the worker's parts share.
export const agentActivity = new AgentActivity(() =>
  typeof chrome === 'undefined' ? undefined : chrome.storage?.session,
)

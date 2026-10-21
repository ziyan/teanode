// The shape of the answer the agent's actions give from inside a page.
export interface InPageAnswer {
  ok: boolean
  data?: any
  error?: string
}

export function inPage(action: string, args: Record<string, unknown>): Promise<InPageAnswer>

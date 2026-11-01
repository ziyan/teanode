// Small helpers the extension's own pages share: building elements without
// ever putting text into innerHTML (names and user names come from the
// person's items), asking the service worker, and telling a person's click
// from a script's.

type Child = Node | string | null | undefined | false

export function element<Tag extends keyof HTMLElementTagNameMap>(
  tag: Tag,
  properties: Partial<Omit<HTMLElementTagNameMap[Tag], 'style' | 'children'>> & { className?: string } = {},
  ...children: Child[]
): HTMLElementTagNameMap[Tag] {
  const made = document.createElement(tag)
  Object.assign(made, properties)
  for (const child of children) {
    if (child === null || child === undefined || child === false) continue
    made.append(child)
  }
  return made
}

export function clear(parent: Element): void {
  while (parent.firstChild) parent.firstChild.remove()
}

// ask sends a message to the service worker. A refusal comes back as an error
// the page shows; the code says why (for example "locked").
export class AskError extends Error {
  constructor(
    message: string,
    readonly errorCode: string | undefined,
  ) {
    super(message)
  }
}

export async function ask<Answer>(message: Record<string, unknown>): Promise<Answer> {
  const answer = await chrome.runtime.sendMessage(message)
  if (answer && typeof answer === 'object' && typeof answer.error === 'string') {
    throw new AskError(answer.error, typeof answer.errorCode === 'string' ? answer.errorCode : undefined)
  }
  return answer as Answer
}

export function errorText(error: unknown): string {
  return error instanceof Error && error.message ? error.message : 'Something went wrong.'
}

// trust is how a click is told from a script's. It is an object only so a test,
// which has no way to make a trusted event, can stand in for the browser.
export const trust = { isTrusted: (event: Event): boolean => event.isTrusted }

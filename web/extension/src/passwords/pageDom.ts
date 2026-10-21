// Small helpers the extension's own pages share: building elements without
// ever putting text into innerHTML (names and user names come from the
// person's items), asking the service worker, and the trusted-click guard for
// pages that live in a frame over a website.

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

// watchFrameVisibility uses the browser's own visibility tracking (the
// intersection observer's trackVisibility), which reports a frame that is
// see-through, filtered, or covered by another element, whatever the page
// around it did to get there. A click in a frame is acted on only when the
// frame has been fully visible for a moment before it. A browser that does
// not report visibility never passes.
export function watchFrameVisibility(target: Element, minimumVisibleMilliseconds = 400): () => boolean {
  let visibleSince = 0
  try {
    const observer = new IntersectionObserver(
      (entries) => {
        const entry = entries[entries.length - 1]
        const isVisible = (entry as IntersectionObserverEntry & { isVisible?: boolean }).isVisible === true
        if (isVisible && entry.intersectionRatio >= 0.98) {
          if (visibleSince === 0) visibleSince = performance.now()
        } else {
          visibleSince = 0
        }
      },
      { threshold: [0, 0.5, 0.98, 1], trackVisibility: true, delay: 100 } as IntersectionObserverInit,
    )
    observer.observe(target)
  } catch {
    return () => false
  }
  return () => visibleSince !== 0 && performance.now() - visibleSince >= minimumVisibleMilliseconds
}

// trust is how a click is told from a script's. It is an object only so a test,
// which has no way to make a trusted event, can stand in for the browser.
export const trust = { isTrusted: (event: Event): boolean => event.isTrusted }

// onTrustedActivate runs the handler for a click or the Enter and Space keys
// on a button, if a person did it (a script's click is not trusted) and the
// frame has been visible for long enough.
export function onTrustedActivate(button: HTMLElement, isAllowed: () => boolean, handler: () => void): void {
  button.addEventListener('click', (event) => {
    if (!trust.isTrusted(event) || !isAllowed()) return
    handler()
  })
}

import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { TranslationProvider } from '../i18n/i18n'
import { ChatLineFrame, type ChatLine } from './agentChat'

afterEach(cleanup)

function line(overrides: Partial<ChatLine> = {}): ChatLine {
  return {
    kind: 'chat',
    key: 'message-one',
    messageId: 'message-one',
    text: 'Fixture words',
    authorKind: 'person',
    authorUserId: 'person-sam',
    authorName: 'Sam Fixture',
    authorOwnerName: '',
    authorAvatarUrl: null,
    authorAvatarColorIndex: 2,
    authorAgentId: '',
    isMine: false,
    editedAt: null,
    isDeleted: false,
    ...overrides,
  }
}

function draw(chatLine: ChatLine, onLeaving?: () => void) {
  return render(
    <TranslationProvider>
      <MemoryRouter>
        <ChatLineFrame line={chatLine} isRunStart readers={[]} onLeaving={onLeaving}>
          <div>{chatLine.text}</div>
        </ChatLineFrame>
      </MemoryRouter>
    </TranslationProvider>,
  )
}

it('opens the card of somebody else from their name and their picture, and tells the drawer it is leaving', () => {
  const onLeaving = vi.fn()
  const { container } = draw(line(), onLeaving)
  const name = screen.getByRole('link', { name: 'Sam Fixture' })
  expect(name.getAttribute('href')).toBe('/people/person-sam')
  // The picture is the same link, kept out of the tab order and the reader's.
  const links = container.querySelectorAll('a')
  expect(links).toHaveLength(2)
  expect(links[0].getAttribute('href')).toBe('/people/person-sam')
  expect(links[0].getAttribute('tabindex')).toBe('-1')
  fireEvent.click(name)
  expect(onLeaving).toHaveBeenCalledTimes(1)
})

it("draws no link for the signed-in person's own words, an agent, or an account that is gone", () => {
  draw(line({ isMine: true, authorUserId: 'person-a' }))
  expect(screen.queryByRole('link')).toBeNull()
  cleanup()

  draw(line({ authorKind: 'agent', authorUserId: '', authorAgentId: 'agent-one', authorName: 'Fixture agent' }))
  expect(screen.queryByRole('link')).toBeNull()
  cleanup()

  draw(line({ authorUserId: '' }))
  expect(screen.queryByRole('link')).toBeNull()
})

import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { IdeaSuggestions } from './ideaRow'

vi.mock('../api', () => ({ graphql: vi.fn(), openAgentConversation: vi.fn(() => true) }))
vi.mock('../i18n/i18n', () => ({ useTranslation: () => ({ t: (key: string) => key, language: 'en' }) }))
vi.mock('./toast', () => ({ useToast: () => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }) }))
const execute = vi.mocked(graphql)
afterEach(cleanup)

it('starts a suggested idea in the conversation and hands its request on to be sent', async () => {
  execute.mockImplementation(async (document: string) => {
    if (document.includes('StartAgentIdea'))
      return { StartAgentIdea: { conversation: { id: 'empty-conversation' }, openingRequest: 'Sort my receipts.' } }
    return {
      ListAgentIdeas: {
        ideas: [
          {
            id: 'receipts',
            ideaKind: 'catalog',
            ideaCategory: 'money',
            emoji: '🧾',
            headline: 'Sort the receipts',
            body: 'Forward them and I file them by month.',
            openingRequest: 'Sort my receipts.',
            suggestionReason: '',
            evidence: [],
            ideaStatus: 'open',
            startedConversationId: '',
            createdAt: '2030-01-01T00:00:00Z',
            shownAt: '2030-01-01T00:00:00Z',
            startedAt: null,
            closedAt: null,
          },
        ],
        ideaCategories: [],
      },
    }
  })
  const onStarted = vi.fn()
  render(
    <MemoryRouter>
      <IdeaSuggestions conversationId="empty-conversation" onStarted={onStarted} />
    </MemoryRouter>,
  )
  fireEvent.click(await screen.findByRole('button', { name: /Sort the receipts/ }))
  await waitFor(() => expect(onStarted).toHaveBeenCalledWith('empty-conversation', 'Sort my receipts.'))
  expect(execute).toHaveBeenCalledWith(expect.stringContaining('StartAgentIdea'), {
    ideaId: 'receipts',
    conversationId: 'empty-conversation',
    language: 'en',
  })
})

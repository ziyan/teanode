import { fireEvent, render, screen } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { APIError, graphql } from '../api'
import { TranslationProvider } from '../i18n/i18n'
import { QuestionCard, cardsOf, citationsIn, readConversationSnapshot, sharedFilesOf, toolWords } from './agentDrawer'
import { hasSharedCitation, linkSharedCitations } from './dashboardPath'

// The drawer asks the window about the screen when it loads, which jsdom
// does not answer.
vi.hoisted(() => {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: () => ({
      matches: false,
      addEventListener() {},
      removeEventListener() {},
      addListener() {},
      removeListener() {},
    }),
  })
})
vi.mock('../api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../api')>()),
  graphql: vi.fn(),
}))

const snapshot = (conversationId: string) => ({
  ReadAgentConversation: { conversation: { id: conversationId }, messages: [], todos: [] },
  ListAgentInteractions: [],
})

// The drawer reopens the conversation this browser showed last, which may
// be another account's or deleted: it opens the main one instead, quietly.
describe('readConversationSnapshot', () => {
  beforeEach(() => vi.mocked(graphql).mockReset())

  it('opens the main conversation when the remembered one is not there', async () => {
    vi.mocked(graphql)
      .mockRejectedValueOnce(new APIError('api: not found'))
      .mockResolvedValueOnce(snapshot('main-conversation'))
    const read = await readConversationSnapshot('someone-elses', new AbortController().signal)
    expect(read.conversation.id).toBe('main-conversation')
    expect(vi.mocked(graphql).mock.calls[1][1]).toMatchObject({ conversationId: undefined })
  })

  it('still reports any other failure', async () => {
    vi.mocked(graphql).mockRejectedValueOnce(new APIError('api: forbidden'))
    await expect(readConversationSnapshot('a-conversation', new AbortController().signal)).rejects.toThrow('forbidden')
    expect(graphql).toHaveBeenCalledTimes(1)
  })

  it('does not retry when the main conversation itself is missing', async () => {
    vi.mocked(graphql).mockRejectedValueOnce(new APIError('api: not found'))
    await expect(readConversationSnapshot('', new AbortController().signal)).rejects.toThrow('not found')
    expect(graphql).toHaveBeenCalledTimes(1)
  })
})

it('names a tool call in words, as the iPhone does', () => {
  expect(toolWords('tool_search')).toBe('Tool search')
  expect(toolWords('skill__weather__get_weather')).toBe('Get weather')
  expect(toolWords('mail-draft')).toBe('Mail draft')
})

describe('sharedFilesOf', () => {
  const file = (overrides: Record<string, unknown> = {}) => ({
    attachment_id: 'attachment-1',
    name: 'IMG_0001.jpg',
    content_type: 'image/jpeg',
    size: 12,
    url: '/api/v1/agent/attachments/attachment-1',
    ...overrides,
  })
  const answer = (...files: Record<string, unknown>[]) => JSON.stringify({ files })

  it('keeps the photo_id of a file the photo tool showed', () => {
    const files = sharedFilesOf({ tool: 'photo', result: answer(file({ photo_id: 'photo-1_A' })) })
    expect(files.map((entry) => entry.photo_id)).toEqual(['photo-1_A'])
  })

  it('drops a photo_id that is not shaped like an id of ours', () => {
    for (const photoId of ['../settings', 'a/b', 'a?b', '', 'x'.repeat(65), 7]) {
      const files = sharedFilesOf({ tool: 'photo', result: answer(file({ photo_id: photoId })) })
      expect(files).toHaveLength(1)
      expect(files[0].photo_id).toBeUndefined()
    }
  })

  it('reads photo_id only from the photo tool', () => {
    const files = sharedFilesOf({ tool: 'skill__news__headlines', result: answer(file({ photo_id: 'photo-1' })) })
    expect(files).toHaveLength(1)
    expect(files[0].photo_id).toBeUndefined()
  })
})

it('does not look a shared page’s fact up among the person’s own pages', () => {
  expect(citationsIn('It is on people/a-grandparent#3 (shared by Ada), and on work/plan#7.')).toEqual(['work/plan#7'])
  expect(citationsIn('people/a-grandparent#3 and self#4')).toEqual(['people/a-grandparent#3', 'self#4'])
  expect(citationsIn('people/a-grandparent#3 (shared by Ada Example)')).toEqual([])
})

it('links a shared citation to its owner’s page when the owner is known, and otherwise leaves the words', () => {
  const written = 'It is on people/a-grandparent#3 (shared by Ada Example), at three.'
  expect(hasSharedCitation(written)).toBe(true)
  expect(hasSharedCitation('It is on people/a-grandparent#3.')).toBe(false)
  const owners: Record<string, string> = { 'ada example': 'ada' }
  const usernameOf = (name: string) => owners[name.toLowerCase()]
  expect(linkSharedCitations(written, usernameOf)).toBe(
    'It is on [people/a-grandparent#3 (shared by Ada Example)](memory:people/a-grandparent#3@ada), at three.',
  )
  // An owner not known, a citation already a link, and a name with a bracket.
  expect(linkSharedCitations(written, () => undefined)).toBe(written)
  const linked = '[people/a-grandparent#3 (shared by Ada Example)](memory:people/a-grandparent#3@ada)'
  expect(linkSharedCitations(linked, usernameOf)).toBe(linked)
  expect(linkSharedCitations('people/a#1 (shared by A]da)', () => 'ada')).toBe('people/a#1 (shared by A]da)')
})

// A request to take the browser tab over is drawn as a question card, with
// a button that opens the Browser page; it can still be answered in words.
it('draws a kept take-over request as a card that opens the Browser page', () => {
  const [line] = cardsOf([
    {
      runId: 'run-1',
      callId: 'call-1',
      interactionKind: 'take_over',
      toolName: 'browser',
      interactionText: 'Sign in to the bank with the code sent to your phone',
      interactionChoices: null as unknown as string[],
      toolRisk: '',
      createdAt: '2026-10-20T12:00:00Z',
    },
  ])
  expect(line).toMatchObject({ kind: 'question', isTakeOver: true, choices: [], key: 'run-1-call-1-card' })
  if (line.kind !== 'question') throw new Error('not a question card')
  const onTakeOver = vi.fn()
  render(
    <TranslationProvider>
      <QuestionCard line={line} onAnswer={() => {}} onChat={() => {}} onTakeOver={onTakeOver} />
    </TranslationProvider>,
  )
  expect(screen.getByText('Sign in to the bank with the code sent to your phone')).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'Take over the tab' }))
  expect(onTakeOver).toHaveBeenCalledTimes(1)
  expect(screen.getByRole('button', { name: 'Chat about it' })).toBeTruthy()
})

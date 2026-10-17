import { beforeEach, describe, expect, it, vi } from 'vitest'

import { APIError, graphql } from '../api'
import { readConversationSnapshot, sharedFilesOf, toolWords } from './agentDrawer'

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

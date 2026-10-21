import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { forgetLinkPreviews, requestLinkPreview, type LinkPreview } from './linkPreviews'

vi.mock('../api', () => ({ graphql: vi.fn() }))

function previewOf(url: string): LinkPreview {
  return {
    url,
    cleanedUrl: url,
    displayText: url,
    isAvailable: true,
    title: `Title of ${url}`,
    description: '',
    siteName: '',
    imageUrl: null,
    imageWidthPixels: null,
    imageHeightPixels: null,
  }
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.mocked(graphql).mockImplementation((async (_document: string, variables: { urls: string[] }) => ({
    ReadLinkPreviews: variables.urls.map(previewOf),
  })) as never)
})
afterEach(() => {
  forgetLinkPreviews()
  vi.useRealTimers()
})

it('asks for the links of a screenful in one question, ten at a time', async () => {
  const addresses = Array.from({ length: 12 }, (_, index) => `https://example.com/page-${index}`)
  const answers = addresses.map((address) => requestLinkPreview('conversation-1', address))
  await vi.advanceTimersByTimeAsync(500)
  const previews = await Promise.all(answers)
  expect(vi.mocked(graphql)).toHaveBeenCalledTimes(2)
  expect((vi.mocked(graphql).mock.calls[0][1] as { urls: string[] }).urls).toEqual(addresses.slice(0, 10))
  expect((vi.mocked(graphql).mock.calls[1][1] as { urls: string[] }).urls).toEqual(addresses.slice(10))
  expect(previews.map((preview) => preview?.url)).toEqual(addresses)
})

it('does not ask again for a link it has asked for', async () => {
  const first = requestLinkPreview('conversation-1', 'https://example.com/once')
  await vi.advanceTimersByTimeAsync(200)
  await first
  const again = requestLinkPreview('conversation-1', 'https://example.com/once')
  await vi.advanceTimersByTimeAsync(200)
  expect((await again)?.title).toBe('Title of https://example.com/once')
  expect(vi.mocked(graphql)).toHaveBeenCalledTimes(1)
})

it('has no preview when the question fails, and asks again after a while', async () => {
  vi.mocked(graphql).mockRejectedValueOnce(new Error('server down'))
  const failed = requestLinkPreview('conversation-1', 'https://example.com/later')
  await vi.advanceTimersByTimeAsync(200)
  expect(await failed).toBeNull()
  const soon = requestLinkPreview('conversation-1', 'https://example.com/later')
  expect(await soon).toBeNull()
  expect(vi.mocked(graphql)).toHaveBeenCalledTimes(1)

  await vi.advanceTimersByTimeAsync(61_000)
  const later = requestLinkPreview('conversation-1', 'https://example.com/later')
  await vi.advanceTimersByTimeAsync(200)
  expect((await later)?.isAvailable).toBe(true)
  expect(vi.mocked(graphql)).toHaveBeenCalledTimes(2)
})

it('asks one question for each conversation, with the links as they stand in the messages', async () => {
  const tracked = 'https://example.com/page?gclid=abc&color=blue'
  const answers = [
    requestLinkPreview('conversation-1', tracked),
    requestLinkPreview('conversation-2', 'https://example.com/other'),
    requestLinkPreview('conversation-1', 'https://example.com/second'),
  ]
  await vi.advanceTimersByTimeAsync(500)
  await Promise.all(answers)
  const asked = vi
    .mocked(graphql)
    .mock.calls.map(([, variables]) => variables as { conversationId: string; urls: string[] })
  expect(asked).toEqual([
    { conversationId: 'conversation-1', urls: [tracked, 'https://example.com/second'] },
    { conversationId: 'conversation-2', urls: ['https://example.com/other'] },
  ])
})

it('asks again for a link in another conversation', async () => {
  const first = requestLinkPreview('conversation-1', 'https://example.com/shared')
  await vi.advanceTimersByTimeAsync(200)
  await first
  const second = requestLinkPreview('conversation-2', 'https://example.com/shared')
  await vi.advanceTimersByTimeAsync(200)
  await second
  expect(vi.mocked(graphql)).toHaveBeenCalledTimes(2)
})

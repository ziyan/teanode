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
  const answers = addresses.map((address) => requestLinkPreview(address))
  await vi.advanceTimersByTimeAsync(500)
  const previews = await Promise.all(answers)
  expect(vi.mocked(graphql)).toHaveBeenCalledTimes(2)
  expect((vi.mocked(graphql).mock.calls[0][1] as { urls: string[] }).urls).toEqual(addresses.slice(0, 10))
  expect((vi.mocked(graphql).mock.calls[1][1] as { urls: string[] }).urls).toEqual(addresses.slice(10))
  expect(previews.map((preview) => preview?.url)).toEqual(addresses)
})

it('does not ask again for a link it has asked for', async () => {
  const first = requestLinkPreview('https://example.com/once')
  await vi.advanceTimersByTimeAsync(200)
  await first
  const again = requestLinkPreview('https://example.com/once')
  await vi.advanceTimersByTimeAsync(200)
  expect((await again)?.title).toBe('Title of https://example.com/once')
  expect(vi.mocked(graphql)).toHaveBeenCalledTimes(1)
})

it('has no preview when the question fails, and asks again after a while', async () => {
  vi.mocked(graphql).mockRejectedValueOnce(new Error('server down'))
  const failed = requestLinkPreview('https://example.com/later')
  await vi.advanceTimersByTimeAsync(200)
  expect(await failed).toBeNull()
  const soon = requestLinkPreview('https://example.com/later')
  expect(await soon).toBeNull()
  expect(vi.mocked(graphql)).toHaveBeenCalledTimes(1)

  await vi.advanceTimersByTimeAsync(61_000)
  const later = requestLinkPreview('https://example.com/later')
  await vi.advanceTimersByTimeAsync(200)
  expect((await later)?.isAvailable).toBe(true)
  expect(vi.mocked(graphql)).toHaveBeenCalledTimes(2)
})

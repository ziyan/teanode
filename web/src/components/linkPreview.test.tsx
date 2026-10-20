import { cleanup, render, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../api'
import { TranslationProvider } from '../i18n/i18n'
import { forgetLinkPreviews, type LinkPreview } from '../lib/linkPreviews'
import { LinkPreviewCard } from './linkPreview'

vi.mock('../api', () => ({ graphql: vi.fn() }))

const AVAILABLE: LinkPreview = {
  url: 'https://www.example.com/shop/chair/?gclid=abc&color=blue',
  cleanedUrl: 'https://www.example.com/shop/chair/?color=blue',
  displayText: 'example.com/shop/chair/?color=blue',
  isAvailable: true,
  title: 'Garden Chair',
  description: 'A chair for the garden.',
  siteName: 'Example Shop',
  imageUrl: '/api/v1/link-preview/image/link-preview-1',
  imageWidthPixels: 600,
  imageHeightPixels: 400,
}

function answerWith(preview: LinkPreview) {
  vi.mocked(graphql).mockResolvedValue({ ReadLinkPreviews: [preview] } as never)
}

function renderCard(text: string, isMarkdown = false, isOwn = false) {
  return render(
    <TranslationProvider>
      <LinkPreviewCard text={text} isMarkdown={isMarkdown} isOwn={isOwn} />
    </TranslationProvider>,
  )
}

beforeEach(() => {
  vi.mocked(graphql).mockReset()
})
afterEach(() => {
  cleanup()
  forgetLinkPreviews()
})

it('draws the page of the first link as one card that opens the clean link', async () => {
  answerWith(AVAILABLE)
  const { container } = renderCard(
    'see https://www.example.com/shop/chair/?gclid=abc&color=blue and https://example.org/other',
  )
  await waitFor(() => expect(container.querySelector('a.link-card')).not.toBeNull())
  const card = container.querySelector('a.link-card') as HTMLAnchorElement
  expect(card.getAttribute('href')).toBe('https://www.example.com/shop/chair/?color=blue')
  expect(card.getAttribute('rel')).toBe('noopener noreferrer')
  expect(card.getAttribute('aria-label')).toBe('Open Garden Chair')
  expect(card.querySelector('img')?.getAttribute('src')).toBe('/api/v1/link-preview/image/link-preview-1')
  expect(card.querySelector('img')?.getAttribute('width')).toBe('600')
  expect(card.querySelector('img')?.getAttribute('height')).toBe('400')
  expect(card.querySelector('.link-card-site')?.textContent).toBe('Example Shop')
  expect(card.querySelector('strong')?.textContent).toBe('Garden Chair')
  expect(card.querySelector('.link-card-description')?.textContent).toBe('A chair for the garden.')
  expect(container.querySelectorAll('a.link-card')).toHaveLength(1)
  expect(vi.mocked(graphql).mock.calls[0][1]).toEqual({
    urls: ['https://www.example.com/shop/chair/?gclid=abc&color=blue'],
  })
})

it('draws nothing while the preview is on its way', () => {
  vi.mocked(graphql).mockReturnValue(new Promise(() => undefined) as never)
  const { container } = renderCard('https://example.com/slow')
  expect(container.querySelector('a.link-card')).toBeNull()
})

it('draws nothing for a page with no preview', async () => {
  answerWith({ ...AVAILABLE, isAvailable: false, title: '', description: '', siteName: '', imageUrl: null })
  const { container } = renderCard('https://example.com/nothing')
  await waitFor(() => expect(vi.mocked(graphql)).toHaveBeenCalled())
  await Promise.resolve()
  expect(container.querySelector('a.link-card')).toBeNull()
})

it('draws no picture for a page that has none, and keeps its words', async () => {
  answerWith({ ...AVAILABLE, imageUrl: null, imageWidthPixels: null, imageHeightPixels: null, description: '' })
  const { container } = renderCard('https://example.com/plain')
  await waitFor(() => expect(container.querySelector('a.link-card')).not.toBeNull())
  expect(container.querySelector('img')).toBeNull()
  expect(container.querySelector('.link-card-description')).toBeNull()
  expect(container.querySelector('strong')?.textContent).toBe('Garden Chair')
})

it('asks for nothing when there is no link, or only one inside code', () => {
  const { container } = renderCard('no link here')
  expect(container.firstChild).toBeNull()
  const code = renderCard('Run `curl https://example.com/x` first', true)
  expect(code.container.firstChild).toBeNull()
  expect(vi.mocked(graphql)).not.toHaveBeenCalled()
})

it('refuses a card whose address is not a web address', async () => {
  answerWith({ ...AVAILABLE, cleanedUrl: 'javascript:alert(1)' })
  const { container } = renderCard('https://example.com/odd')
  await waitFor(() => expect(vi.mocked(graphql)).toHaveBeenCalled())
  await Promise.resolve()
  expect(container.querySelector('a.link-card')).toBeNull()
})

it('sits on the right under the signed-in person’s own message', async () => {
  answerWith(AVAILABLE)
  const { container } = renderCard('https://example.com/mine', false, true)
  await waitFor(() => expect(container.querySelector('a.link-card.is-own')).not.toBeNull())
})

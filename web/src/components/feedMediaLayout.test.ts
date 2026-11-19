import { afterEach, expect, it, vi } from 'vitest'

import {
  feedHeaderLabelOf,
  isMotionReduced,
  mediaArrangementOf,
  mediaAspectOf,
  splitFeedMedia,
  weatherGradientOf,
  webAddressOf,
  type FeedMedia,
} from './feedMediaLayout'

function media(mediaKey: string, overrides: Partial<FeedMedia> = {}): FeedMedia {
  return {
    mediaKey,
    feedMediaKind: 'photo',
    imageUrl: `/image/${mediaKey}`,
    videoUrl: '',
    linkUrl: '',
    siteName: '',
    mediaCaption: '',
    widthPixels: null,
    heightPixels: null,
    listRowIndex: null,
    ...overrides,
  }
}

afterEach(() => vi.unstubAllGlobals())

it('splits the card media from the row thumbnails and keeps the first thumbnail of a row', () => {
  const split = splitFeedMedia([
    media('a'),
    media('b', { listRowIndex: 2 }),
    media('c', { listRowIndex: 2 }),
    media('d', { listRowIndex: 0 }),
    media('e'),
  ])
  expect(split.cardMedia.map((entry) => entry.mediaKey)).toEqual(['a', 'e'])
  expect([...split.rowMediaByIndex.keys()].sort()).toEqual([0, 2])
  expect(split.rowMediaByIndex.get(2)?.mediaKey).toBe('b')
  expect(splitFeedMedia(undefined).cardMedia).toEqual([])
})

it('arranges one item full width, two to four in a grid and more in a scrolling row', () => {
  expect([0, 1, 2, 4, 5, 6].map(mediaArrangementOf)).toEqual(['none', 'single', 'grid', 'grid', 'scroll', 'scroll'])
})

it('crops to 16:9, and to 4:5 only for a portrait photo', () => {
  expect(mediaAspectOf(media('a'))).toBe('wide')
  expect(mediaAspectOf(media('a', { widthPixels: 800, heightPixels: 1000 }))).toBe('portrait')
  expect(mediaAspectOf(media('a', { widthPixels: 1000, heightPixels: 800 }))).toBe('wide')
  expect(mediaAspectOf(media('a', { feedMediaKind: 'video', widthPixels: 800, heightPixels: 1000 }))).toBe('wide')
  expect(mediaAspectOf(media('a', { feedMediaKind: 'picture', widthPixels: 800, heightPixels: 1000 }))).toBe('wide')
})

it('labels a card with the first picture site name, else its source', () => {
  const pictures = [
    media('a', { feedMediaKind: 'picture' }),
    media('b', { feedMediaKind: 'picture', siteName: 'Example Daily' }),
  ]
  expect(feedHeaderLabelOf(pictures, 'Garden watch')).toBe('Example Daily')
  expect(feedHeaderLabelOf([media('a')], 'Garden watch')).toBe('Garden watch')
  expect(feedHeaderLabelOf([], '')).toBe('')
})

it('chooses the weather gradient from the symbol, then the words', () => {
  expect(weatherGradientOf('sun.max', 'Sunny')).toBe('clearDay')
  expect(weatherGradientOf('moon.stars', 'Clear')).toBe('clearNight')
  expect(weatherGradientOf('', 'Clear night')).toBe('clearNight')
  expect(weatherGradientOf('cloud.sun', 'Partly cloudy')).toBe('cloudy')
  expect(weatherGradientOf('cloud.rain', 'Showers')).toBe('rain')
  expect(weatherGradientOf('', 'Light drizzle')).toBe('rain')
  expect(weatherGradientOf('cloud.snow', '')).toBe('snow')
  expect(weatherGradientOf('cloud.bolt.rain', 'Thunderstorms with rain')).toBe('storm')
  expect(weatherGradientOf('', '小雨')).toBe('rain')
  expect(weatherGradientOf('', '')).toBe('clearDay')
})

it('follows only http and https addresses', () => {
  expect(webAddressOf('https://example.com/a')).toBe('https://example.com/a')
  expect(webAddressOf('javascript:alert(1)')).toBe('')
  expect(webAddressOf('not an address')).toBe('')
})

it('reads the reduced motion preference, and assumes it where it cannot ask', () => {
  vi.stubGlobal('matchMedia', undefined)
  expect(isMotionReduced()).toBe(true)
  vi.stubGlobal('matchMedia', () => ({ matches: false }))
  expect(isMotionReduced()).toBe(false)
  vi.stubGlobal('matchMedia', () => ({ matches: true }))
  expect(isMotionReduced()).toBe(true)
})

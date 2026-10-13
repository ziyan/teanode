import { expect, it } from 'vitest'
import { passagePlaybackPath } from './processing'
it('aligns absolute recording passages with each owned original timeline', () => {
  expect(passagePlaybackPath('fixture/media', 602000, 605000, 600000)).toBe(
    '/api/v1/recordings/media/fixture%2Fmedia#t=2,5',
  )
  expect(passagePlaybackPath('fixture/media', 2000, 5000, 0)).toBe('/api/v1/recordings/media/fixture%2Fmedia#t=2,5')
})

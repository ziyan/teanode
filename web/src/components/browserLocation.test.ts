import { describe, expect, it } from 'vitest'
import { isAskedOfThisDrawer, readBrowserLocation, RECENTLY_SENT_MS } from './browserLocation'

function geolocationThat(answer: (success: PositionCallback, failure: PositionErrorCallback) => void): Geolocation {
  return {
    getCurrentPosition: (success: PositionCallback, failure?: PositionErrorCallback | null) =>
      answer(success, failure ?? (() => undefined)),
    watchPosition: () => 0,
    clearWatch: () => undefined,
  } as Geolocation
}

describe('readBrowserLocation', () => {
  it('says where the browser is', async () => {
    const geolocation = geolocationThat((success) =>
      success({
        coords: { latitude: 12.34, longitude: -56.78, accuracy: 35 } as GeolocationCoordinates,
        timestamp: Date.UTC(2026, 0, 2, 3, 4, 5),
      } as GeolocationPosition),
    )
    await expect(readBrowserLocation(geolocation, true)).resolves.toEqual({
      latitudeDegrees: 12.34,
      longitudeDegrees: -56.78,
      accuracyMeters: 35,
      measuredAt: '2026-01-02T03:04:05.000Z',
    })
  })

  it('says why when the person did not allow it', async () => {
    const geolocation = geolocationThat((_, failure) => failure({ code: 1 } as GeolocationPositionError))
    const answer = await readBrowserLocation(geolocation, true)
    expect(answer.latitudeDegrees).toBeUndefined()
    expect(answer.errorMessage).toMatch(/not allowed/)
  })

  it('does not ask on a page that is not secure, or a browser without geolocation', async () => {
    expect((await readBrowserLocation(undefined, true)).errorMessage).toMatch(/cannot share/)
    const geolocation = geolocationThat(() => {
      throw new Error('asked')
    })
    expect((await readBrowserLocation(geolocation, false)).errorMessage).toMatch(/https/)
  })
})

describe('isAskedOfThisDrawer', () => {
  const now = 1_000_000_000
  it('answers a turn that names it, and only that drawer answers', () => {
    const asked = { askedDrawerId: 'drawer-a', lastSentAt: now - 60_000, now, isVisible: true }
    expect(isAskedOfThisDrawer({ ...asked, drawerId: 'drawer-a', isVisible: false })).toBe(true)
    expect(isAskedOfThisDrawer({ ...asked, drawerId: 'drawer-b' })).toBe(false)
  })

  it('answers a turn no drawer sent only when the person sent from here lately and is looking', () => {
    const unnamed = { askedDrawerId: undefined, drawerId: 'drawer-a', now }
    expect(isAskedOfThisDrawer({ ...unnamed, lastSentAt: now - 60_000, isVisible: true })).toBe(true)
    expect(isAskedOfThisDrawer({ ...unnamed, lastSentAt: now - 60_000, isVisible: false })).toBe(false)
    expect(isAskedOfThisDrawer({ ...unnamed, lastSentAt: now - RECENTLY_SENT_MS - 1, isVisible: true })).toBe(false)
    expect(isAskedOfThisDrawer({ ...unnamed, lastSentAt: 0, isVisible: true })).toBe(false)
  })
})

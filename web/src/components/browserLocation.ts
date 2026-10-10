// A turn asks where the person is with the location tool, and the
// drawer they are chatting in answers with this browser's geolocation.
// What it says goes to the agent, so it is in English, as the tools'
// words are.

export interface BrowserLocationAnswer {
  latitudeDegrees?: number
  longitudeDegrees?: number
  accuracyMeters?: number
  measuredAt?: string
  errorMessage?: string
}

// How long the browser may take to find itself, and how old a fix it may
// give instead. The server waits a minute in all, which leaves room for
// the person to answer the browser's question the first time.
const LOCATION_TIMEOUT_MS = 30_000
const LOCATION_MAXIMUM_AGE_MS = 5 * 60_000

// readBrowserLocation asks the browser where it is. It never throws: a
// refusal is an answer too, and the turn is told why.
export function readBrowserLocation(
  geolocation: Geolocation | undefined = typeof navigator === 'undefined' ? undefined : navigator.geolocation,
  isSecure: boolean = typeof window === 'undefined' ? true : window.isSecureContext,
): Promise<BrowserLocationAnswer> {
  if (!isSecure) {
    return Promise.resolve({
      errorMessage: 'the dashboard is not served over https, and a browser shares its location only with a secure page',
    })
  }
  if (!geolocation) {
    return Promise.resolve({ errorMessage: 'this browser cannot share a location' })
  }
  return new Promise((resolve) => {
    geolocation.getCurrentPosition(
      (position) =>
        resolve({
          latitudeDegrees: position.coords.latitude,
          longitudeDegrees: position.coords.longitude,
          accuracyMeters: position.coords.accuracy,
          measuredAt: new Date(position.timestamp).toISOString(),
        }),
      (error) => resolve({ errorMessage: locationErrorMessage(error.code) }),
      { enableHighAccuracy: true, timeout: LOCATION_TIMEOUT_MS, maximumAge: LOCATION_MAXIMUM_AGE_MS },
    )
  })
}

// GeolocationPositionError's codes: 1 denied, 2 unavailable, 3 timeout.
export function locationErrorMessage(code: number): string {
  switch (code) {
    case 1:
      return 'the person has not allowed the dashboard to know their location; they can allow it in the browser’s settings for this site'
    case 3:
      return 'the browser took too long to find its location'
    default:
      return 'the browser could not find its location'
  }
}

// How long after the person last sent from a drawer it still counts as
// the one they are chatting in, for a turn no drawer sent: one the server
// began after they approved a card.
export const RECENTLY_SENT_MS = 30 * 60_000

// isAskedOfThisDrawer says whether this drawer is the one to answer a
// location call. A turn a drawer sent names it, and only that drawer
// answers: another left open on a desk would answer first with where the
// desk is. A turn no drawer sent is answered by the one the person sent
// from lately, while they are looking at it.
export function isAskedOfThisDrawer({
  askedDrawerId,
  drawerId,
  lastSentAt,
  now,
  isVisible,
}: {
  askedDrawerId: string | undefined
  drawerId: string
  lastSentAt: number
  now: number
  isVisible: boolean
}): boolean {
  if (askedDrawerId) return askedDrawerId === drawerId
  return isVisible && lastSentAt > 0 && now - lastSentAt < RECENTLY_SENT_MS
}

// newDrawerId names one open drawer, for the turns it sends.
export function newDrawerId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  return `drawer-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}

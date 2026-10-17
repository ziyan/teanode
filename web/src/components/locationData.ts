import type { Key } from '../i18n/i18n'

// What the server says about where people are and have been, and the words for
// it that the Location page and its dialogs share. The names are the server's
// (internal/api/v1api/apigraph/location_history.go and location_share.go).

export type LocationPoint = {
  latitudeDegrees: number
  longitudeDegrees: number
  accuracyMeters: number
  measuredAt: string
}

export type Place = {
  id: string
  placeName: string
  isNamedByPerson: boolean
  placeLabel: string
  latitudeDegrees: number
  longitudeDegrees: number
  radiusMeters: number
  osmName: string
  osmCategory: string
  addressText: string
  visitCount: number
  lastVisitAt: string | null
}

export type PlaceVisit = {
  id: string
  place: Place
  arrivedAt: string | null
  departedAt: string | null
  durationSeconds: number | null
}

export type LocationView = {
  userId: string
  username: string
  name: string
  avatarUrl: string | null
  avatarColorIndex: number
  // "owner" for the person signed in, else what they share: current or history.
  shareLevel: string
  latestPoint: LocationPoint | null
  openVisit: Pick<PlaceVisit, 'id' | 'place' | 'arrivedAt'> | null
}

export type FoundPlace = {
  placeName: string
  osmType: string
  osmId: number | null
  osmCategory: string
  addressText: string
  latitudeDegrees: number
  longitudeDegrees: number
  distanceMeters: number
  placeId: string | null
}

export const POINT_FIELDS = 'latitudeDegrees longitudeDegrees accuracyMeters measuredAt'
export const PLACE_FIELDS =
  'id placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmName osmCategory addressText visitCount lastVisitAt'
export const VISIT_FIELDS = `id arrivedAt departedAt durationSeconds place { ${PLACE_FIELDS} }`
export const PERSON_FIELDS = `userId username name avatarUrl avatarColorIndex shareLevel
  latestPoint { ${POINT_FIELDS} } openVisit { id arrivedAt place { ${PLACE_FIELDS} } }`

export const LABEL_KEYS: Record<string, Key> = {
  home: 'location.label.home',
  work: 'location.label.work',
  school: 'location.label.school',
  family: 'location.label.family',
  gym: 'location.label.gym',
}

// placeTitle is what a place is called on screen: the name it has, else the
// word of its label, else where it is, else that it has no name.
export function placeTitle(place: Place, t: (key: Key) => string): string {
  return (
    place.placeName.trim() ||
    (LABEL_KEYS[place.placeLabel] ? t(LABEL_KEYS[place.placeLabel]) : '') ||
    place.addressText.trim() ||
    t('location.unnamedPlace')
  )
}

// dayKeyOf is the calendar day, as the person's own clock counts it, that a
// moment falls on: 2030-03-05. The address and the day headings use it.
export function dayKeyOf(moment: string | Date): string {
  const date = typeof moment === 'string' ? new Date(moment) : moment
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${date.getFullYear()}-${month}-${day}`
}

// parseDayKey is the start of a day in the address, or null when the address
// holds anything else.
export function parseDayKey(value: string | null): Date | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value ?? '')
  if (!match) {
    return null
  }
  const date = new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]))
  return dayKeyOf(date) === value ? date : null
}

// dayRange is the moments a day starts and ends at, to ask the server about.
export function dayRange(dayStart: Date): { from: string; until: string } {
  const next = new Date(dayStart.getFullYear(), dayStart.getMonth(), dayStart.getDate() + 1)
  return { from: dayStart.toISOString(), until: next.toISOString() }
}

const EARTH_RADIUS_METERS = 6371000

// distanceMeters is how far apart two positions are on the ground, along the
// great circle between them.
export function distanceMeters(
  first: { latitudeDegrees: number; longitudeDegrees: number },
  second: { latitudeDegrees: number; longitudeDegrees: number },
): number {
  const toRadians = (degrees: number) => (degrees * Math.PI) / 180
  const latitudeDelta = toRadians(second.latitudeDegrees - first.latitudeDegrees)
  const longitudeDelta = toRadians(second.longitudeDegrees - first.longitudeDegrees)
  const haversine =
    Math.sin(latitudeDelta / 2) ** 2 +
    Math.cos(toRadians(first.latitudeDegrees)) *
      Math.cos(toRadians(second.latitudeDegrees)) *
      Math.sin(longitudeDelta / 2) ** 2
  return 2 * EARTH_RADIUS_METERS * Math.asin(Math.min(1, Math.sqrt(haversine)))
}

// nearestPlaceHolding is the place whose circle holds the position, the
// nearest when several do, or null when none does.
export function nearestPlaceHolding<
  Held extends { latitudeDegrees: number; longitudeDegrees: number; radiusMeters: number },
>(places: Held[], position: { latitudeDegrees: number; longitudeDegrees: number }): Held | null {
  let nearest: Held | null = null
  let nearestDistanceMeters = Infinity
  for (const place of places) {
    const placeDistanceMeters = distanceMeters(place, position)
    if (placeDistanceMeters <= place.radiusMeters && placeDistanceMeters < nearestDistanceMeters) {
      nearest = place
      nearestDistanceMeters = placeDistanceMeters
    }
  }
  return nearest
}

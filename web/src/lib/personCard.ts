import type { CircleKind, CircleMember } from '../pages/circles'
import type { LocationView, PlaceVisit } from '../components/locationData'
import { PERSON_FIELDS, VISIT_FIELDS } from '../components/locationData'
import type { HealthType } from '../pages/health/healthApi'
import type { Photo, PhotoAlbum } from '../photos/api'
import { PHOTO_ALBUM_FIELDS, PHOTO_FIELDS } from '../photos/api'

// A person's card: who somebody is to the signed-in person and a few items of
// everything of theirs that person can see elsewhere, in one answer. The names
// are the server's (internal/api/v1api/apigraph/person_card.go).

// Whether a section has something to show. A section that is unavailable is
// left out of the page; the others each say what they are.
export type PersonCardSectionState = 'shown' | 'notShared' | 'needsContact' | 'unavailable' | 'failed'

export type PersonCardContact = {
  id: string
  /** The book the contact is in. */
  addressBookId: string
  name?: string
  organization?: string
  emails: string[]
  phones: string[]
  addresses: { written: string }[]
  hasPhoto: boolean
  accountUserId?: string | null
}

export type PersonCardCircle = {
  circleId: string
  circleName: string
  circleKind: CircleKind
}

export type PersonCardLocationSection = {
  sectionState: PersonCardSectionState
  location: LocationView | null
  todayVisits: PlaceVisit[]
  hasMoreTodayVisits: boolean
}

export type PersonCardHealthSection = {
  sectionState: PersonCardSectionState
  healthTypes: HealthType[]
  hasMore: boolean
}

export type PersonCardPhotosSection = {
  photosState: PersonCardSectionState
  photos: Photo[]
  hasMorePhotos: boolean
  albumsState: PersonCardSectionState
  albums: PhotoAlbum[]
  hasMoreAlbums: boolean
}

export type PersonCardMail = {
  mailboxItemId: string
  mailboxId: string
  folderPath: string
  subject: string
  fromAddress: string
  fromName: string
  isFromPerson: boolean
  isUnread: boolean
  receivedAt: string
  snippet: string
}

export type PersonCardMailSection = {
  sectionState: PersonCardSectionState
  mails: PersonCardMail[]
  hasMore: boolean
}

export type PersonCardRecording = {
  recordingId: string
  title: string
  recordedAt: string | null
  durationMS: number
  spokenPassageCount: number
}

export type PersonCardRecordingSection = {
  sectionState: PersonCardSectionState
  recordings: PersonCardRecording[]
  hasMore: boolean
}

export type PersonCardMemoryPage = {
  nodePath: string
  nodeName: string
  nodeKind: string
  summary: string
  updatedAt: string
  /** Not tied to the contact: the person page with the contact's name. */
  isMatchedByName: boolean
}

export type PersonCardMemorySection = {
  sectionState: PersonCardSectionState
  pages: PersonCardMemoryPage[]
  hasMore: boolean
}

export type PersonCard = {
  // Null for a contact that is not an account in the signed-in person's circles.
  person: CircleMember | null
  contact: PersonCardContact | null
  sharedCircles: PersonCardCircle[]
  canMessage: boolean
  location: PersonCardLocationSection | null
  health: PersonCardHealthSection | null
  photos: PersonCardPhotosSection | null
  mail: PersonCardMailSection | null
  recordings: PersonCardRecordingSection | null
  memory: PersonCardMemorySection | null
}

const CONTACT_FIELDS = 'id addressBookId name organization emails phones hasPhoto accountUserId addresses { written }'
const MEMBER_FIELDS =
  'userId username name avatarUrl avatarColorIndex memberRole joinedAt relationshipKind relationshipTermGender relationshipWord linkedContactId'
const HEALTH_DAY_FIELDS = 'summaryDay sumValue averageValue minimumValue maximumValue sampleCount durationSeconds'

// One query fills the card. The person is named by an account or by one of the
// signed-in person's contacts, and only one of the two is given.
export const READ_PERSON_CARD = `
  query ($userId: String, $addressBookId: String, $contactId: String, $itemLimit: Int) {
    ReadPersonCard(userId: $userId, addressBookId: $addressBookId, contactId: $contactId, itemLimit: $itemLimit) {
      person { ${MEMBER_FIELDS} }
      contact { ${CONTACT_FIELDS} }
      sharedCircles { circleId circleName circleKind }
      canMessage
      location {
        sectionState
        location { ${PERSON_FIELDS} }
        todayVisits { ${VISIT_FIELDS} }
        hasMoreTodayVisits
      }
      health {
        sectionState
        healthTypes {
          healthType healthCategory typeName valueKind quantityUnit summaryKind firstDay lastDay sampleCount
          latestDaySummary { ${HEALTH_DAY_FIELDS} }
        }
        hasMore
      }
      photos {
        photosState
        photos { ${PHOTO_FIELDS} }
        hasMorePhotos
        albumsState
        albums { ${PHOTO_ALBUM_FIELDS} }
        hasMoreAlbums
      }
      mail {
        sectionState
        mails {
          mailboxItemId mailboxId folderPath subject fromAddress fromName isFromPerson isUnread receivedAt snippet
        }
        hasMore
      }
      recordings {
        sectionState
        recordings { recordingId title recordedAt durationMS spokenPassageCount }
        hasMore
      }
      memory {
        sectionState
        pages { nodePath nodeName nodeKind summary updatedAt isMatchedByName }
        hasMore
      }
    }
  }`

// Where a section's "Show all" goes, in the part of the app the section is
// taken from.
export function personLocationPath(userId: string): string {
  return `/location?person=${encodeURIComponent(userId)}`
}

export function personHealthPath(userId: string): string {
  return `/health?person=${encodeURIComponent(userId)}`
}

// All the signed-in person's mail from an address, in every folder.
export function mailFromAddressPath(address: string): string {
  return `/mailbox?${new URLSearchParams({ from: address, everywhere: 'true' }).toString()}`
}

// A new message to an address.
export function composeToAddressPath(address: string): string {
  return `/mailbox/compose?${new URLSearchParams({ to: address }).toString()}`
}

// A section is drawn when the server says anything about it other than that it
// is unavailable.
export function isSectionDrawn<Section extends { sectionState: PersonCardSectionState }>(
  section: Section | null | undefined,
): section is Section {
  return section !== null && section !== undefined && section.sectionState !== 'unavailable'
}

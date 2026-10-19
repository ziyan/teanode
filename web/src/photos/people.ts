import { graphql } from '../api'
import { CIRCLE_PERSON_FIELDS, type CirclePerson } from '../lib/circlePeople'

// The people found in a person's photos: groups of faces the server believes
// are one person, each named, once the person chooses, by a contact from their
// address book. The names are the server's
// (internal/api/v1api/apigraph/photo_face.go), and the photos of a group are
// ListPhotos with its photoFaceGroupId (see api.ts).

export interface PhotoFaceGroup {
  id: string
  createdAt: string
  updatedAt: string
  // The contact that names the group; empty while it is nobody.
  contactId: string
  contactAddressBookId: string
  contactName: string
  // Whether the contact is the person's own, so that the group is them.
  isOwnContact: boolean
  // Counted over photos that are not in the Trash.
  faceCount: number
  photoCount: number
  // The picture of the group's cover face.
  facePictureUrl: string
  coverPhotoFaceId: string
  isHidden: boolean
  // The person in the circles that the contact is, when the person said so:
  // their picture and the words "In your circles" are drawn for the group.
  accountUserId?: string | null
  account?: CirclePerson | null
}

const PHOTO_FACE_GROUP_FIELDS =
  'id createdAt updatedAt contactId contactAddressBookId contactName isOwnContact faceCount photoCount facePictureUrl coverPhotoFaceId isHidden' +
  ` accountUserId account { ${CIRCLE_PERSON_FIELDS} }`

export const PEOPLE_DOCUMENTS = {
  ListPhotoFaceGroups: `query ($isHiddenIncluded: Boolean) { ListPhotoFaceGroups(isHiddenIncluded: $isHiddenIncluded) { ${PHOTO_FACE_GROUP_FIELDS} } }`,
  ReadPhotoFaceGroup: `query ($photoFaceGroupId: String!) { ReadPhotoFaceGroup(photoFaceGroupId: $photoFaceGroupId) { ${PHOTO_FACE_GROUP_FIELDS} } }`,
  NamePhotoFaceGroup: `mutation ($photoFaceGroupId: String!, $contactId: String!) { NamePhotoFaceGroup(photoFaceGroupId: $photoFaceGroupId, contactId: $contactId) { photoFaceGroup { ${PHOTO_FACE_GROUP_FIELDS} } mergedPhotoFaceGroupId isContactPictureSet } }`,
  UnnamePhotoFaceGroup: `mutation ($photoFaceGroupId: String!) { UnnamePhotoFaceGroup(photoFaceGroupId: $photoFaceGroupId) { ${PHOTO_FACE_GROUP_FIELDS} } }`,
  MergePhotoFaceGroups: `mutation ($photoFaceGroupIds: [String!]!) { MergePhotoFaceGroups(photoFaceGroupIds: $photoFaceGroupIds) { ${PHOTO_FACE_GROUP_FIELDS} } }`,
  RemovePhotoFaceFromGroup: `mutation ($photoFaceId: String!) { RemovePhotoFaceFromGroup(photoFaceId: $photoFaceId) { ${PHOTO_FACE_GROUP_FIELDS} } }`,
  HidePhotoFaceGroup: `mutation ($photoFaceGroupId: String!, $isHidden: Boolean!) { HidePhotoFaceGroup(photoFaceGroupId: $photoFaceGroupId, isHidden: $isHidden) { ${PHOTO_FACE_GROUP_FIELDS} } }`,
  RemoveContactPicture: 'mutation ($id: String!) { RemoveContactPicture(id: $id) { id hasPhoto } }',
  SetPhotoFaceRecognition: 'mutation ($isEnabled: Boolean!) { SetPhotoFaceRecognition(isEnabled: $isEnabled) }',
  ListAddressBooks: 'query { ListAddressBooks { id name } }',
  ListContacts: `query ($addressBookId: String!, $query: String, $first: Int) {
    ListContacts(addressBookId: $addressBookId, query: $query, first: $first) {
      id name organization emails account { ${CIRCLE_PERSON_FIELDS} }
    }
  }`,
  SaveContact:
    'mutation ($addressBookId: String!, $name: String) { SaveContact(addressBookId: $addressBookId, name: $name) { id name } }',
}

export async function listPhotoFaceGroups(isHiddenIncluded: boolean, signal?: AbortSignal): Promise<PhotoFaceGroup[]> {
  const answer = await graphql<{ ListPhotoFaceGroups: PhotoFaceGroup[] }>(
    PEOPLE_DOCUMENTS.ListPhotoFaceGroups,
    { isHiddenIncluded },
    signal,
  )
  return answer.ListPhotoFaceGroups
}

export async function readPhotoFaceGroup(photoFaceGroupId: string, signal?: AbortSignal): Promise<PhotoFaceGroup> {
  const answer = await graphql<{ ReadPhotoFaceGroup: PhotoFaceGroup }>(
    PEOPLE_DOCUMENTS.ReadPhotoFaceGroup,
    { photoFaceGroupId },
    signal,
  )
  return answer.ReadPhotoFaceGroup
}

// What naming a group answered. A contact names one person, so a contact that
// named another group already merges the two: photoFaceGroup is the group
// that was named already, with the faces of both, and mergedPhotoFaceGroupId
// is the group that went into it (empty when nothing was merged). A contact
// that had no picture is given one cut from a face of the group.
export interface NamedPhotoFaceGroup {
  photoFaceGroup: PhotoFaceGroup
  mergedPhotoFaceGroupId: string
  isContactPictureSet: boolean
}

export async function namePhotoFaceGroup(photoFaceGroupId: string, contactId: string): Promise<NamedPhotoFaceGroup> {
  const answer = await graphql<{ NamePhotoFaceGroup: NamedPhotoFaceGroup }>(PEOPLE_DOCUMENTS.NamePhotoFaceGroup, {
    photoFaceGroupId,
    contactId,
  })
  return answer.NamePhotoFaceGroup
}

// Takes the picture off a contact and leaves the rest of the card.
export async function removeContactPicture(contactId: string): Promise<void> {
  await graphql(PEOPLE_DOCUMENTS.RemoveContactPicture, { id: contactId })
}

export async function unnamePhotoFaceGroup(photoFaceGroupId: string): Promise<PhotoFaceGroup> {
  const answer = await graphql<{ UnnamePhotoFaceGroup: PhotoFaceGroup }>(PEOPLE_DOCUMENTS.UnnamePhotoFaceGroup, {
    photoFaceGroupId,
  })
  return answer.UnnamePhotoFaceGroup
}

// The first group stays and takes the faces of the others.
export async function mergePhotoFaceGroups(photoFaceGroupIds: string[]): Promise<PhotoFaceGroup> {
  const answer = await graphql<{ MergePhotoFaceGroups: PhotoFaceGroup }>(PEOPLE_DOCUMENTS.MergePhotoFaceGroups, {
    photoFaceGroupIds,
  })
  return answer.MergePhotoFaceGroups
}

// "This is not them": the face leaves its group and is not grouped again.
export async function removePhotoFaceFromGroup(photoFaceId: string): Promise<PhotoFaceGroup> {
  const answer = await graphql<{ RemovePhotoFaceFromGroup: PhotoFaceGroup }>(
    PEOPLE_DOCUMENTS.RemovePhotoFaceFromGroup,
    { photoFaceId },
  )
  return answer.RemovePhotoFaceFromGroup
}

export async function hidePhotoFaceGroup(photoFaceGroupId: string, isHidden: boolean): Promise<PhotoFaceGroup> {
  const answer = await graphql<{ HidePhotoFaceGroup: PhotoFaceGroup }>(PEOPLE_DOCUMENTS.HidePhotoFaceGroup, {
    photoFaceGroupId,
    isHidden,
  })
  return answer.HidePhotoFaceGroup
}

// Switching it off deletes every face and group found so far, with their names.
export async function setPhotoFaceRecognition(isEnabled: boolean): Promise<boolean> {
  const answer = await graphql<{ SetPhotoFaceRecognition: boolean }>(PEOPLE_DOCUMENTS.SetPhotoFaceRecognition, {
    isEnabled,
  })
  return answer.SetPhotoFaceRecognition
}

// A contact as the naming dialog offers it.
export interface ContactChoice {
  id: string
  name: string
  organization: string
  emails: string[]
  // The person in the circles the contact is, when it is linked to one. The
  // dialog says so beside the contact; it never makes a contact for a person
  // in the circles, which only a contact that is already there can be.
  account?: CirclePerson | null
}

// How many contacts the dialog lists per address book: more than fits without
// scrolling, few enough that the search is how a contact is found in a big
// book.
const CONTACT_CHOICES = 20

// The contacts of the person's address books that match what was typed, and
// whether some book had more matching than are listed. One more than the
// dialog lists is asked for from each book, so that a book with exactly that
// many is not mistaken for one with more.
export async function findContactChoices(
  searchText: string,
  signal?: AbortSignal,
): Promise<{ contacts: ContactChoice[]; hasMore: boolean }> {
  const books = (await graphql<{ ListAddressBooks: { id: string }[] }>(PEOPLE_DOCUMENTS.ListAddressBooks, {}, signal))
    .ListAddressBooks
  const found = await Promise.all(
    books.map((book) =>
      graphql<{ ListContacts: ContactChoice[] }>(
        PEOPLE_DOCUMENTS.ListContacts,
        { addressBookId: book.id, query: searchText || null, first: CONTACT_CHOICES + 1 },
        signal,
      ),
    ),
  )
  return {
    contacts: found.flatMap((answer) => answer.ListContacts.slice(0, CONTACT_CHOICES)),
    hasMore: found.some((answer) => answer.ListContacts.length > CONTACT_CHOICES),
  }
}

// The person has no address book to make a contact in. A page says it in its
// own words, in the reader's language.
export class NoAddressBookError extends Error {
  constructor() {
    super('The person has no address book.')
    this.name = 'NoAddressBookError'
  }
}

// A new contact with only a name, in the person's first address book, for a
// person typed in the naming dialog who is not in the address book yet.
export async function createContact(name: string): Promise<ContactChoice> {
  const books = (await graphql<{ ListAddressBooks: { id: string }[] }>(PEOPLE_DOCUMENTS.ListAddressBooks))
    .ListAddressBooks
  if (books.length === 0) throw new NoAddressBookError()
  const answer = await graphql<{ SaveContact: { id: string; name: string } }>(PEOPLE_DOCUMENTS.SaveContact, {
    addressBookId: books[0].id,
    name,
  })
  return { id: answer.SaveContact.id, name: answer.SaveContact.name, organization: '', emails: [] }
}

// What a contact is called in a list: the name, else the first address.
export function contactChoiceLabel(contact: ContactChoice): string {
  return contact.name || contact.emails[0] || contact.organization || contact.id
}

// What a group is called: the contact that names it, or nothing.
export function personName(group: Pick<PhotoFaceGroup, 'contactName'>): string {
  return group.contactName.trim()
}

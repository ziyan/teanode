import { graphql } from '../api'

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
  // Counted over photos that are not in the Trash.
  faceCount: number
  photoCount: number
  // The picture of the group's cover face.
  facePictureUrl: string
  coverPhotoFaceId: string
  isHidden: boolean
}

const PHOTO_FACE_GROUP_FIELDS =
  'id createdAt updatedAt contactId contactAddressBookId contactName faceCount photoCount facePictureUrl coverPhotoFaceId isHidden'

export const PEOPLE_DOCUMENTS = {
  ListPhotoFaceGroups: `query ($isHiddenIncluded: Boolean) { ListPhotoFaceGroups(isHiddenIncluded: $isHiddenIncluded) { ${PHOTO_FACE_GROUP_FIELDS} } }`,
  ReadPhotoFaceGroup: `query ($photoFaceGroupId: String!) { ReadPhotoFaceGroup(photoFaceGroupId: $photoFaceGroupId) { ${PHOTO_FACE_GROUP_FIELDS} } }`,
  NamePhotoFaceGroup: `mutation ($photoFaceGroupId: String!, $contactId: String!) { NamePhotoFaceGroup(photoFaceGroupId: $photoFaceGroupId, contactId: $contactId) { ${PHOTO_FACE_GROUP_FIELDS} } }`,
  UnnamePhotoFaceGroup: `mutation ($photoFaceGroupId: String!) { UnnamePhotoFaceGroup(photoFaceGroupId: $photoFaceGroupId) { ${PHOTO_FACE_GROUP_FIELDS} } }`,
  MergePhotoFaceGroups: `mutation ($photoFaceGroupIds: [String!]!) { MergePhotoFaceGroups(photoFaceGroupIds: $photoFaceGroupIds) { ${PHOTO_FACE_GROUP_FIELDS} } }`,
  RemovePhotoFaceFromGroup: `mutation ($photoFaceId: String!) { RemovePhotoFaceFromGroup(photoFaceId: $photoFaceId) { ${PHOTO_FACE_GROUP_FIELDS} } }`,
  HidePhotoFaceGroup: `mutation ($photoFaceGroupId: String!, $isHidden: Boolean!) { HidePhotoFaceGroup(photoFaceGroupId: $photoFaceGroupId, isHidden: $isHidden) { ${PHOTO_FACE_GROUP_FIELDS} } }`,
  SetPhotoFaceRecognition: 'mutation ($isEnabled: Boolean!) { SetPhotoFaceRecognition(isEnabled: $isEnabled) }',
  ListAddressBooks: 'query { ListAddressBooks { id name } }',
  ListContacts: `query ($addressBookId: String!, $query: String, $first: Int) {
    ListContacts(addressBookId: $addressBookId, query: $query, first: $first) { id name organization emails }
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

export async function namePhotoFaceGroup(photoFaceGroupId: string, contactId: string): Promise<PhotoFaceGroup> {
  const answer = await graphql<{ NamePhotoFaceGroup: PhotoFaceGroup }>(PEOPLE_DOCUMENTS.NamePhotoFaceGroup, {
    photoFaceGroupId,
    contactId,
  })
  return answer.NamePhotoFaceGroup
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
}

// How many contacts the dialog lists per address book: more than fits without
// scrolling, few enough that the search is how a contact is found in a big
// book.
const CONTACT_CHOICES = 20

// The contacts of the person's address books that match what was typed.
export async function findContactChoices(searchText: string, signal?: AbortSignal): Promise<ContactChoice[]> {
  const books = (await graphql<{ ListAddressBooks: { id: string }[] }>(PEOPLE_DOCUMENTS.ListAddressBooks, {}, signal))
    .ListAddressBooks
  const found = await Promise.all(
    books.map((book) =>
      graphql<{ ListContacts: ContactChoice[] }>(
        PEOPLE_DOCUMENTS.ListContacts,
        { addressBookId: book.id, query: searchText || null, first: CONTACT_CHOICES },
        signal,
      ),
    ),
  )
  return found.flatMap((answer) => answer.ListContacts)
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

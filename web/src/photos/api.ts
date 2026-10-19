import { graphql } from '../api'

// The photo library's documents and the shapes they return. The names are the
// server's (internal/api/v1api/apigraph/photo.go), and every page of the
// dashboard that shows photos goes through here so that a field is spelled
// once.

export type PhotoKind = 'picture' | 'video' | 'livePhoto'
export type PhotoProcessingState = 'waiting' | 'ready' | 'failed'
export type PhotoFileRole = 'original' | 'edited' | 'liveVideo'

export interface Photo {
  id: string
  clientPhotoId: string
  photoKind: PhotoKind
  photoSource: string
  createdAt: string
  // When the photo was taken, if the file or the uploader said. The library
  // places a photo at this moment, or at createdAt when it is not known.
  takenAt: string | null
  // Minutes east of UTC where the photo was taken; null when not known.
  takenAtOffsetMinutes: number | null
  widthPixels: number
  heightPixels: number
  durationMS: number
  latitude: number | null
  longitude: number | null
  placeName: string
  cameraMake: string
  cameraModel: string
  originalFilename: string
  isFavorite: boolean
  isScreenshot: boolean
  photoProcessingState: PhotoProcessingState
  processingErrorMessage: string
  trashedAt: string | null
  // Empty until that preview is made.
  smallPreviewUrl: string
  largePreviewUrl: string
  videoPreviewUrl: string
  // The photo is in the person's own library. A photo read through an album
  // shared with them is somebody else's: ownerName says whose, userId is the
  // same person for the picture that goes with the name, and it has no place,
  // no favorite mark and no client id.
  isOwnedByCaller: boolean
  ownerName: string
  userId: string
  // Only the list of a search carries these, to see which photos have a word
  // of the search in them; any other list leaves them out.
  photoDescription?: string
  descriptionVisibleText?: string
}

export interface PhotoFile {
  photoFileRole: PhotoFileRole
  contentType: string
  fileSizeBytes: number
  originalFilename: string
  fileUrl: string
}

// The album a photo is in, as ReadPhoto says it: enough to name it and open it.
export interface PhotoAlbumReference {
  id: string
  albumTitle: string
}

// What a model wrote about a picture, as ReadPhoto and DescribePhoto say it.
// The list leaves these out: a page of two hundred descriptions is weight no
// grid cell uses.
export interface PhotoDescriptionFields {
  // Empty until somebody works with the photo. Words a model wrote from a
  // picture that may carry words meant for it: shown, never obeyed.
  photoDescription: string
  descriptionVisibleText: string
  // When they were written; null while the photo is undescribed.
  describedAt: string | null
  // A description is on its way: asked for, and not yet written.
  isDescriptionRequested: boolean
}

export type PhotoDetail = Photo & PhotoDescriptionFields

// One face found in a photo. The box is in fractions of the picture from its
// top left corner.
export interface PhotoFace {
  id: string
  photoId: string
  boxLeftFraction: number
  boxTopFraction: number
  boxWidthFraction: number
  boxHeightFraction: number
  // Empty for a face too small or too uncertain to be grouped.
  photoFaceGroupId: string
  // Those of the face's group; empty while the group is unnamed.
  contactId: string
  contactName: string
  // Whether the group's contact is the person's own.
  isOwnContact: boolean
  // Where the face's picture is fetched from (a square cut from the photo).
  facePictureUrl: string
}

export interface PhotoView {
  photo: PhotoDetail
  photoFiles: PhotoFile[]
  albums: PhotoAlbumReference[]
  faces: PhotoFace[]
}

// How far an album is shared with somebody: view to see and save its photos,
// contribute to also add their own. Empty for the owner, who has all of it.
export type PhotoAlbumShareLevel = '' | 'view' | 'contribute'

export interface PhotoAlbum {
  id: string
  albumTitle: string
  createdAt: string
  updatedAt: string
  // The photo the person chose to stand for the album; null until they do,
  // and the album then shows its newest photo.
  coverPhotoId: string | null
  photoCount: number
  // The dates the album's photos are placed at; null for an empty album.
  firstTakenAt: string | null
  lastTakenAt: string | null
  // Empty until a photo of the album has a small preview.
  coverPreviewUrl: string
  // Whose it is: a person's, or a circle's (then ownerUserId is null and
  // circleName says which). The owner's name and picture are for the card.
  ownerUserId: string | null
  circleId: string | null
  ownerUsername: string
  ownerName: string
  ownerAvatarUrl: string | null
  ownerAvatarColorIndex: number
  circleName: string
  // What the person may do with it: the owner, and an admin of the circle
  // that owns it, manage it; the owner, a circle's members and anybody it is
  // shared with at contribute add their own photos.
  isOwnedByCaller: boolean
  shareLevel: PhotoAlbumShareLevel
  canCallerContribute: boolean
  canCallerManage: boolean
  // The share that reaches the person lets them see where photos were taken.
  isLocationIncluded: boolean
}

// One share of an album: with one person or one circle, never both.
export interface PhotoAlbumShare {
  id: string
  photoAlbumId: string
  granteeUserId: string | null
  granteeUsername: string
  granteeName: string
  granteeAvatarUrl: string | null
  granteeAvatarColorIndex: number
  granteeCircleId: string | null
  granteeCircleName: string
  shareLevel: 'view' | 'contribute'
  isLocationIncluded: boolean
  createdAt: string
}

// A run of photos in no album that belong together: a trip or an event. The
// server makes nothing from it until the person asks.
export interface PhotoAlbumSuggestion {
  takenAfter: string
  takenBefore: string
  placeNames: string[]
  albumTitle: string
  photoCount: number
  samplePhotoIds: string[]
}

export interface PhotoLibrary {
  photoCount: number
  videoCount: number
  favoriteCount: number
  trashedCount: number
  usedBytes: number
  maximumFileBytes: number
  maximumAccountBytes: number
  trashRetentionDays: number
  uploadPath: string
  uploadPartBytes: number
  // Whether this server and this person can have a photo described, and when
  // not, why, in the server's words.
  isDescriptionAvailable: boolean
  descriptionUnavailableMessage: string
  // Whether this server can find faces, why not when it cannot, and the
  // person's own switch for it.
  isFaceRecognitionAvailable: boolean
  faceRecognitionUnavailableMessage: string
  isPhotoFaceRecognitionEnabled: boolean
}

// Which of the library's three lists is shown.
export type PhotoListKind = 'library' | 'favorites' | 'trash'

// How many photos a page of the timeline holds.
export const PHOTOS_PER_PAGE = 200

const PHOTO_FIELDS =
  'id clientPhotoId photoKind photoSource createdAt takenAt takenAtOffsetMinutes widthPixels heightPixels durationMS latitude longitude placeName cameraMake cameraModel originalFilename isFavorite isScreenshot photoProcessingState processingErrorMessage trashedAt smallPreviewUrl largePreviewUrl videoPreviewUrl isOwnedByCaller ownerName userId'

const PHOTO_DETAIL_FIELDS = `${PHOTO_FIELDS} photoDescription descriptionVisibleText describedAt isDescriptionRequested`

const PHOTO_FACE_FIELDS =
  'id photoId boxLeftFraction boxTopFraction boxWidthFraction boxHeightFraction photoFaceGroupId contactId contactName isOwnContact facePictureUrl'

const PHOTO_ALBUM_FIELDS =
  'id albumTitle createdAt updatedAt coverPhotoId photoCount firstTakenAt lastTakenAt coverPreviewUrl ownerUserId circleId ownerUsername ownerName ownerAvatarUrl ownerAvatarColorIndex circleName isOwnedByCaller shareLevel canCallerContribute canCallerManage isLocationIncluded'

const PHOTO_ALBUM_SHARE_FIELDS =
  'id photoAlbumId granteeUserId granteeUsername granteeName granteeAvatarUrl granteeAvatarColorIndex granteeCircleId granteeCircleName shareLevel isLocationIncluded createdAt'

function listPhotosDocument(fields: string): string {
  return `query ($offset: Int, $pageSize: Int, $isFavorite: Boolean, $isTrashed: Boolean, $searchText: String, $albumId: String, $faceGroupId: String, $accountUserId: String, $photoKind: String, $takenAfter: DateTime, $takenBefore: DateTime, $nearLatitude: Float, $nearLongitude: Float, $nearDistanceKilometers: Float) { ListPhotos(offset: $offset, pageSize: $pageSize, isFavorite: $isFavorite, isTrashed: $isTrashed, searchText: $searchText, photoKind: $photoKind, albumId: $albumId, faceGroupId: $faceGroupId, accountUserId: $accountUserId, takenAfter: $takenAfter, takenBefore: $takenBefore, nearLatitude: $nearLatitude, nearLongitude: $nearLongitude, nearDistanceKilometers: $nearDistanceKilometers) { ${fields} } }`
}

export const PHOTO_DOCUMENTS = {
  ListPhotos: listPhotosDocument(PHOTO_FIELDS),
  ListPhotosOfSearch: listPhotosDocument(`${PHOTO_FIELDS} photoDescription descriptionVisibleText`),
  CountPhotos:
    'query ($isFavorite: Boolean, $isTrashed: Boolean, $searchText: String, $albumId: String, $faceGroupId: String, $accountUserId: String, $photoKind: String, $takenAfter: DateTime, $takenBefore: DateTime, $nearLatitude: Float, $nearLongitude: Float, $nearDistanceKilometers: Float) { CountPhotos(isFavorite: $isFavorite, isTrashed: $isTrashed, searchText: $searchText, photoKind: $photoKind, albumId: $albumId, faceGroupId: $faceGroupId, accountUserId: $accountUserId, takenAfter: $takenAfter, takenBefore: $takenBefore, nearLatitude: $nearLatitude, nearLongitude: $nearLongitude, nearDistanceKilometers: $nearDistanceKilometers) }',
  ReadPhoto: `query ($photoId: String!) { ReadPhoto(photoId: $photoId) { photo { ${PHOTO_DETAIL_FIELDS} } photoFiles { photoFileRole contentType fileSizeBytes originalFilename fileUrl } albums { id albumTitle } faces { ${PHOTO_FACE_FIELDS} } } }`,
  ReadPhotoLibrary:
    'query { ReadPhotoLibrary { photoCount videoCount favoriteCount trashedCount usedBytes maximumFileBytes maximumAccountBytes trashRetentionDays uploadPath uploadPartBytes isDescriptionAvailable descriptionUnavailableMessage isFaceRecognitionAvailable faceRecognitionUnavailableMessage isPhotoFaceRecognitionEnabled } }',
  FindPhotos:
    'query ($clientPhotoIds: [String!]!) { FindPhotos(clientPhotoIds: $clientPhotoIds) { id clientPhotoId trashedAt } }',
  UpdatePhoto: `mutation ($photoId: String!, $isFavorite: Boolean) { UpdatePhoto(photoId: $photoId, isFavorite: $isFavorite) { ${PHOTO_FIELDS} } }`,
  UpdatePhotoTakenAt: `mutation ($photoId: String!, $takenAt: DateTime) { UpdatePhoto(photoId: $photoId, takenAt: $takenAt) { ${PHOTO_FIELDS} } }`,
  SearchPhotoPlaces:
    'query ($query: String!, $nearLatitude: Float, $nearLongitude: Float, $limitCount: Int) { SearchPhotoPlaces(query: $query, nearLatitude: $nearLatitude, nearLongitude: $nearLongitude, limitCount: $limitCount) { placeName latitude longitude distanceKilometers } }',
  SetPhotoLocation:
    'mutation ($photoIds: [String!]!, $latitude: Float!, $longitude: Float!) { SetPhotoLocation(photoIds: $photoIds, latitude: $latitude, longitude: $longitude) }',
  RemovePhotoLocation: 'mutation ($photoIds: [String!]!) { RemovePhotoLocation(photoIds: $photoIds) }',
  SavePhotoToLibrary: `mutation ($photoId: String!) { SavePhotoToLibrary(photoId: $photoId) { ${PHOTO_FIELDS} } }`,
  SavePhotoFromAttachment: `mutation ($agentAttachmentId: String!) { SavePhotoFromAttachment(agentAttachmentId: $agentAttachmentId) { ${PHOTO_FIELDS} } }`,
  DescribePhoto: `mutation ($photoId: String!, $isRedescribed: Boolean) { DescribePhoto(photoId: $photoId, isRedescribed: $isRedescribed) { ${PHOTO_DETAIL_FIELDS} } }`,
  TrashPhotos: 'mutation ($photoIds: [String!]!) { TrashPhotos(photoIds: $photoIds) }',
  RestorePhotos: 'mutation ($photoIds: [String!]!) { RestorePhotos(photoIds: $photoIds) }',
  DeletePhotos: 'mutation ($photoIds: [String!]!) { DeletePhotos(photoIds: $photoIds) }',
  EmptyPhotoTrash: 'mutation { EmptyPhotoTrash }',
  RetryPhotoProcessing: 'mutation ($photoIds: [String!]!) { RetryPhotoProcessing(photoIds: $photoIds) }',
  ListPhotoAlbums: `query { ListPhotoAlbums { ${PHOTO_ALBUM_FIELDS} } }`,
  SuggestPhotoAlbums:
    'query { SuggestPhotoAlbums { takenAfter takenBefore placeNames albumTitle photoCount samplePhotoIds } }',
  ReadPhotoAlbum: `query ($photoAlbumId: String!) { ReadPhotoAlbum(photoAlbumId: $photoAlbumId) { ${PHOTO_ALBUM_FIELDS} } }`,
  CreatePhotoAlbum: `mutation ($albumTitle: String!, $photoIds: [String!], $circleId: String) { CreatePhotoAlbum(albumTitle: $albumTitle, photoIds: $photoIds, circleId: $circleId) { ${PHOTO_ALBUM_FIELDS} } }`,
  ListPhotoAlbumShares: `query ($photoAlbumId: String!) { ListPhotoAlbumShares(photoAlbumId: $photoAlbumId) { ${PHOTO_ALBUM_SHARE_FIELDS} } }`,
  SharePhotoAlbum: `mutation ($photoAlbumId: String!, $granteeUserId: String, $granteeCircleId: String, $shareLevel: String!, $isLocationIncluded: Boolean) { SharePhotoAlbum(photoAlbumId: $photoAlbumId, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, shareLevel: $shareLevel, isLocationIncluded: $isLocationIncluded) { ${PHOTO_ALBUM_SHARE_FIELDS} } }`,
  UpdatePhotoAlbumShare: `mutation ($photoAlbumShareId: String!, $shareLevel: String!, $isLocationIncluded: Boolean) { UpdatePhotoAlbumShare(photoAlbumShareId: $photoAlbumShareId, shareLevel: $shareLevel, isLocationIncluded: $isLocationIncluded) { ${PHOTO_ALBUM_SHARE_FIELDS} } }`,
  RemovePhotoAlbumShare:
    'mutation ($photoAlbumShareId: String!) { RemovePhotoAlbumShare(photoAlbumShareId: $photoAlbumShareId) }',
  RenamePhotoAlbum: `mutation ($photoAlbumId: String!, $albumTitle: String!) { RenamePhotoAlbum(photoAlbumId: $photoAlbumId, albumTitle: $albumTitle) { ${PHOTO_ALBUM_FIELDS} } }`,
  DeletePhotoAlbum: 'mutation ($photoAlbumId: String!) { DeletePhotoAlbum(photoAlbumId: $photoAlbumId) }',
  AddPhotosToAlbum:
    'mutation ($photoAlbumId: String!, $photoIds: [String!]!) { AddPhotosToAlbum(photoAlbumId: $photoAlbumId, photoIds: $photoIds) }',
  RemovePhotosFromAlbum:
    'mutation ($photoAlbumId: String!, $photoIds: [String!]!) { RemovePhotosFromAlbum(photoAlbumId: $photoAlbumId, photoIds: $photoIds) }',
  SetPhotoAlbumCover: `mutation ($photoAlbumId: String!, $photoId: String!) { SetPhotoAlbumCover(photoAlbumId: $photoAlbumId, photoId: $photoId) { ${PHOTO_ALBUM_FIELDS} } }`,
}

// What the server accepts in one call that takes photo ids.
const MOST_PHOTO_IDS_PER_CALL = 500

export interface PhotoFilter {
  listKind: PhotoListKind
  searchText: string
  // Keeps the photos of one album.
  photoAlbumId?: string
  // Keeps the photos with a face in one group of faces, one person.
  photoFaceGroupId?: string
  // Keeps the photos of the contact the person linked to this account in
  // their circles. The server resolves it through the person's own contacts.
  accountUserId?: string
  // Keeps one kind: only pictures, only Live Photos or only videos.
  photoKind?: PhotoKind
  // Keeps the photos taken in a range: from the first moment to just before
  // the second. The date filter of the library and the photos of a suggestion
  // both are this.
  takenAfter?: string
  takenBefore?: string
  // Keeps the photos taken within a distance of a point: "photos taken near
  // here". All three or none.
  nearLatitude?: number
  nearLongitude?: number
  nearDistanceKilometers?: number
}

function filterVariables(filter: PhotoFilter) {
  return {
    isFavorite: filter.listKind === 'favorites' ? true : null,
    isTrashed: filter.listKind === 'trash' ? true : null,
    searchText: filter.searchText || null,
    albumId: filter.photoAlbumId || null,
    faceGroupId: filter.photoFaceGroupId || null,
    accountUserId: filter.accountUserId || null,
    photoKind: filter.photoKind || null,
    takenAfter: filter.takenAfter || null,
    takenBefore: filter.takenBefore || null,
    nearLatitude: filter.nearLatitude ?? null,
    nearLongitude: filter.nearLongitude ?? null,
    nearDistanceKilometers: filter.nearDistanceKilometers ?? null,
  }
}

export async function listPhotos(
  filter: PhotoFilter,
  offset: number,
  pageSize: number,
  signal?: AbortSignal,
): Promise<Photo[]> {
  const answer = await graphql<{ ListPhotos: Photo[] }>(
    filter.searchText ? PHOTO_DOCUMENTS.ListPhotosOfSearch : PHOTO_DOCUMENTS.ListPhotos,
    { ...filterVariables(filter), offset, pageSize },
    signal,
  )
  return answer.ListPhotos
}

// How many photos the filter matches, for the pager's total.
export async function countPhotos(filter: PhotoFilter, signal?: AbortSignal): Promise<number> {
  const answer = await graphql<{ CountPhotos: number }>(PHOTO_DOCUMENTS.CountPhotos, filterVariables(filter), signal)
  return answer.CountPhotos
}

export async function readPhoto(photoId: string, signal?: AbortSignal): Promise<PhotoView> {
  const answer = await graphql<{ ReadPhoto: PhotoView }>(PHOTO_DOCUMENTS.ReadPhoto, { photoId }, signal)
  return answer.ReadPhoto
}

// Asks for a description of a photo, or a new one. The answer is the photo as
// it now stands: with isDescriptionRequested true while the description is on
// its way, and describedAt once it has been written.
export async function describePhoto(photoId: string, isRedescribed: boolean): Promise<PhotoDetail> {
  const answer = await graphql<{ DescribePhoto: PhotoDetail }>(PHOTO_DOCUMENTS.DescribePhoto, {
    photoId,
    isRedescribed,
  })
  return answer.DescribePhoto
}

export async function readPhotoLibrary(signal?: AbortSignal): Promise<PhotoLibrary> {
  const answer = await graphql<{ ReadPhotoLibrary: PhotoLibrary }>(PHOTO_DOCUMENTS.ReadPhotoLibrary, {}, signal)
  return answer.ReadPhotoLibrary
}

// A photo the library already holds, as found by the uploader's name for it.
export interface KnownPhoto {
  id: string
  // The photo is in the Trash, where it is not shown with the others.
  isTrashed: boolean
}

// The photos, of those asked about by their client ids, that the library
// already holds, with the Trash told apart: a file sent again after it was
// trashed is held, but a person looking at the library does not see it.
export async function findKnownPhotos(
  clientPhotoIds: string[],
  signal?: AbortSignal,
): Promise<Map<string, KnownPhoto>> {
  const known = new Map<string, KnownPhoto>()
  for (let start = 0; start < clientPhotoIds.length; start += MOST_PHOTO_IDS_PER_CALL) {
    const answer = await graphql<{ FindPhotos: { id: string; clientPhotoId: string; trashedAt: string | null }[] }>(
      PHOTO_DOCUMENTS.FindPhotos,
      { clientPhotoIds: clientPhotoIds.slice(start, start + MOST_PHOTO_IDS_PER_CALL) },
      signal,
    )
    for (const found of answer.FindPhotos) {
      known.set(found.clientPhotoId, { id: found.id, isTrashed: Boolean(found.trashedAt) })
    }
  }
  return known
}

export async function setPhotoFavorite(photoId: string, isFavorite: boolean): Promise<Photo> {
  const answer = await graphql<{ UpdatePhoto: Photo }>(PHOTO_DOCUMENTS.UpdatePhoto, { photoId, isFavorite })
  return answer.UpdatePhoto
}

// Corrects the date a photo was taken. takenAt is RFC 3339 and carries the
// offset of the place the photo was taken at, which the server keeps as the
// offset the photo is shown at.
export async function setPhotoTakenAt(photoId: string, takenAt: string): Promise<Photo> {
  const answer = await graphql<{ UpdatePhoto: Photo }>(PHOTO_DOCUMENTS.UpdatePhotoTakenAt, { photoId, takenAt })
  return answer.UpdatePhoto
}

// A city the server found for the words typed: its name as a photo's place
// is named, where it is, and how far that is from the point the search was
// asked to stay near (null when it was not asked to).
export interface PhotoPlace {
  placeName: string
  latitude: number
  longitude: number
  distanceKilometers: number | null
}

// Looks for cities by name, best match first. With a point, the nearest of
// equal matches come first.
export async function searchPhotoPlaces(
  query: string,
  near?: { latitude: number; longitude: number },
  signal?: AbortSignal,
): Promise<PhotoPlace[]> {
  const answer = await graphql<{ SearchPhotoPlaces: PhotoPlace[] }>(
    PHOTO_DOCUMENTS.SearchPhotoPlaces,
    { query, nearLatitude: near?.latitude ?? null, nearLongitude: near?.longitude ?? null },
    signal,
  )
  return answer.SearchPhotoPlaces
}

// Keeps a photo of an album shared with the person as a photo of their own.
export async function savePhotoToLibrary(photoId: string): Promise<Photo> {
  const answer = await graphql<{ SavePhotoToLibrary: Photo }>(PHOTO_DOCUMENTS.SavePhotoToLibrary, { photoId })
  return answer.SavePhotoToLibrary
}

// Keeps a picture or a video of the person's conversation with their assistant
// as a photo.
export async function savePhotoFromAttachment(agentAttachmentId: string): Promise<Photo> {
  const answer = await graphql<{ SavePhotoFromAttachment: Photo }>(PHOTO_DOCUMENTS.SavePhotoFromAttachment, {
    agentAttachmentId,
  })
  return answer.SavePhotoFromAttachment
}

// The mutations that take photo ids send them in batches the server accepts,
// and say how many of them it acted on.
async function changePhotos(
  name: 'TrashPhotos' | 'RestorePhotos' | 'DeletePhotos' | 'RetryPhotoProcessing',
  photoIds: string[],
): Promise<number> {
  let changedCount = 0
  for (let start = 0; start < photoIds.length; start += MOST_PHOTO_IDS_PER_CALL) {
    const answer = await graphql<Record<string, number>>(PHOTO_DOCUMENTS[name], {
      photoIds: photoIds.slice(start, start + MOST_PHOTO_IDS_PER_CALL),
    })
    changedCount += answer[name]
  }
  return changedCount
}

// Puts photos whose previews failed (or that are ready and should be made
// again) back to waiting, and says how many it moved.
export const retryPhotoProcessing = (photoIds: string[]) => changePhotos('RetryPhotoProcessing', photoIds)
export const trashPhotos = (photoIds: string[]) => changePhotos('TrashPhotos', photoIds)
export const restorePhotos = (photoIds: string[]) => changePhotos('RestorePhotos', photoIds)
export const deletePhotosForever = (photoIds: string[]) => changePhotos('DeletePhotos', photoIds)

// Says where photos were taken, or that they were taken nowhere the library
// should place them, in batches the server accepts. Both answer how many
// photos changed: the ones that are not the person's, or are in the Trash, are
// skipped.
async function changePhotoLocations(
  name: 'SetPhotoLocation' | 'RemovePhotoLocation',
  photoIds: string[],
  coordinates: { latitude?: number; longitude?: number },
): Promise<number> {
  let changedCount = 0
  for (let start = 0; start < photoIds.length; start += MOST_PHOTO_IDS_PER_CALL) {
    const answer = await graphql<Record<string, number>>(PHOTO_DOCUMENTS[name], {
      photoIds: photoIds.slice(start, start + MOST_PHOTO_IDS_PER_CALL),
      ...coordinates,
    })
    changedCount += answer[name]
  }
  return changedCount
}

export const setPhotoLocation = (photoIds: string[], latitude: number, longitude: number) =>
  changePhotoLocations('SetPhotoLocation', photoIds, { latitude, longitude })
export const removePhotoLocation = (photoIds: string[]) => changePhotoLocations('RemovePhotoLocation', photoIds, {})

export async function emptyPhotoTrash(): Promise<number> {
  const answer = await graphql<{ EmptyPhotoTrash: number }>(PHOTO_DOCUMENTS.EmptyPhotoTrash)
  return answer.EmptyPhotoTrash
}

// Where the small preview of a photo is read from, for a chip that shows the
// photo without having the photo in hand.
export const photoSmallPreviewPath = (photoId: string) => `/api/v1/photos/${encodeURIComponent(photoId)}/previews/small`

// Where a file of a photo is downloaded from, to save. The server names the
// download after the file's own name.
export const photoFilePath = (photoId: string, photoFileRole: PhotoFileRole) =>
  `/api/v1/photos/${encodeURIComponent(photoId)}/files/${photoFileRole}`

// ---- Albums ----

export async function listPhotoAlbums(signal?: AbortSignal): Promise<PhotoAlbum[]> {
  const answer = await graphql<{ ListPhotoAlbums: PhotoAlbum[] }>(PHOTO_DOCUMENTS.ListPhotoAlbums, {}, signal)
  return answer.ListPhotoAlbums
}

export async function readPhotoAlbum(photoAlbumId: string, signal?: AbortSignal): Promise<PhotoAlbum> {
  const answer = await graphql<{ ReadPhotoAlbum: PhotoAlbum }>(PHOTO_DOCUMENTS.ReadPhotoAlbum, { photoAlbumId }, signal)
  return answer.ReadPhotoAlbum
}

export async function listPhotoAlbumShares(photoAlbumId: string, signal?: AbortSignal): Promise<PhotoAlbumShare[]> {
  const answer = await graphql<{ ListPhotoAlbumShares: PhotoAlbumShare[] }>(
    PHOTO_DOCUMENTS.ListPhotoAlbumShares,
    { photoAlbumId },
    signal,
  )
  return answer.ListPhotoAlbumShares
}

// Shares an album with one person or one circle, or changes the level they
// have: sharing again with the same grantee is how the server updates it.
export async function sharePhotoAlbum(
  photoAlbumId: string,
  grantee: { granteeUserId: string } | { granteeCircleId: string },
  shareLevel: 'view' | 'contribute',
  isLocationIncluded: boolean,
): Promise<PhotoAlbumShare> {
  const answer = await graphql<{ SharePhotoAlbum: PhotoAlbumShare }>(PHOTO_DOCUMENTS.SharePhotoAlbum, {
    photoAlbumId,
    granteeUserId: 'granteeUserId' in grantee ? grantee.granteeUserId : null,
    granteeCircleId: 'granteeCircleId' in grantee ? grantee.granteeCircleId : null,
    shareLevel,
    isLocationIncluded,
  })
  return answer.SharePhotoAlbum
}

export async function updatePhotoAlbumShare(
  photoAlbumShareId: string,
  shareLevel: 'view' | 'contribute',
  isLocationIncluded: boolean,
): Promise<PhotoAlbumShare> {
  const answer = await graphql<{ UpdatePhotoAlbumShare: PhotoAlbumShare }>(PHOTO_DOCUMENTS.UpdatePhotoAlbumShare, {
    photoAlbumShareId,
    shareLevel,
    isLocationIncluded,
  })
  return answer.UpdatePhotoAlbumShare
}

export async function removePhotoAlbumShare(photoAlbumShareId: string): Promise<void> {
  await graphql(PHOTO_DOCUMENTS.RemovePhotoAlbumShare, { photoAlbumShareId })
}

export async function suggestPhotoAlbums(signal?: AbortSignal): Promise<PhotoAlbumSuggestion[]> {
  const answer = await graphql<{ SuggestPhotoAlbums: PhotoAlbumSuggestion[] }>(
    PHOTO_DOCUMENTS.SuggestPhotoAlbums,
    {},
    signal,
  )
  return answer.SuggestPhotoAlbums
}

// The photos an album is made with, or has put in it, go in batches the
// server accepts: the first batch makes the album and the rest are added.
// With a circle's id the album is the circle's: every member adds to it.
export async function createPhotoAlbum(
  albumTitle: string,
  photoIds: string[],
  circleId: string | null = null,
): Promise<PhotoAlbum> {
  const answer = await graphql<{ CreatePhotoAlbum: PhotoAlbum }>(PHOTO_DOCUMENTS.CreatePhotoAlbum, {
    albumTitle,
    photoIds: photoIds.slice(0, MOST_PHOTO_IDS_PER_CALL),
    circleId,
  })
  if (photoIds.length > MOST_PHOTO_IDS_PER_CALL) {
    await addPhotosToAlbum(answer.CreatePhotoAlbum.id, photoIds.slice(MOST_PHOTO_IDS_PER_CALL))
  }
  return answer.CreatePhotoAlbum
}

export async function renamePhotoAlbum(photoAlbumId: string, albumTitle: string): Promise<PhotoAlbum> {
  const answer = await graphql<{ RenamePhotoAlbum: PhotoAlbum }>(PHOTO_DOCUMENTS.RenamePhotoAlbum, {
    photoAlbumId,
    albumTitle,
  })
  return answer.RenamePhotoAlbum
}

export async function deletePhotoAlbum(photoAlbumId: string): Promise<void> {
  await graphql(PHOTO_DOCUMENTS.DeletePhotoAlbum, { photoAlbumId })
}

async function changeAlbumPhotos(
  name: 'AddPhotosToAlbum' | 'RemovePhotosFromAlbum',
  photoAlbumId: string,
  photoIds: string[],
): Promise<number> {
  let changedCount = 0
  for (let start = 0; start < photoIds.length; start += MOST_PHOTO_IDS_PER_CALL) {
    const answer = await graphql<Record<string, number>>(PHOTO_DOCUMENTS[name], {
      photoAlbumId,
      photoIds: photoIds.slice(start, start + MOST_PHOTO_IDS_PER_CALL),
    })
    changedCount += answer[name]
  }
  return changedCount
}

// How many photos were put in: those already in the album are left alone and
// not counted.
export const addPhotosToAlbum = (photoAlbumId: string, photoIds: string[]) =>
  changeAlbumPhotos('AddPhotosToAlbum', photoAlbumId, photoIds)
export const removePhotosFromAlbum = (photoAlbumId: string, photoIds: string[]) =>
  changeAlbumPhotos('RemovePhotosFromAlbum', photoAlbumId, photoIds)

export async function setPhotoAlbumCover(photoAlbumId: string, photoId: string): Promise<PhotoAlbum> {
  const answer = await graphql<{ SetPhotoAlbumCover: PhotoAlbum }>(PHOTO_DOCUMENTS.SetPhotoAlbumCover, {
    photoAlbumId,
    photoId,
  })
  return answer.SetPhotoAlbumCover
}

// Every photo a suggestion covers, found the way the server found the run: by
// its dates. A photo of the range that is already in another album comes with
// them, which does no harm, an album holds references.
export async function listSuggestionPhotoIds(suggestion: PhotoAlbumSuggestion): Promise<string[]> {
  const filter: PhotoFilter = {
    listKind: 'library',
    searchText: '',
    takenAfter: suggestion.takenAfter,
    takenBefore: suggestion.takenBefore,
  }
  const photoIds: string[] = []
  for (;;) {
    const page = await listPhotos(filter, photoIds.length, MOST_PHOTO_IDS_PER_CALL)
    photoIds.push(...page.map((photo) => photo.id))
    if (page.length < MOST_PHOTO_IDS_PER_CALL) return photoIds
  }
}

// Where the small previews of some photos are, in one call: an object from a
// photo's id to its address. Photos with no small preview yet are left out.
// Not a Map: a query compares answers by their JSON, which a Map has none of.
export async function readSamplePreviews(photoIds: string[], signal?: AbortSignal): Promise<Record<string, string>> {
  const previews: Record<string, string> = {}
  if (photoIds.length === 0) return previews
  const variables: Record<string, string> = {}
  const selections = photoIds.map((photoId, index) => {
    variables[`photoId${index}`] = photoId
    return `sample${index}: ReadPhoto(photoId: $photoId${index}) { photo { id smallPreviewUrl } }`
  })
  const declarations = photoIds.map((_, index) => `$photoId${index}: String!`).join(', ')
  const answer = await graphql<Record<string, { photo: { id: string; smallPreviewUrl: string } }>>(
    `query (${declarations}) { ${selections.join(' ')} }`,
    variables,
    signal,
  )
  for (const view of Object.values(answer)) {
    if (view.photo.smallPreviewUrl) previews[view.photo.id] = view.photo.smallPreviewUrl
  }
  return previews
}

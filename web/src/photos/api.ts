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
}

export interface PhotoFile {
  photoFileRole: PhotoFileRole
  contentType: string
  fileSizeBytes: number
  originalFilename: string
  fileUrl: string
}

export interface PhotoView {
  photo: Photo
  photoFiles: PhotoFile[]
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
}

// Which of the library's three lists is shown.
export type PhotoListKind = 'library' | 'favorites' | 'trash'

// How many photos a page of the timeline holds.
export const PHOTOS_PER_PAGE = 200

const PHOTO_FIELDS =
  'id clientPhotoId photoKind photoSource createdAt takenAt takenAtOffsetMinutes widthPixels heightPixels durationMS latitude longitude placeName cameraMake cameraModel originalFilename isFavorite isScreenshot photoProcessingState processingErrorMessage trashedAt smallPreviewUrl largePreviewUrl videoPreviewUrl'

export const PHOTO_DOCUMENTS = {
  ListPhotos: `query ($offset: Int, $pageSize: Int, $isFavorite: Boolean, $isTrashed: Boolean, $searchText: String) { ListPhotos(offset: $offset, pageSize: $pageSize, isFavorite: $isFavorite, isTrashed: $isTrashed, searchText: $searchText) { ${PHOTO_FIELDS} } }`,
  CountPhotos:
    'query ($isFavorite: Boolean, $isTrashed: Boolean, $searchText: String) { CountPhotos(isFavorite: $isFavorite, isTrashed: $isTrashed, searchText: $searchText) }',
  ReadPhoto: `query ($photoId: String!) { ReadPhoto(photoId: $photoId) { photo { ${PHOTO_FIELDS} } photoFiles { photoFileRole contentType fileSizeBytes originalFilename fileUrl } } }`,
  ReadPhotoLibrary:
    'query { ReadPhotoLibrary { photoCount videoCount favoriteCount trashedCount usedBytes maximumFileBytes maximumAccountBytes trashRetentionDays uploadPath uploadPartBytes } }',
  FindPhotos:
    'query ($clientPhotoIds: [String!]!) { FindPhotos(clientPhotoIds: $clientPhotoIds) { id clientPhotoId } }',
  UpdatePhoto: `mutation ($photoId: String!, $isFavorite: Boolean) { UpdatePhoto(photoId: $photoId, isFavorite: $isFavorite) { ${PHOTO_FIELDS} } }`,
  TrashPhotos: 'mutation ($photoIds: [String!]!) { TrashPhotos(photoIds: $photoIds) }',
  RestorePhotos: 'mutation ($photoIds: [String!]!) { RestorePhotos(photoIds: $photoIds) }',
  DeletePhotos: 'mutation ($photoIds: [String!]!) { DeletePhotos(photoIds: $photoIds) }',
  EmptyPhotoTrash: 'mutation { EmptyPhotoTrash }',
}

// What the server accepts in one call that takes photo ids.
const MOST_PHOTO_IDS_PER_CALL = 500

export interface PhotoFilter {
  listKind: PhotoListKind
  searchText: string
}

function filterVariables(filter: PhotoFilter) {
  return {
    isFavorite: filter.listKind === 'favorites' ? true : null,
    isTrashed: filter.listKind === 'trash' ? true : null,
    searchText: filter.searchText || null,
  }
}

export async function listPhotos(
  filter: PhotoFilter,
  offset: number,
  pageSize: number,
  signal?: AbortSignal,
): Promise<Photo[]> {
  const answer = await graphql<{ ListPhotos: Photo[] }>(
    PHOTO_DOCUMENTS.ListPhotos,
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

export async function readPhotoLibrary(signal?: AbortSignal): Promise<PhotoLibrary> {
  const answer = await graphql<{ ReadPhotoLibrary: PhotoLibrary }>(PHOTO_DOCUMENTS.ReadPhotoLibrary, {}, signal)
  return answer.ReadPhotoLibrary
}

// The client ids, of those asked about, that the library already holds.
export async function findKnownClientPhotoIds(clientPhotoIds: string[], signal?: AbortSignal): Promise<Set<string>> {
  const known = new Set<string>()
  for (let start = 0; start < clientPhotoIds.length; start += MOST_PHOTO_IDS_PER_CALL) {
    const answer = await graphql<{ FindPhotos: { clientPhotoId: string }[] }>(
      PHOTO_DOCUMENTS.FindPhotos,
      { clientPhotoIds: clientPhotoIds.slice(start, start + MOST_PHOTO_IDS_PER_CALL) },
      signal,
    )
    for (const found of answer.FindPhotos) known.add(found.clientPhotoId)
  }
  return known
}

export async function setPhotoFavorite(photoId: string, isFavorite: boolean): Promise<Photo> {
  const answer = await graphql<{ UpdatePhoto: Photo }>(PHOTO_DOCUMENTS.UpdatePhoto, { photoId, isFavorite })
  return answer.UpdatePhoto
}

// The mutations that take photo ids send them in batches the server accepts,
// and say how many of them it acted on.
async function changePhotos(
  name: 'TrashPhotos' | 'RestorePhotos' | 'DeletePhotos',
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

export const trashPhotos = (photoIds: string[]) => changePhotos('TrashPhotos', photoIds)
export const restorePhotos = (photoIds: string[]) => changePhotos('RestorePhotos', photoIds)
export const deletePhotosForever = (photoIds: string[]) => changePhotos('DeletePhotos', photoIds)

export async function emptyPhotoTrash(): Promise<number> {
  const answer = await graphql<{ EmptyPhotoTrash: number }>(PHOTO_DOCUMENTS.EmptyPhotoTrash)
  return answer.EmptyPhotoTrash
}

// Where a file of a photo is downloaded from, to save. The server names the
// download after the file's own name.
export const photoFilePath = (photoId: string, photoFileRole: PhotoFileRole) =>
  `/api/v1/photos/${encodeURIComponent(photoId)}/files/${photoFileRole}`

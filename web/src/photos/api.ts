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

// The album a photo is in, as ReadPhoto says it: enough to name it and open it.
export interface PhotoAlbumReference {
  id: string
  albumTitle: string
}

export interface PhotoView {
  photo: Photo
  photoFiles: PhotoFile[]
  albums: PhotoAlbumReference[]
}

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
}

// Which of the library's three lists is shown.
export type PhotoListKind = 'library' | 'favorites' | 'trash'

// How many photos a page of the timeline holds.
export const PHOTOS_PER_PAGE = 200

const PHOTO_FIELDS =
  'id clientPhotoId photoKind photoSource createdAt takenAt takenAtOffsetMinutes widthPixels heightPixels durationMS latitude longitude placeName cameraMake cameraModel originalFilename isFavorite isScreenshot photoProcessingState processingErrorMessage trashedAt smallPreviewUrl largePreviewUrl videoPreviewUrl'

const PHOTO_ALBUM_FIELDS =
  'id albumTitle createdAt updatedAt coverPhotoId photoCount firstTakenAt lastTakenAt coverPreviewUrl'

export const PHOTO_DOCUMENTS = {
  ListPhotos: `query ($offset: Int, $pageSize: Int, $isFavorite: Boolean, $isTrashed: Boolean, $searchText: String, $albumId: String, $takenAfter: DateTime, $takenBefore: DateTime) { ListPhotos(offset: $offset, pageSize: $pageSize, isFavorite: $isFavorite, isTrashed: $isTrashed, searchText: $searchText, albumId: $albumId, takenAfter: $takenAfter, takenBefore: $takenBefore) { ${PHOTO_FIELDS} } }`,
  CountPhotos:
    'query ($isFavorite: Boolean, $isTrashed: Boolean, $searchText: String, $albumId: String, $takenAfter: DateTime, $takenBefore: DateTime) { CountPhotos(isFavorite: $isFavorite, isTrashed: $isTrashed, searchText: $searchText, albumId: $albumId, takenAfter: $takenAfter, takenBefore: $takenBefore) }',
  ReadPhoto: `query ($photoId: String!) { ReadPhoto(photoId: $photoId) { photo { ${PHOTO_FIELDS} } photoFiles { photoFileRole contentType fileSizeBytes originalFilename fileUrl } albums { id albumTitle } } }`,
  ReadPhotoLibrary:
    'query { ReadPhotoLibrary { photoCount videoCount favoriteCount trashedCount usedBytes maximumFileBytes maximumAccountBytes trashRetentionDays uploadPath uploadPartBytes } }',
  FindPhotos:
    'query ($clientPhotoIds: [String!]!) { FindPhotos(clientPhotoIds: $clientPhotoIds) { id clientPhotoId } }',
  UpdatePhoto: `mutation ($photoId: String!, $isFavorite: Boolean) { UpdatePhoto(photoId: $photoId, isFavorite: $isFavorite) { ${PHOTO_FIELDS} } }`,
  TrashPhotos: 'mutation ($photoIds: [String!]!) { TrashPhotos(photoIds: $photoIds) }',
  RestorePhotos: 'mutation ($photoIds: [String!]!) { RestorePhotos(photoIds: $photoIds) }',
  DeletePhotos: 'mutation ($photoIds: [String!]!) { DeletePhotos(photoIds: $photoIds) }',
  EmptyPhotoTrash: 'mutation { EmptyPhotoTrash }',
  ListPhotoAlbums: `query { ListPhotoAlbums { ${PHOTO_ALBUM_FIELDS} } }`,
  SuggestPhotoAlbums:
    'query { SuggestPhotoAlbums { takenAfter takenBefore placeNames albumTitle photoCount samplePhotoIds } }',
  ReadPhotoAlbum: `query ($photoAlbumId: String!) { ReadPhotoAlbum(photoAlbumId: $photoAlbumId) { ${PHOTO_ALBUM_FIELDS} } }`,
  CreatePhotoAlbum: `mutation ($albumTitle: String!, $photoIds: [String!]) { CreatePhotoAlbum(albumTitle: $albumTitle, photoIds: $photoIds) { ${PHOTO_ALBUM_FIELDS} } }`,
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
  // Keeps the photos taken in a range, for the photos of a suggestion.
  takenAfter?: string
  takenBefore?: string
}

function filterVariables(filter: PhotoFilter) {
  return {
    isFavorite: filter.listKind === 'favorites' ? true : null,
    isTrashed: filter.listKind === 'trash' ? true : null,
    searchText: filter.searchText || null,
    albumId: filter.photoAlbumId || null,
    takenAfter: filter.takenAfter || null,
    takenBefore: filter.takenBefore || null,
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

// ---- Albums ----

export async function listPhotoAlbums(signal?: AbortSignal): Promise<PhotoAlbum[]> {
  const answer = await graphql<{ ListPhotoAlbums: PhotoAlbum[] }>(PHOTO_DOCUMENTS.ListPhotoAlbums, {}, signal)
  return answer.ListPhotoAlbums
}

export async function readPhotoAlbum(photoAlbumId: string, signal?: AbortSignal): Promise<PhotoAlbum> {
  const answer = await graphql<{ ReadPhotoAlbum: PhotoAlbum }>(PHOTO_DOCUMENTS.ReadPhotoAlbum, { photoAlbumId }, signal)
  return answer.ReadPhotoAlbum
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
export async function createPhotoAlbum(albumTitle: string, photoIds: string[]): Promise<PhotoAlbum> {
  const answer = await graphql<{ CreatePhotoAlbum: PhotoAlbum }>(PHOTO_DOCUMENTS.CreatePhotoAlbum, {
    albumTitle,
    photoIds: photoIds.slice(0, MOST_PHOTO_IDS_PER_CALL),
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

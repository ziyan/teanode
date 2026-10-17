import { authorization } from '../api'
import { translateNow } from '../i18n/i18n'
import { RecordingSha256 } from '../recording/sha256'

// Sending one file to the library over the server's resumable upload
// endpoint (internal/photo/upload.go): a POST that creates the upload and
// carries the first part, then PATCHes from wherever the server says it is.
// A connection that is cut costs the part in flight and nothing more, since
// the server keeps every whole part and says, to a HEAD, how far it got.

export const PHOTO_UPLOADS_PATH = '/api/v1/photos/uploads'
export const DEFAULT_PART_BYTES = 8 * 1024 * 1024

// How many times a request is repeated, after asking the server where the
// upload is, before the file is given up on.
const MOST_TRIES = 3
const BACKOFF_MS = [500, 2000, 5000]

// Files up to this size are hashed in one call to the browser's own SHA-256,
// which is native and fast. Bigger ones are read in slices through the
// incremental hash written for the recordings, because SubtleCrypto cannot
// hash a stream and a video of several gigabytes cannot be held in memory.
// Either way the file's real SHA-256 is its client id, so the same file sent
// from here or from the command line is found by FindPhotos and skipped.
const HASH_WHOLE_FILE_BYTES = 256 * 1024 * 1024

// An error with the words the server gave, for a toast.
export class PhotoUploadError extends Error {
  readonly httpStatusCode: number | null
  constructor(message: string, httpStatusCode: number | null = null) {
    super(message)
    this.name = 'PhotoUploadError'
    this.httpStatusCode = httpStatusCode
  }
}

const EXTENSION_TYPES: Record<string, string> = {
  jpg: 'image/jpeg',
  jpeg: 'image/jpeg',
  png: 'image/png',
  gif: 'image/gif',
  webp: 'image/webp',
  avif: 'image/avif',
  heic: 'image/heic',
  heif: 'image/heif',
  tif: 'image/tiff',
  tiff: 'image/tiff',
  mp4: 'video/mp4',
  m4v: 'video/mp4',
  mov: 'video/quicktime',
  webm: 'video/webm',
  mkv: 'video/x-matroska',
  avi: 'video/x-msvideo',
  '3gp': 'video/3gpp',
}

// The media type of a file. A browser leaves the type of a HEIC picture or a
// QuickTime movie empty, so the extension says it then.
export function contentTypeOf(file: Pick<File, 'name' | 'type'>): string {
  if (file.type) return file.type
  const extension = file.name.includes('.') ? file.name.split('.').pop()!.toLowerCase() : ''
  return EXTENSION_TYPES[extension] ?? ''
}

// Whether the library can take a file: a picture or a video.
export function isPhotoFile(file: Pick<File, 'name' | 'type'>): boolean {
  const contentType = contentTypeOf(file)
  return contentType.startsWith('image/') || contentType.startsWith('video/')
}

function toHex(bytes: Uint8Array): string {
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
}

// sha256Of is the digest of a file as 64 hexadecimal digits. The callback is
// told how much has been read, so a big video shows that it is being worked
// on; it is not called for a file hashed in one piece.
export async function sha256Of(
  file: Blob,
  onProgress: (readBytes: number) => void = () => {},
  signal?: AbortSignal,
): Promise<string> {
  if (file.size <= HASH_WHOLE_FILE_BYTES && typeof crypto !== 'undefined' && crypto.subtle) {
    return toHex(new Uint8Array(await crypto.subtle.digest('SHA-256', await file.arrayBuffer())))
  }
  const digest = new RecordingSha256()
  for (let start = 0; start < file.size; start += DEFAULT_PART_BYTES) {
    if (signal?.aborted) throw new DOMException('The upload was canceled.', 'AbortError')
    digest.update(new Uint8Array(await file.slice(start, start + DEFAULT_PART_BYTES).arrayBuffer()))
    onProgress(Math.min(file.size, start + DEFAULT_PART_BYTES))
  }
  return digest.hex()
}

export interface UploadOptions {
  signal?: AbortSignal
  // How big a part is; the server's own figure, from ReadPhotoLibrary.
  partBytes?: number
  // Called with the bytes the server has, each time that grows.
  onProgress?: (sentBytes: number) => void
  // Replaceable so that a test runs without waiting and without a network.
  fetchImpl?: typeof fetch
  sleep?: (milliseconds: number) => Promise<void>
}

export interface UploadedPhoto {
  photoId: string
}

const defaultSleep = (milliseconds: number) => new Promise<void>((resolve) => window.setTimeout(resolve, milliseconds))

function isAbort(caught: unknown): boolean {
  return caught instanceof DOMException && caught.name === 'AbortError'
}

async function problemOf(response: Response): Promise<PhotoUploadError> {
  let text = ''
  try {
    text = (await response.text()).trim()
  } catch {
    // A body that cannot be read leaves the status to speak.
  }
  return new PhotoUploadError(
    text && text.length < 300 ? text : translateNow('api.serverReturned', { statusCode: response.status }),
    response.status,
  )
}

function readOffset(response: Response): number | null {
  const value = response.headers.get('Upload-Offset')
  if (value === null || value === '') return null
  const offset = Number(value)
  return Number.isFinite(offset) && offset >= 0 ? offset : null
}

// The state of an upload as an answer says it.
interface UploadState {
  offsetBytes: number
  photoId: string | null
}

// uploadPhotoFile sends a file, hashed already, and says which photo it
// became. clientPhotoId is what the library calls the photo, and the digest
// is sent so that the server refuses a file that was changed on the way.
export async function uploadPhotoFile(
  file: File,
  clientPhotoId: string,
  sha256: string,
  options: UploadOptions = {},
): Promise<UploadedPhoto> {
  const { signal, onProgress = () => {}, sleep = defaultSleep } = options
  const partBytes = options.partBytes && options.partBytes > 0 ? options.partBytes : DEFAULT_PART_BYTES
  const send = options.fetchImpl ?? ((...parameters: Parameters<typeof fetch>) => fetch(...parameters))
  let uploadPath = ''

  // A request that finishes the file answers 201 with the photo's id in
  // X-Photo-ID; one that does not answers with the offset.
  async function readState(response: Response): Promise<UploadState> {
    const location = response.headers.get('Location')
    // The address the server gave is absolute and may name a host a proxy
    // hides; the path is the part that is the same from here.
    if (location) uploadPath = new URL(location, window.location.origin).pathname
    const photoId = response.headers.get('X-Photo-ID')
    const offsetBytes = readOffset(response)
    if (photoId && (response.headers.get('Upload-Complete') === '?1' || offsetBytes === null)) {
      return { offsetBytes: file.size, photoId }
    }
    if (offsetBytes === null) throw new PhotoUploadError(translateNow('photos.uploadNoOffset'), response.status)
    return { offsetBytes, photoId: null }
  }

  function headers(extra: Record<string, string>): Record<string, string> {
    return { ...extra, ...authorization() }
  }

  async function create(): Promise<UploadState> {
    const isWhole = file.size <= partBytes
    const response = await send(PHOTO_UPLOADS_PATH, {
      method: 'POST',
      headers: headers({
        'Content-Type': contentTypeOf(file) || 'application/octet-stream',
        'Upload-Complete': isWhole ? '?1' : '?0',
        'Upload-Length': String(file.size),
        'X-Photo-Client-ID': clientPhotoId,
        // Header values are text; a name with other characters is sent as
        // it is read, and the server refuses control characters only.
        'X-Photo-Filename': encodeHeaderText(file.name),
        'X-Photo-Source': 'dashboard',
        'X-Photo-Declared-SHA256': sha256,
      }),
      body: file.slice(0, partBytes),
      signal,
    })
    if (!response.ok) throw await problemOf(response)
    return readState(response)
  }

  // Where the upload is, asked of the server: after a cut connection the
  // answer to the request in flight is lost, and the bytes are not.
  async function ask(): Promise<UploadState> {
    const response = await send(uploadPath, { method: 'HEAD', headers: headers({}), signal })
    if (!response.ok) throw await problemOf(response)
    return readState(response)
  }

  async function append(offsetBytes: number): Promise<UploadState> {
    const end = Math.min(file.size, offsetBytes + partBytes)
    const response = await send(uploadPath, {
      method: 'PATCH',
      headers: headers({
        'Content-Type': 'application/partial-upload',
        'Upload-Offset': String(offsetBytes),
        'Upload-Complete': end >= file.size ? '?1' : '?0',
      }),
      body: file.slice(offsetBytes, end),
      signal,
    })
    // The upload is somewhere else than this request thought, and says
    // where: carry on from there.
    if (response.status === 409) {
      const actualOffset = readOffset(response)
      if (actualOffset === null) throw await problemOf(response)
      return { offsetBytes: actualOffset, photoId: null }
    }
    if (!response.ok) throw await problemOf(response)
    return readState(response)
  }

  // Tries a request, and after a failure that may be the connection's asks
  // the server where the upload is and goes on from the answer. A refusal
  // from the server (a 4xx) is final, and so is a cancel.
  async function withRecovery(request: () => Promise<UploadState>, hasUploadPath: boolean): Promise<UploadState> {
    for (let attempt = 0; ; attempt++) {
      try {
        return await request()
      } catch (caught) {
        if (isAbort(caught) || signal?.aborted) throw caught
        const status = caught instanceof PhotoUploadError ? caught.httpStatusCode : null
        const isFinal = status !== null && status >= 400 && status < 500 && status !== 408 && status !== 429
        if (isFinal || attempt + 1 >= MOST_TRIES) {
          throw caught instanceof PhotoUploadError
            ? caught
            : new PhotoUploadError(translateNow('photos.uploadConnection'))
        }
        await sleep(BACKOFF_MS[Math.min(attempt, BACKOFF_MS.length - 1)])
        if (hasUploadPath && uploadPath) {
          try {
            return await ask()
          } catch (askCaught) {
            if (isAbort(askCaught) || signal?.aborted) throw askCaught
            // Asked again on the next turn of the loop, by the request.
          }
        }
      }
    }
  }

  let state = await withRecovery(create, false)
  onProgress(Math.min(state.offsetBytes, file.size))
  while (state.photoId === null) {
    if (state.offsetBytes >= file.size) {
      // Every byte is there and the server has not said it is complete: ask.
      state = await withRecovery(ask, true)
      if (state.photoId === null && state.offsetBytes >= file.size) {
        throw new PhotoUploadError(translateNow('photos.uploadNotCompleted'))
      }
      continue
    }
    const offsetBytes = state.offsetBytes
    state = await withRecovery(() => append(offsetBytes), true)
    onProgress(Math.min(state.offsetBytes, file.size))
  }
  return { photoId: state.photoId }
}

// A header carries bytes, and the server reads it as UTF-8 text. fetch only
// sends Latin-1 values, so a name with other characters would be refused by
// the browser before it left; those are sent as their UTF-8 bytes read as
// Latin-1, which is what the server reads back.
export function encodeHeaderText(text: string): string {
  if (/^[\x20-\x7e]*$/.test(text)) return text
  return Array.from(new TextEncoder().encode(text), (byte) => String.fromCharCode(byte)).join('')
}

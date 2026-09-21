import { authorization } from './api'
// Uploading files with progress.
//
// fetch cannot report how much of a body has been sent, and XMLHttpRequest
// can, so this is the one place the old API is used. One request carries a
// whole selection of files as multipart/form-data; the parts go in order,
// so the byte count as it rises says which file is on the wire and how far
// along it is.

export type UploadProgress = {
  // Bytes sent so far, over the whole request, and the whole request.
  sent: number
  total: number

  // For each file, how far along it is, 0 to 1, in the order given.
  files: number[]
}

export type UploadHandle = {
  promise: Promise<unknown>
  cancel: () => void
}

export function uploadFiles(
  method: 'PUT' | 'POST',
  url: string,
  files: File[],
  onProgress: (progress: UploadProgress) => void,
): UploadHandle {
  const request = new XMLHttpRequest()
  const body = new FormData()
  for (const file of files) {
    body.append('file', file, file.name)
  }
  const sizes = files.map((file) => file.size)
  const totalBytes = sizes.reduce((sum, size) => sum + size, 0)

  const promise = new Promise<unknown>((resolve, reject) => {
    request.upload.onprogress = (event) => {
      if (!event.lengthComputable) {
        return
      }
      // The body carries multipart boundaries and headers besides the
      // files; the files' share of the bytes sent is taken proportionally,
      // which is close enough for a bar.
      const share = totalBytes > 0 ? Math.min(1, event.loaded / Math.max(1, event.total)) : 1
      let remaining = share * totalBytes
      const perFile = sizes.map((size) => {
        const done = Math.max(0, Math.min(size, remaining))
        remaining -= done
        return size === 0 ? 1 : done / size
      })
      onProgress({ sent: event.loaded, total: event.total, files: perFile })
    }
    request.onload = () => {
      let parsed: unknown = null
      try {
        parsed = request.responseText ? JSON.parse(request.responseText) : null
      } catch {
        parsed = null
      }
      if (request.status >= 200 && request.status < 300) {
        onProgress({ sent: totalBytes, total: totalBytes, files: sizes.map(() => 1) })
        resolve(parsed)
      } else {
        const message =
          parsed && typeof parsed === 'object' && 'error' in parsed && typeof (parsed as { error: unknown }).error === 'string'
            ? (parsed as { error: string }).error
            : request.statusText || `HTTP ${request.status}`
        reject(new Error(message))
      }
    }
    request.onerror = () => reject(new Error('The upload failed.'))
    request.onabort = () => reject(new DOMException('The upload was canceled.', 'AbortError'))
    request.open(method, url)
    request.setRequestHeader('Accept', 'application/json')
    for (const [name, value] of Object.entries(authorization())) request.setRequestHeader(name, value)
    request.send(body)
  })

  return { promise, cancel: () => request.abort() }
}

export function isCancelled(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'AbortError'
}

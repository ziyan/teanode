import { describe, expect, it, vi } from 'vitest'
import { PhotoUploadError, contentTypeOf, encodeHeaderText, isPhotoFile, sha256Of, uploadPhotoFile } from './uploader'

// A small stand-in for the server's upload endpoint: it keeps the bytes it
// was sent, answers the way internal/photo/upload.go does, and can be told to
// lose the answer to one request (the bytes are stored, the connection drops).
function fakeServer(options: { loseAnswerToPatch?: number; refuseWith?: { status: number; text: string } } = {}) {
  let storedBytes = 0
  let patchCount = 0
  let isComplete = false
  const requests: { method: string; path: string; headers: Record<string, string> }[] = []
  const handler = vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const headers = (init?.headers ?? {}) as Record<string, string>
    const method = init?.method ?? 'GET'
    const path = String(input)
    requests.push({ method, path, headers })
    const sizeOf = (body: unknown) => (body instanceof Blob ? body.size : 0)
    const state = (status: number) =>
      new Response(isComplete ? '{"photoId":"photo-1"}' : null, {
        status,
        headers: {
          Location: 'http://internal.example:8080/api/v1/photos/uploads/upload-1',
          'Upload-Offset': String(storedBytes),
          'Upload-Complete': isComplete ? '?1' : '?0',
          ...(isComplete ? { 'X-Photo-ID': 'photo-1' } : {}),
        },
      })
    if (options.refuseWith) return new Response(options.refuseWith.text, { status: options.refuseWith.status })
    if (method === 'POST') {
      storedBytes += sizeOf(init?.body)
      isComplete = headers['Upload-Complete'] === '?1'
      return state(201)
    }
    if (method === 'HEAD') return state(204)
    if (method === 'PATCH') {
      patchCount++
      if (Number(headers['Upload-Offset']) !== storedBytes) {
        return new Response('the upload is at ' + storedBytes, {
          status: 409,
          headers: { 'Upload-Offset': String(storedBytes) },
        })
      }
      storedBytes += sizeOf(init?.body)
      isComplete = headers['Upload-Complete'] === '?1'
      if (patchCount === options.loseAnswerToPatch) throw new TypeError('network error')
      return state(isComplete ? 201 : 204)
    }
    throw new Error('unexpected ' + method)
  })
  return { handler, requests, stored: () => storedBytes }
}

const fileOf = (size: number, name = 'picture.jpg', type = 'image/jpeg') =>
  new File([new Uint8Array(size)], name, { type })
const noWait = async () => {}

describe('uploadPhotoFile', () => {
  it('sends a file that fits in one part in a single request', async () => {
    const server = fakeServer()
    const result = await uploadPhotoFile(fileOf(10), 'client-1', 'a'.repeat(64), {
      partBytes: 16,
      fetchImpl: server.handler as unknown as typeof fetch,
      sleep: noWait,
    })
    expect(result.photoId).toBe('photo-1')
    expect(server.requests.map((request) => request.method)).toEqual(['POST'])
    expect(server.requests[0].headers).toMatchObject({
      'Upload-Complete': '?1',
      'X-Photo-Client-ID': 'client-1',
      'X-Photo-Source': 'dashboard',
      'X-Photo-Declared-SHA256': 'a'.repeat(64),
      'Content-Type': 'image/jpeg',
    })
  })

  it('sends the rest in patches from the offset, the last one marked complete', async () => {
    const server = fakeServer()
    const progress: number[] = []
    await uploadPhotoFile(fileOf(40), 'client-1', 'b'.repeat(64), {
      partBytes: 16,
      onProgress: (sentBytes) => progress.push(sentBytes),
      fetchImpl: server.handler as unknown as typeof fetch,
      sleep: noWait,
    })
    const patches = server.requests.filter((request) => request.method === 'PATCH')
    expect(patches.map((request) => request.headers['Upload-Offset'])).toEqual(['16', '32'])
    expect(patches.map((request) => request.headers['Upload-Complete'])).toEqual(['?0', '?1'])
    expect(patches[0].headers['Content-Type']).toBe('application/partial-upload')
    // The address is taken as a path of this server, not the host it names.
    expect(patches[0].path).toBe('/api/v1/photos/uploads/upload-1')
    expect(server.stored()).toBe(40)
    expect(progress).toEqual([16, 32, 40])
  })

  it('asks where the upload is after a lost answer and continues without resending', async () => {
    const server = fakeServer({ loseAnswerToPatch: 1 })
    const sleep = vi.fn(noWait)
    const result = await uploadPhotoFile(fileOf(40), 'client-1', 'c'.repeat(64), {
      partBytes: 16,
      fetchImpl: server.handler as unknown as typeof fetch,
      sleep,
    })
    expect(result.photoId).toBe('photo-1')
    expect(server.requests.map((request) => request.method)).toEqual(['POST', 'PATCH', 'HEAD', 'PATCH'])
    // The patch after the HEAD starts at 32, where the server said it was,
    // not at 16 where the lost answer left the client.
    expect(server.requests[3].headers['Upload-Offset']).toBe('32')
    expect(server.stored()).toBe(40)
    expect(sleep).toHaveBeenCalledTimes(1)
  })

  it('gives up after three tries when the connection stays down', async () => {
    const handler = vi.fn(async () => {
      throw new TypeError('network error')
    })
    await expect(
      uploadPhotoFile(fileOf(10), 'client-1', 'd'.repeat(64), {
        partBytes: 16,
        fetchImpl: handler as unknown as typeof fetch,
        sleep: noWait,
      }),
    ).rejects.toBeInstanceOf(PhotoUploadError)
    expect(handler).toHaveBeenCalledTimes(3)
  })

  it('does not retry a refusal and says what the server said', async () => {
    const server = fakeServer({ refuseWith: { status: 413, text: 'the library is full' } })
    await expect(
      uploadPhotoFile(fileOf(10), 'client-1', 'e'.repeat(64), {
        partBytes: 16,
        fetchImpl: server.handler as unknown as typeof fetch,
        sleep: noWait,
      }),
    ).rejects.toThrow('the library is full')
    expect(server.handler).toHaveBeenCalledTimes(1)
  })
})

describe('file helpers', () => {
  it('hashes a file with SHA-256', async () => {
    expect(await sha256Of(new Blob(['abc']))).toBe('ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad')
  })

  it('names the type of a HEIC picture the browser left empty', () => {
    expect(contentTypeOf({ name: 'IMG_0001.HEIC', type: '' })).toBe('image/heic')
    expect(contentTypeOf({ name: 'clip.mov', type: '' })).toBe('video/quicktime')
    expect(contentTypeOf({ name: 'picture.jpg', type: 'image/png' })).toBe('image/png')
  })

  it('accepts pictures and videos and nothing else', () => {
    expect(isPhotoFile({ name: 'a.heic', type: '' })).toBe(true)
    expect(isPhotoFile({ name: 'a.mp4', type: 'video/mp4' })).toBe(true)
    expect(isPhotoFile({ name: 'a.pdf', type: 'application/pdf' })).toBe(false)
    expect(isPhotoFile({ name: 'notes', type: '' })).toBe(false)
  })

  it('sends a name with other characters as its UTF-8 bytes', () => {
    expect(encodeHeaderText('plain.jpg')).toBe('plain.jpg')
    expect(encodeHeaderText('é').length).toBe(2)
  })
})

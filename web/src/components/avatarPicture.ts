// What a picture is made into before it leaves the browser. The server decodes
// PNG and JPEG only, and a phone's photo is often HEIC or a camera's WebP, so
// whatever the browser can show is drawn onto a canvas and sent as a JPEG. The
// canvas also drops every metadata block, the location a phone writes into a
// photo included, before the server has seen it.

// The longest side of what is sent. The server keeps 512 pixels; more than
// this only makes the upload slow.
export const AVATAR_LONGEST_SIDE_PIXELS = 2048

// The quality of the JPEG sent: high, because the server scales it down and
// encodes it again.
const AVATAR_JPEG_QUALITY = 0.9

// fitWithin is the size to draw a picture at so its longer side is at most the
// limit, keeping its proportions. A picture already within the limit keeps its
// size; it is never enlarged.
export function fitWithin(width: number, height: number, longestSidePixels: number): { width: number; height: number } {
  const longest = Math.max(width, height)
  if (longest <= longestSidePixels) {
    return { width, height }
  }
  const scale = longestSidePixels / longest
  return { width: Math.max(1, Math.round(width * scale)), height: Math.max(1, Math.round(height * scale)) }
}

// decode opens a file as something that can be drawn. createImageBitmap first,
// because it applies the photo's orientation and is the one that reads HEIC
// where the browser can; an image element when it is not there.
async function decode(
  file: File,
): Promise<{ source: CanvasImageSource; width: number; height: number; release: () => void }> {
  if (typeof createImageBitmap === 'function') {
    try {
      const bitmap = await createImageBitmap(file, { imageOrientation: 'from-image' })
      return { source: bitmap, width: bitmap.width, height: bitmap.height, release: () => bitmap.close() }
    } catch {
      // Fall through to the image element, which can read what the bitmap
      // decoder refuses on some browsers.
    }
  }
  const url = URL.createObjectURL(file)
  try {
    const image = new Image()
    image.src = url
    await image.decode()
    return {
      source: image,
      width: image.naturalWidth,
      height: image.naturalHeight,
      release: () => URL.revokeObjectURL(url),
    }
  } catch (caught) {
    URL.revokeObjectURL(url)
    throw caught
  }
}

// pictureToJpegBase64 is the picture as base64 of a JPEG at most 2048 pixels on
// its long side, which is how the server's SetUserAvatar takes bytes. It
// rejects when the browser cannot decode the file.
export async function pictureToJpegBase64(file: File): Promise<string> {
  const decoded = await decode(file)
  try {
    const size = fitWithin(decoded.width, decoded.height, AVATAR_LONGEST_SIDE_PIXELS)
    const canvas = document.createElement('canvas')
    canvas.width = size.width
    canvas.height = size.height
    const context = canvas.getContext('2d')
    if (!context) {
      throw new Error('canvas unavailable')
    }
    // A JPEG has no transparency, and a transparent PNG would come out black.
    context.fillStyle = '#ffffff'
    context.fillRect(0, 0, size.width, size.height)
    context.drawImage(decoded.source, 0, 0, size.width, size.height)
    const blob = await new Promise<Blob>((resolve, reject) => {
      canvas.toBlob(
        (made) => (made ? resolve(made) : reject(new Error('could not encode the picture'))),
        'image/jpeg',
        AVATAR_JPEG_QUALITY,
      )
    })
    return await new Promise<string>((resolve, reject) => {
      const reader = new FileReader()
      reader.onerror = () => reject(reader.error)
      reader.onload = () => {
        const dataUrl = String(reader.result)
        resolve(dataUrl.slice(dataUrl.indexOf(',') + 1))
      }
      reader.readAsDataURL(blob)
    })
  } finally {
    decoded.release()
  }
}

import { useRef, useState } from 'react'

import { useTranslation } from '../i18n/i18n'
import { PictureIcon } from './icons'

// Uploading a picture, in one place, because three editors need it and they
// put the result in three different ways: the rich text editor writes an <img>
// at the cursor, and the two source editors write the tag as text where the
// caret is in a textarea.

export type UploadedMedia = {
  id: string
  filename: string
  contentType: string
  size: number
  url: string
}

export function useMediaUpload(domainId?: string) {
  const { t } = useTranslation()
  const [uploading, setUploading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function upload(file: File): Promise<UploadedMedia | null> {
    if (!domainId) {
      return null
    }
    setError(null)
    setUploading(true)
    try {
      const body = new FormData()
      body.append('file', file)
      body.append('domainId', domainId)
      const response = await fetch('/api/v1/media', { method: 'POST', body, credentials: 'same-origin' })
      if (!response.ok) {
        // The server says what is wrong in words meant for a person — too
        // large, not a picture — so they are shown rather than replaced with
        // something vaguer.
        setError((await response.text()).trim() || t('richText.uploadFailed'))
        return null
      }
      return (await response.json()) as UploadedMedia
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : t('richText.uploadFailed'))
      return null
    } finally {
      setUploading(false)
    }
  }

  return { upload, uploading, error, setError }
}

// MediaButton is the button and the hidden file input together. The caller
// decides what to do with the picture once it is up.
export function MediaButton({
  domainId,
  onUploaded,
  onMouseDown,
  label,
}: {
  domainId?: string
  onUploaded: (media: UploadedMedia) => void
  // Called before the file dialog opens, for an editor that has to remember
  // where the cursor was: opening the dialog takes the focus away.
  onMouseDown?: () => void
  label?: string
}) {
  const { t } = useTranslation()
  const picker = useRef<HTMLInputElement>(null)
  const { upload, uploading, error } = useMediaUpload(domainId)

  if (!domainId) {
    return null
  }

  return (
    <>
      <button
        type="button"
        title={t('richText.picture')}
        aria-label={t('richText.picture')}
        disabled={uploading}
        onMouseDown={(event) => {
          event.preventDefault()
          onMouseDown?.()
          picker.current?.click()
        }}
      >
        <PictureIcon size={16} />
        {label && <span>{label}</span>}
      </button>
      <input
        ref={picker}
        type="file"
        accept="image/png,image/jpeg,image/gif,image/webp"
        hidden
        onChange={(event) => {
          const file = event.target.files?.[0]
          // Cleared so that choosing the same file twice in a row still fires.
          event.target.value = ''
          if (!file) {
            return
          }
          void upload(file).then((media) => {
            if (media) {
              onUploaded(media)
            }
          })
        }}
      />
      {error && <span className="error richtext-error">{error}</span>}
    </>
  )
}

// imageTag is what goes into a template for an uploaded picture. The alt text
// is the filename, which is better than nothing for a reader whose mail
// program does not load pictures, and is something the operator can edit.
export function imageTag(media: UploadedMedia): string {
  return `<img src="${escapeAttribute(media.url)}" alt="${escapeAttribute(media.filename)}">`
}

function escapeAttribute(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

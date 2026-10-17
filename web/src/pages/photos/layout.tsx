import { useEffect, useRef } from 'react'
import { Outlet, useNavigate } from 'react-router-dom'
import { useToast } from '../../components/toast'
import { useTranslation } from '../../i18n/i18n'
import { restorePhotos } from '../../photos/api'
import { announcePhotosChanged } from '../../photos/changes'
import { photoUploads, useUploadItems } from '../../photos/uploadQueue'
import { useSession } from '../../session'
import { UploadTray } from './uploadTray'

// How long after a file finishes the open page is told, so that a burst of
// files finishing together is one read.
const UPLOADED_REFRESH_MS = 800

// Every page of Photos sits inside this one. The upload queue outlives a page,
// so what it has to say does too: the tray with each file's progress, the
// toasts for what finished or failed, and the warning before leaving the tab
// while files are still on their way. Mounted here once, they are there on
// Albums, People and a photo's own address as they are on the library, and a
// page that was left does not take them with it.
export function PhotosLayout() {
  const session = useSession()
  return <AccountPhotosLayout key={session.userId ?? ''} userId={session.userId ?? ''} />
}

function AccountPhotosLayout({ userId }: { userId: string }) {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const navigate = useNavigate()
  const uploadItems = useUploadItems()
  const isUploading = uploadItems.some(
    (item) => item.state === 'waiting' || item.state === 'hashing' || item.state === 'sending',
  )

  // Another account signing in on this browser does not inherit the queue.
  useEffect(() => {
    photoUploads.setOwner(userId)
  }, [userId])

  // Leaving the tab while files are on their way loses them.
  useEffect(() => {
    if (!isUploading) return
    const warn = (event: BeforeUnloadEvent) => event.preventDefault()
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [isUploading])

  const speak = useRef({ toast, t, plural, navigate })
  speak.current = { toast, t, plural, navigate }
  useEffect(() => {
    let refreshTimer: number | null = null
    let hasToldOfFailure = false
    const stopListening = photoUploads.onEvent((event) => {
      const { toast: toaster, t: translate, plural: pluralize } = speak.current
      if (event.kind === 'itemDone') {
        if (refreshTimer !== null) window.clearTimeout(refreshTimer)
        refreshTimer = window.setTimeout(announcePhotosChanged, UPLOADED_REFRESH_MS)
      } else if (event.kind === 'itemFailed') {
        // The tray lists every failure; one toast says that there was one.
        if (!hasToldOfFailure) {
          hasToldOfFailure = true
          toaster.failed(
            translate('photos.uploadFailedFile', { name: event.item.name, message: event.item.errorMessage }),
          )
        }
      } else {
        hasToldOfFailure = false
        const { uploadedCount, skippedCount, trashedCount, trashedPhotoIds, failedCount } = event.summary
        const sentences = [
          uploadedCount > 0 && pluralize(uploadedCount, { one: 'photos.uploadedOne', other: 'photos.uploadedOther' }),
          skippedCount > 0 && pluralize(skippedCount, { one: 'photos.skippedOne', other: 'photos.skippedOther' }),
          trashedCount > 0 &&
            pluralize(trashedCount, { one: 'photos.uploadInTrashOne', other: 'photos.uploadInTrashOther' }),
          failedCount > 0 &&
            pluralize(failedCount, { one: 'photos.failedUploadOne', other: 'photos.failedUploadOther' }),
        ].filter(Boolean)
        // A file that is in the Trash can be brought back from the toast that
        // says so.
        const restoreAction =
          trashedPhotoIds.length > 0
            ? {
                label: translate('photos.restore'),
                run: async () => {
                  try {
                    const restoredCount = await restorePhotos(trashedPhotoIds)
                    speak.current.toast.done(
                      speak.current.plural(restoredCount, { one: 'photos.restoredOne', other: 'photos.restoredOther' }),
                      {
                        label: speak.current.t('photos.showInLibrary'),
                        run: () =>
                          speak.current.navigate(
                            trashedPhotoIds.length === 1
                              ? `/photos/${encodeURIComponent(trashedPhotoIds[0])}`
                              : '/photos',
                          ),
                      },
                    )
                    announcePhotosChanged()
                  } catch (caught) {
                    speak.current.toast.failure(caught, speak.current.t('photos.actionFailed'))
                  }
                },
              }
            : undefined
        if (failedCount > 0) toaster.failed(sentences.join(' '), restoreAction)
        else toaster.done(sentences.join(' '), restoreAction)
        announcePhotosChanged()
      }
    })
    return () => {
      stopListening()
      if (refreshTimer !== null) window.clearTimeout(refreshTimer)
    }
  }, [])

  return (
    <>
      <Outlet />
      <UploadTray />
    </>
  )
}

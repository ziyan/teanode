import { useState } from 'react'
import { SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { readPhotoLibrary, type PhotoLibrary } from '../../photos/api'
import { setPhotoReading } from '../../photos/people'
import { hasPermission, useSession } from '../../session'

// PhotoReadingSetting is the person's own switch for having each new picture
// read as it arrives: described, and recorded in Finance when it is a receipt,
// a statement or a tax document. Turning it off deletes nothing, so it does
// not ask first.
export function PhotoReadingSetting({
  info,
  isCard,
  onChanged,
}: {
  info: PhotoLibrary
  isCard?: boolean
  onChanged: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [isBusy, setIsBusy] = useState(false)
  const isEnabled = info.isPhotoReadingEnabled

  async function change(isNowEnabled: boolean) {
    setIsBusy(true)
    try {
      await setPhotoReading(isNowEnabled)
      toast.done(isNowEnabled ? t('photos.readingTurnedOn') : t('photos.readingTurnedOff'))
      onChanged()
    } catch (caught) {
      toast.failure(caught, t('photos.actionFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <SettingsSection card={isCard} title={t('photos.readingTitle')}>
      <label className="checkbox is-plain">
        <input
          type="checkbox"
          checked={isEnabled}
          disabled={isBusy || !info.isDescriptionAvailable}
          onChange={(event) => void change(event.target.checked)}
        />
        <span>{t('photos.readingSwitch')}</span>
      </label>
      <p className="muted field-hint">
        {info.isDescriptionAvailable ? t('photos.readingHint') : info.descriptionUnavailableMessage}
      </p>
    </SettingsSection>
  )
}

// The preferences card: shown to a person who may use Photos, once their
// library has been read.
export function PhotoReadingPreferences() {
  const session = useSession()
  const isUsingPhotos = session.services?.isPhotosEnabled === true && hasPermission(session.permissions, 'photos:use')
  const library = useQuery(
    async () => (isUsingPhotos ? await readPhotoLibrary() : null),
    [isUsingPhotos, session.userId],
  )
  if (!isUsingPhotos || !library.data) return null
  return <PhotoReadingSetting info={library.data} isCard onChanged={() => void library.reload(true)} />
}

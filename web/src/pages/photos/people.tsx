import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { ErrorMessage, Loading } from '../../components/common'
import { ConfirmDialog } from '../../components/dialog'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { readPhotoLibrary, type PhotoLibrary } from '../../photos/api'
import { listPhotoFaceGroups, setPhotoFaceRecognition, type PhotoFaceGroup } from '../../photos/people'
import { hasPermission, useSession } from '../../session'
import { PhotoSections } from './albumControls'
import { sectionAddress } from './albums'
import { FacePicture, personAddress, useDisplayedPersonName } from './personControls'

// The People page: a round picture for each person the server found in the
// photos, with the name of the contact the person gave it, or an invitation
// to name it. Nothing here names anybody by itself: a group is nobody until
// the person chooses a contact.

export function PhotoPeoplePage() {
  const session = useSession()
  return <AccountPeoplePage key={session.userId ?? ''} />
}

function AccountPeoplePage() {
  const { t, plural } = useTranslation()
  const navigate = useNavigate()
  const displayedName = useDisplayedPersonName()
  const [isHiddenShown, setIsHiddenShown] = useState(false)
  const library = useQuery(() => readPhotoLibrary(), [])
  const groups = useQuery(() => listPhotoFaceGroups(isHiddenShown), [isHiddenShown])
  const list = groups.data ?? []
  const hasHiddenOnes = isHiddenShown && list.some((group) => group.isHidden)
  const info = library.data
  const emptyText = !info
    ? t('photos.emptyPeople')
    : !info.isFaceRecognitionAvailable
      ? info.faceRecognitionUnavailableMessage || t('photos.facesUnavailable')
      : !info.isPhotoFaceRecognitionEnabled
        ? t('photos.emptyPeopleOff')
        : t('photos.emptyPeople')

  return (
    <div className="photos-page">
      <ErrorMessage error={groups.error} />
      <div className="photo-toolbar">
        <PhotoSections current="people" onSelect={(section) => navigate(sectionAddress(section))} />
        <label className="checkbox photo-show-hidden">
          <input type="checkbox" checked={isHiddenShown} onChange={(event) => setIsHiddenShown(event.target.checked)} />
          <span>{t('photos.showHidden')}</span>
        </label>
      </div>

      {info && info.isFaceRecognitionAvailable && !info.isPhotoFaceRecognitionEnabled && (
        <FaceRecognitionSetting info={info} isCard onChanged={() => void library.reload(true)} />
      )}

      {groups.loading && !groups.data ? (
        <Loading />
      ) : list.length === 0 ? (
        <SettingsEmpty>{emptyText}</SettingsEmpty>
      ) : (
        <>
          {hasHiddenOnes && <p className="muted photo-section-hint">{t('photos.hiddenHint')}</p>}
          <ul className="photo-person-grid">
            {list.map((group) => (
              <li key={group.id}>
                <PersonCell group={group} name={displayedName(group)} plural={plural} />
              </li>
            ))}
          </ul>
        </>
      )}
    </div>
  )
}

// One person: the round picture, the name under it (or what to do about the
// lack of one, in muted text) and how many photos they are in.
function PersonCell({
  group,
  name,
  plural,
}: {
  group: PhotoFaceGroup
  name: string
  plural: ReturnType<typeof useTranslation>['plural']
}) {
  const isNamed = group.contactName.trim() !== ''
  return (
    <Link className={group.isHidden ? 'photo-person-cell is-hidden' : 'photo-person-cell'} to={personAddress(group.id)}>
      <FacePicture facePictureUrl={group.facePictureUrl} name={name} size="large" />
      <span className={isNamed ? 'photo-person-name' : 'photo-person-name muted'}>{name}</span>
      <span className="photo-person-details muted">
        {plural(group.photoCount, { one: 'photos.itemCountOne', other: 'photos.itemCountOther' })}
      </span>
    </Link>
  )
}

// FaceRecognitionSetting is the person's own switch for finding faces in
// their photos. It is on the People page while it is off, and among the
// account's preferences. Turning it off deletes the faces found so far with
// the names given to them, so that asks first; turning it on has the
// photos read again.
export function FaceRecognitionSetting({
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
  const [isConfirmingOff, setIsConfirmingOff] = useState(false)
  const isEnabled = info.isPhotoFaceRecognitionEnabled

  async function change(isNowEnabled: boolean) {
    setIsBusy(true)
    try {
      await setPhotoFaceRecognition(isNowEnabled)
      setIsConfirmingOff(false)
      toast.done(isNowEnabled ? t('photos.facesTurnedOn') : t('photos.facesTurnedOff'))
      onChanged()
    } catch (caught) {
      toast.failure(caught, t('photos.actionFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <SettingsSection card={isCard} title={t('photos.facesTitle')} description={t('photos.facesHint')}>
      <label className="checkbox">
        <input
          type="checkbox"
          checked={isEnabled}
          disabled={isBusy || !info.isFaceRecognitionAvailable}
          onChange={(event) => (event.target.checked ? void change(true) : setIsConfirmingOff(true))}
        />
        <span>{t('photos.facesSwitch')}</span>
      </label>
      <p className="muted field-hint">
        {info.isFaceRecognitionAvailable
          ? t('photos.facesOffWarning')
          : info.faceRecognitionUnavailableMessage || t('photos.facesUnavailable')}
      </p>
      {isConfirmingOff && (
        <ConfirmDialog
          title={t('photos.facesOffTitle')}
          body={t('photos.facesOffBody')}
          confirmLabel={t('photos.facesOffConfirm')}
          busy={isBusy}
          onConfirm={() => void change(false)}
          onClose={() => setIsConfirmingOff(false)}
        />
      )}
    </SettingsSection>
  )
}

// The preferences card: shown to a person who may use Photos, once their
// library has been read.
export function FaceRecognitionPreferences() {
  const session = useSession()
  const isUsingPhotos = session.services?.isPhotosEnabled === true && hasPermission(session.permissions, 'photos:use')
  const library = useQuery(
    async () => (isUsingPhotos ? await readPhotoLibrary() : null),
    [isUsingPhotos, session.userId],
  )
  if (!isUsingPhotos || !library.data) return null
  return <FaceRecognitionSetting info={library.data} isCard onChanged={() => void library.reload(true)} />
}

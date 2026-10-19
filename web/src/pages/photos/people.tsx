import { useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { ErrorMessage, Loading, Tag } from '../../components/common'
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
import {
  FacePicture,
  LinkedFacePicture,
  NamePersonFlow,
  YouMark,
  personAddress,
  useDisplayedPersonName,
} from './personControls'
import { PhotoToolbar } from './toolbar'

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
  // Whether hidden people are shown is in the address (?hidden=1), so that a
  // reload, Back and a copied link all keep it.
  const [parameters, setParameters] = useSearchParams()
  const isHiddenShown = parameters.get('hidden') === '1'
  const setIsHiddenShown = (isShown: boolean) =>
    setParameters((previous) => {
      const written = new URLSearchParams(previous)
      if (isShown) written.set('hidden', '1')
      else written.delete('hidden')
      return written
    })
  const [naming, setNaming] = useState<PhotoFaceGroup | null>(null)
  const session = useSession()
  const canNamePeople =
    session.services?.isContactsEnabled === true && hasPermission(session.permissions, 'contacts:use')
  const library = useQuery(() => readPhotoLibrary(), [])
  // Everybody is asked for, hidden people included, so that the page knows
  // whether there is anybody hidden: the switch is only drawn when it would
  // reveal somebody, and a library where everybody is hidden still has it. It
  // also stays while it is on, so that an address with ?hidden=1 can be undone.
  const groups = useQuery(() => listPhotoFaceGroups(true), [])
  const everybody = groups.data ?? []
  const hiddenCount = everybody.filter((group) => group.isHidden).length
  const list = isHiddenShown ? everybody : everybody.filter((group) => !group.isHidden)
  const hasHiddenOnes = isHiddenShown && hiddenCount > 0
  // When somebody in the list is in the person's circles every tile keeps
  // room for the second line that says so, so that the tiles stay one height.
  const hasCirclePeople = list.some((group) => Boolean(group.account))
  const isWaitingForGroups = groups.loading && !groups.data
  const info = library.data
  const emptyText = !info
    ? t('photos.emptyPeople')
    : !info.isFaceRecognitionAvailable
      ? t('photos.facesUnavailable')
      : !info.isPhotoFaceRecognitionEnabled
        ? t('photos.emptyPeopleOff')
        : // People were found but every one is hidden, which is not the same as none found.
          hiddenCount > 0 && !isHiddenShown
          ? t('photos.everyoneHidden')
          : t('photos.emptyPeople')

  return (
    <div className="photos-page">
      <ErrorMessage error={groups.error} />
      <PhotoSections current="people" onSelect={(section) => navigate(sectionAddress(section))} />
      {/* Whether "Show hidden people" is drawn is known only when the groups
          arrive. Until then the row's height is kept, so the grid does not
          move down when the switch appears. */}
      {isWaitingForGroups ? (
        <div className="photo-toolbar" aria-hidden="true" />
      ) : (
        <PhotoToolbar
          tools={
            (hiddenCount > 0 || isHiddenShown) && (
              <label className="checkbox is-plain photo-show-hidden">
                <input
                  type="checkbox"
                  checked={isHiddenShown}
                  onChange={(event) => setIsHiddenShown(event.target.checked)}
                />
                <span>{t('photos.showHidden')}</span>
              </label>
            )
          }
        />
      )}

      {info && info.isFaceRecognitionAvailable && !info.isPhotoFaceRecognitionEnabled && (
        <FaceRecognitionSetting info={info} isCard onChanged={() => void library.reload(true)} />
      )}

      {isWaitingForGroups ? (
        <Loading />
      ) : list.length === 0 ? (
        <SettingsEmpty>{emptyText}</SettingsEmpty>
      ) : (
        <>
          {hasHiddenOnes && <p className="muted photo-section-hint">{t('photos.hiddenHint')}</p>}
          <ul className={hasCirclePeople ? 'photo-person-grid has-circle-people' : 'photo-person-grid'}>
            {list.map((group) => (
              <li key={group.id}>
                <PersonCell
                  group={group}
                  name={displayedName(group)}
                  plural={plural}
                  canName={canNamePeople}
                  onName={() => setNaming(group)}
                />
              </li>
            ))}
          </ul>
        </>
      )}
      {naming && (
        <NamePersonFlow group={naming} onClose={() => setNaming(null)} onNamed={() => void groups.reload(true)} />
      )}
    </div>
  )
}

// One person: the round picture, the name under it, and how many photos they
// are in. For a person who is nobody yet, and whom the reader may name, the
// Name button takes the place of the name line rather than adding a line, so
// that every tile is the same height whether or not it is named. A hidden
// person is dimmed, picture and name, and says so in a tag, so that dimmed is
// not the only way it is told. The tile's link is the picture, the name and
// the count; for a person who is offered the Name button the link is the
// picture and the button is beside it, since a button in a link is neither.
function PersonCell({
  group,
  name,
  plural,
  canName,
  onName,
}: {
  group: PhotoFaceGroup
  name: string
  plural: ReturnType<typeof useTranslation>['plural']
  canName: boolean
  onName: () => void
}) {
  const { t } = useTranslation()
  const isNamed = group.contactName.trim() !== ''
  const isOfferedName = !isNamed && canName
  const photoCountText = plural(group.photoCount, { one: 'photos.itemCountOne', other: 'photos.itemCountOther' })
  return (
    <div className={group.isHidden ? 'photo-person-cell is-hidden' : 'photo-person-cell'}>
      {isOfferedName ? (
        <>
          <Link className="photo-person-link" to={personAddress(group.id)} aria-label={`${name}, ${photoCountText}`}>
            <FacePicture facePictureUrl={group.facePictureUrl} name={name} size="large" />
          </Link>
          <span className="photo-person-title">
            <button type="button" className="photo-person-name-button" onClick={onName}>
              {t('photos.nameAction')}
            </button>
          </span>
          <span className="photo-person-details muted">{photoCountText}</span>
        </>
      ) : (
        <Link className="photo-person-link" to={personAddress(group.id)}>
          <LinkedFacePicture facePictureUrl={group.facePictureUrl} name={name} account={group.account} />
          <span className="photo-person-title">
            <span className={isNamed ? 'photo-person-name' : 'photo-person-name muted'}>{name}</span>
            {group.isOwnContact && <YouMark />}
          </span>
          <span className="photo-person-details muted">
            <span>{photoCountText}</span>
            {group.account && <span>{t('photos.inYourCircles')}</span>}
          </span>
        </Link>
      )}
      {group.isHidden && <Tag value={t('photos.hiddenTag')} />}
    </div>
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
      <label className="checkbox is-plain">
        <input
          type="checkbox"
          checked={isEnabled}
          disabled={isBusy || !info.isFaceRecognitionAvailable}
          onChange={(event) => (event.target.checked ? void change(true) : setIsConfirmingOff(true))}
        />
        <span>{t('photos.facesSwitch')}</span>
      </label>
      <p className="muted field-hint">
        {info.isFaceRecognitionAvailable ? t('photos.facesOffWarning') : t('photos.facesUnavailable')}
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

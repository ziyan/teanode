import { useMemo, useState } from 'react'
import { Avatar } from '../../components/avatar'
import { Tag } from '../../components/common'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { CheckIcon, CloseIcon, PencilIcon, PeopleIcon } from '../../components/icons'
import { Pager } from '../../components/pager'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import {
  addPhotosToAlbum,
  countPhotos,
  listPhotoAlbumShares,
  listPhotos,
  removePhotoAlbumShare,
  sharePhotoAlbum,
  type PhotoAlbum,
  type PhotoAlbumShare,
} from '../../photos/api'
import { circlePersonName, listCircleChoices, listCirclePeople } from '../../photos/sharing'

// Sharing an album, and adding to one that is shared. The levels, what a reader
// is shown, and why photos leave an album with the access that put them there
// are in docs/decisions/20261016-an-album-is-shared-at-a-level.md. The server
// refuses anybody the person shares no circle with and answers as not found,
// so the choices here are only the people and circles it would accept.

type ShareLevel = 'view' | 'contribute'
const SHARE_LEVELS: ShareLevel[] = ['view', 'contribute']

// A choice is "user:<id>" or "circle:<id>": one string for the radio group.
const choiceOfShare = (share: PhotoAlbumShare) =>
  share.granteeCircleId ? `circle:${share.granteeCircleId}` : `user:${share.granteeUserId}`

// Who an album belongs to, for a card and for the head of its page: the
// owner's face and name, or the circle's name for a circle's album.
export function AlbumOwnerMark({ album }: { album: PhotoAlbum }) {
  if (album.circleId) {
    return (
      <span className="photo-owner-mark">
        <span className="photo-circle-chip" aria-hidden="true">
          <PeopleIcon size={12} />
        </span>
        <span className="photo-owner-name">{album.circleName}</span>
      </span>
    )
  }
  return (
    <span className="photo-owner-mark">
      <Avatar
        name={album.ownerName}
        avatarUrl={album.ownerAvatarUrl}
        colorIndex={album.ownerAvatarColorIndex}
        size={18}
      />
      <span className="photo-owner-name">{album.ownerName}</span>
    </span>
  )
}

// ShareAlbumDialog lists who the album is shared with, at what level, with a
// way to change or end each, and a form to share it with somebody else. Choosing
// somebody it is shared with already fills the form with what they have, and
// sharing again changes it: the server keeps one share for each person.
export function ShareAlbumDialog({
  album,
  onClose,
  onChanged,
}: {
  album: PhotoAlbum
  onClose: () => void
  // Called after the shares changed, so that the page can read them again.
  onChanged: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const shares = useQuery(() => listPhotoAlbumShares(album.id), [album.id], { refresh: false })
  const choices = useQuery(
    async () => {
      const [people, circles] = await Promise.all([listCirclePeople(), listCircleChoices()])
      return { people, circles }
    },
    [],
    { refresh: false },
  )
  const [choice, setChoice] = useState('')
  const [shareLevel, setShareLevel] = useState<ShareLevel>('view')
  const [isLocationIncluded, setIsLocationIncluded] = useState(false)
  const [isBusy, setIsBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const [stopping, setStopping] = useState<PhotoAlbumShare | null>(null)

  const current = useMemo(() => shares.data ?? [], [shares.data])
  const people = choices.data?.people ?? []
  const circles = choices.data?.circles ?? []
  const everyoneIn = (name: string) => t('photos.shareEveryoneIn', { name })
  const nameOfShare = (share: PhotoAlbumShare) =>
    share.granteeCircleId ? everyoneIn(share.granteeCircleName) : share.granteeName || share.granteeUsername
  const nameOfChoice = (key: string) => {
    const [kind, id] = key.split(':')
    if (kind === 'circle') return everyoneIn(circles.find((each) => each.id === id)?.name ?? '')
    const person = people.find((each) => each.userId === id)
    return person ? circlePersonName(person) : ''
  }
  const existing = current.find((share) => choiceOfShare(share) === choice) ?? null

  function choose(key: string) {
    setChoice(key)
    setProblem(null)
    const found = current.find((share) => choiceOfShare(share) === key)
    setShareLevel(found?.shareLevel ?? 'view')
    setIsLocationIncluded(found?.isLocationIncluded ?? false)
  }

  async function run(work: () => Promise<unknown>, done: string): Promise<boolean> {
    setIsBusy(true)
    setProblem(null)
    try {
      await work()
      toast.done(done)
      await shares.reload(true)
      onChanged()
      return true
    } catch (caught) {
      setProblem(caught instanceof Error ? caught.message : String(caught))
      toast.failure(caught, t('photos.actionFailed'))
      return false
    } finally {
      setIsBusy(false)
    }
  }

  async function submit() {
    const [kind, id] = choice.split(':')
    if (!id) return
    const name = nameOfChoice(choice)
    const isDone = await run(
      () =>
        sharePhotoAlbum(
          album.id,
          kind === 'circle' ? { granteeCircleId: id } : { granteeUserId: id },
          shareLevel,
          isLocationIncluded,
        ),
      existing ? t('photos.shareChanged', { name }) : t('photos.shareSaid', { name }),
    )
    if (isDone) choose('')
  }

  async function stop(share: PhotoAlbumShare) {
    const name = nameOfShare(share)
    const isDone = await run(() => removePhotoAlbumShare(share.id), t('photos.shareStopped', { name }))
    if (isDone) {
      setStopping(null)
      if (choiceOfShare(share) === choice) choose('')
    }
  }

  // Taking a share back ends the access of everybody it reached, and their
  // photos in the album go with it, so it is asked about first. Shown instead
  // of the form rather than over it: two dialogs would both close on Escape.
  if (stopping) {
    const name = nameOfShare(stopping)
    return (
      <ConfirmDialog
        title={t('photos.shareStopTitle', { name })}
        body={t('photos.shareStopBody', { name, title: album.albumTitle })}
        confirmLabel={t('photos.shareStop')}
        busy={isBusy}
        error={problem}
        onConfirm={() => void stop(stopping)}
        onClose={() => setStopping(null)}
      />
    )
  }

  const hasNobodyToChoose = choices.data !== null && people.length === 0 && circles.length === 0
  return (
    <FormDialog
      title={t('photos.shareTitle', { title: album.albumTitle })}
      submitLabel={existing ? t('common.save') : t('photos.shareAdd')}
      closeLabel={t('common.done')}
      busy={isBusy}
      error={problem}
      canSubmit={choice !== ''}
      onClose={onClose}
      onSubmit={() => void submit()}
    >
      <p className="muted">{t('photos.shareIntro')}</p>

      {shares.data && current.length === 0 ? (
        <p className="muted">{t('photos.shareNobody')}</p>
      ) : (
        current.length > 0 && (
          <>
            <h4 className="photo-share-heading">{t('photos.shareCurrent')}</h4>
            <ul className="photo-share-list" aria-label={t('photos.shareCurrent')}>
              {current.map((share) => {
                const name = nameOfShare(share)
                return (
                  <li key={share.id} className="photo-share-row">
                    {share.granteeCircleId ? (
                      <span className="photo-circle-chip photo-circle-chip-large" aria-hidden="true">
                        <PeopleIcon size={16} />
                      </span>
                    ) : (
                      <Avatar
                        name={name}
                        avatarUrl={share.granteeAvatarUrl}
                        colorIndex={share.granteeAvatarColorIndex}
                        size={28}
                      />
                    )}
                    <span className="photo-share-name">{name}</span>
                    <span className="photo-share-tags">
                      <Tag
                        value={t(share.shareLevel === 'contribute' ? 'photos.levelContribute' : 'photos.levelView')}
                      />
                      {share.isLocationIncluded && <Tag value={t('photos.sharePlacesIncluded')} />}
                    </span>
                    <span className="row-actions">
                      <Tooltip label={t('photos.shareChange', { name })}>
                        <button
                          type="button"
                          className="icon-action"
                          aria-label={t('photos.shareChange', { name })}
                          disabled={isBusy}
                          onClick={() => choose(choiceOfShare(share))}
                        >
                          <PencilIcon size={16} />
                        </button>
                      </Tooltip>
                      <Tooltip label={t('photos.shareStopFor', { name })}>
                        <button
                          type="button"
                          className="icon-action danger"
                          aria-label={t('photos.shareStopFor', { name })}
                          disabled={isBusy}
                          onClick={() => setStopping(share)}
                        >
                          <CloseIcon size={16} />
                        </button>
                      </Tooltip>
                    </span>
                  </li>
                )
              })}
            </ul>
          </>
        )
      )}

      {hasNobodyToChoose ? (
        <p className="muted">{t('photos.shareNoPeople')}</p>
      ) : (
        <>
          <h4 className="photo-share-heading" id="photo-share-with-heading">
            {t('photos.shareWith')}
          </h4>
          <div className="photo-share-choices" role="radiogroup" aria-labelledby="photo-share-with-heading">
            {people.map((person) => {
              const key = `user:${person.userId}`
              return (
                <button
                  key={key}
                  type="button"
                  role="radio"
                  aria-checked={choice === key}
                  className={choice === key ? 'photo-share-choice is-chosen' : 'photo-share-choice'}
                  onClick={() => choose(key)}
                >
                  <Avatar
                    name={circlePersonName(person)}
                    avatarUrl={person.avatarUrl}
                    colorIndex={person.avatarColorIndex}
                    size={28}
                  />
                  <span className="photo-share-choice-name">{circlePersonName(person)}</span>
                  <span className="photo-share-choice-check">{choice === key && <CheckIcon size={16} />}</span>
                </button>
              )
            })}
            {circles.map((circle) => {
              const key = `circle:${circle.id}`
              return (
                <button
                  key={key}
                  type="button"
                  role="radio"
                  aria-checked={choice === key}
                  className={choice === key ? 'photo-share-choice is-chosen' : 'photo-share-choice'}
                  onClick={() => choose(key)}
                >
                  <span className="photo-circle-chip photo-circle-chip-large" aria-hidden="true">
                    <PeopleIcon size={16} />
                  </span>
                  <span className="photo-share-choice-name">{everyoneIn(circle.name)}</span>
                  <span className="photo-share-choice-check">{choice === key && <CheckIcon size={16} />}</span>
                </button>
              )
            })}
          </div>

          <h4 className="photo-share-heading" id="photo-share-level-heading">
            {t('photos.shareLevelLabel')}
          </h4>
          <div className="photo-share-levels" role="radiogroup" aria-labelledby="photo-share-level-heading">
            {SHARE_LEVELS.map((level) => (
              <div key={level} className="photo-share-option">
                <label className="checkbox is-plain">
                  <input
                    type="radio"
                    name="photo-share-level"
                    checked={shareLevel === level}
                    onChange={() => setShareLevel(level)}
                  />
                  <span>{t(level === 'contribute' ? 'photos.levelContribute' : 'photos.levelView')}</span>
                </label>
                <p className="muted photo-share-option-hint">
                  {t(level === 'contribute' ? 'photos.levelContributeHint' : 'photos.levelViewHint')}
                </p>
              </div>
            ))}
          </div>

          <div className="photo-share-option">
            <label className="checkbox is-plain">
              <input
                type="checkbox"
                checked={isLocationIncluded}
                onChange={(event) => setIsLocationIncluded(event.target.checked)}
              />
              <span>{t('photos.shareIncludePlaces')}</span>
            </label>
            <p className="muted photo-share-option-hint">{t('photos.shareIncludePlacesHint')}</p>
          </div>
          <p className="muted photo-share-option-hint photo-share-names-hint">{t('photos.sharePeopleNamesHint')}</p>
        </>
      )}
    </FormDialog>
  )
}

// How many of the person's photos the picker shows on a page.
const PICKER_PAGE_SIZE = 60

// AddPhotosDialog picks photos from the person's own library to put in an album
// they contribute to. The choice is kept across pages, and the photos that are
// in the album already are left alone by the server.
export function AddPhotosDialog({
  album,
  onClose,
  onAdded,
}: {
  album: PhotoAlbum
  onClose: () => void
  onAdded: () => void
}) {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const [pageIndex, setPageIndex] = useState(0)
  const [chosenIds, setChosenIds] = useState<ReadonlySet<string>>(new Set())
  const [isBusy, setIsBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const filter = useMemo(() => ({ listKind: 'library' as const, searchText: '' }), [])
  const page = useQuery(() => listPhotos(filter, pageIndex * PICKER_PAGE_SIZE, PICKER_PAGE_SIZE), [filter, pageIndex], {
    refresh: false,
  })
  const count = useQuery(() => countPhotos(filter), [filter], { refresh: false })
  const photos = page.data ?? []

  function toggle(photoId: string) {
    setChosenIds((previous) => {
      const next = new Set(previous)
      if (next.has(photoId)) next.delete(photoId)
      else next.add(photoId)
      return next
    })
  }

  async function submit() {
    setIsBusy(true)
    setProblem(null)
    try {
      const addedCount = await addPhotosToAlbum(album.id, Array.from(chosenIds))
      toast.done(
        addedCount === 0
          ? t('photos.alreadyInAlbum', { title: album.albumTitle })
          : plural(
              addedCount,
              { one: 'photos.addedToAlbumOne', other: 'photos.addedToAlbumOther' },
              { title: album.albumTitle },
            ),
      )
      onAdded()
      onClose()
    } catch (caught) {
      setProblem(caught instanceof Error ? caught.message : String(caught))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <FormDialog
      title={t('photos.addPhotosTitle', { title: album.albumTitle })}
      submitLabel={
        chosenIds.size > 0
          ? plural(chosenIds.size, { one: 'photos.addPhotosCountOne', other: 'photos.addPhotosCountOther' })
          : t('photos.addPhotos')
      }
      busy={isBusy}
      error={problem}
      canSubmit={chosenIds.size > 0}
      wide
      onClose={onClose}
      onSubmit={() => void submit()}
    >
      <p className="muted">{t('photos.addPhotosHint')}</p>
      {page.data && photos.length === 0 ? (
        <p className="muted">{t('photos.emptyLibrary')}</p>
      ) : (
        <div className="photo-picker" role="group" aria-label={t('photos.addPhotosPicker')}>
          {photos.map((photo) => {
            const isChosen = chosenIds.has(photo.id)
            return (
              <button
                key={photo.id}
                type="button"
                className={isChosen ? 'photo-picker-cell is-selected' : 'photo-picker-cell'}
                aria-pressed={isChosen}
                aria-label={t('photos.selectPhoto', {
                  name: photo.originalFilename || photo.placeName || t('nav.photos'),
                })}
                onClick={() => toggle(photo.id)}
              >
                {photo.smallPreviewUrl ? (
                  <img src={photo.smallPreviewUrl} alt="" loading="lazy" draggable={false} />
                ) : (
                  <span className="photo-picker-blank" />
                )}
                <span className="photo-check-box photo-picker-check" aria-hidden="true">
                  {isChosen && <CheckIcon size={14} />}
                </span>
              </button>
            )
          })}
        </div>
      )}
      {(count.data ?? 0) > PICKER_PAGE_SIZE && (
        <div className="list-foot">
          <span />
          <Pager
            pageIndex={pageIndex}
            pageSize={PICKER_PAGE_SIZE}
            shownCount={photos.length}
            totalCount={count.data ?? 0}
            isLoading={page.loading}
            onPageIndex={setPageIndex}
          />
        </div>
      )}
    </FormDialog>
  )
}

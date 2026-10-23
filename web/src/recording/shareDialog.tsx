import { useState } from 'react'
import { Link } from 'react-router-dom'

import { failureText, graphql } from '../api'
import { Tag } from '../components/common'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { TrashIcon } from '../components/icons'
import { personLabel, personName } from '../components/personLabel'
import { useToast } from '../components/toast'
import { Tooltip } from '../components/tooltip'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'
import {
  RECORDING_SHARE_DOCUMENTS,
  RECORDING_SHARE_LEVELS,
  type RecordingShare,
  type RecordingShareLevel,
} from './sharing'

// Sharing one recording: with a person who shares a circle with the owner, or
// with a whole circle, at a level. The server refuses anybody the owner shares
// no circle with and answers as not found, so the choices here are only the
// people and circles it would accept. The people who speak in a recording did
// not agree to its being shared, which the dialog says before anything is
// chosen.

type Person = { userId: string; username: string; name: string; avatarUrl: string | null; avatarColorIndex: number }
type CircleChoice = { id: string; name: string; members: { userId: string }[] }

const LIST_PEOPLE = `query { ListCirclePeople { userId username name avatarUrl avatarColorIndex } }`
const LIST_CIRCLES = `query { ListCircles { id name members { userId } } }`

function shareName(share: RecordingShare, everyoneIn: (name: string) => string): string {
  if (share.granteeCircleId) return everyoneIn(share.granteeCircleName)
  return share.granteeName || share.granteeUsername
}

// RecordingShareDialog lists who the recording is shared with, at what level,
// with a way to stop, and a form to share it with somebody else.
export function RecordingShareDialog({ recordingId, onClose }: { recordingId: string; onClose: () => void }) {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const shares = useQuery(
    () =>
      graphql<{ ListRecordingShares: RecordingShare[] }>(RECORDING_SHARE_DOCUMENTS.ListRecordingShares, {
        recordingId,
      }),
    [recordingId],
    { refresh: false },
  )
  const choices = useQuery(
    async () => {
      const [people, circles] = await Promise.all([
        graphql<{ ListCirclePeople: Person[] }>(LIST_PEOPLE),
        graphql<{ ListCircles: CircleChoice[] }>(LIST_CIRCLES),
      ])
      return { people: people.ListCirclePeople, circles: circles.ListCircles }
    },
    [],
    { refresh: false },
  )
  const [choice, setChoice] = useState('')
  const [recordingShareLevel, setRecordingShareLevel] = useState<RecordingShareLevel>('summary')
  const [isBusy, setIsBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  // The share being ended, asked about first.
  const [stopping, setStopping] = useState<RecordingShare | null>(null)

  const current = shares.data?.ListRecordingShares ?? []
  const people = choices.data?.people ?? []
  const circles = choices.data?.circles ?? []
  const everyoneIn = (name: string) => t('recordings.shareEveryoneIn', { name })
  // A choice also says how many people it is, so two circles with one name
  // can be told apart, as they are on the command line and in the agent's tools.
  const circleOption = (circle: CircleChoice) =>
    t('recordings.shareCircleOption', {
      name: circle.name,
      people: plural(
        circle.members.length,
        { one: 'circles.memberCountOne', other: 'circles.memberCount' },
        { count: circle.members.length },
      ),
    })

  async function run(work: () => Promise<unknown>, done: string): Promise<boolean> {
    setIsBusy(true)
    setProblem(null)
    try {
      await work()
      toast.done(done)
      await shares.reload()
      return true
    } catch (failure) {
      // The dialog is open, so the failure is said in it, once, and the dialog
      // stays up with what was chosen.
      setProblem(failureText(failure, t('recordings.shareFailed')))
      return false
    } finally {
      setIsBusy(false)
    }
  }

  function add() {
    const [kind, id] = choice.split(':')
    if (!id) return
    const person = people.find((each) => each.userId === id)
    const name =
      kind === 'circle'
        ? everyoneIn(circles.find((each) => each.id === id)?.name ?? '')
        : person
          ? personName(person)
          : ''
    void run(
      () =>
        graphql(RECORDING_SHARE_DOCUMENTS.ShareRecording, {
          recordingId,
          granteeUserId: kind === 'user' ? id : null,
          granteeCircleId: kind === 'circle' ? id : null,
          recordingShareLevel,
        }),
      t('recordings.shareSaid', { name }),
    ).then((isDone) => isDone && setChoice(''))
  }

  return (
    <>
      <FormDialog
        title={t('recordings.shareTitle')}
        submitLabel={t('recordings.shareAdd')}
        closeLabel={t('common.done')}
        busy={isBusy}
        error={stopping ? null : problem}
        canSubmit={choice !== ''}
        onClose={onClose}
        onSubmit={add}
      >
        <p className="notice warn">{t('recordings.shareConsent')}</p>

        <h4 className="recording-shares-heading">{t('recordings.shareCurrent')}</h4>
        <div className="recording-shares" aria-label={t('recordings.shareCurrent')}>
          {shares.data && current.length === 0 && <p className="muted">{t('recordings.shareNobody')}</p>}
          {current.map((share) => {
            const name = shareName(share, everyoneIn)
            return (
              <div key={share.id} className="recording-share-row">
                <span className="recording-share-name">{name}</span>
                <Tag value={t(`recordings.shareLevelName.${share.recordingShareLevel}` as Parameters<typeof t>[0])} />
                <Tooltip label={t('recordings.shareStop', { name })}>
                  <button
                    type="button"
                    className="icon-action danger"
                    aria-label={t('recordings.shareStop', { name })}
                    disabled={isBusy}
                    onClick={() => {
                      setProblem(null)
                      setStopping(share)
                    }}
                  >
                    <TrashIcon size={16} />
                  </button>
                </Tooltip>
              </div>
            )
          })}
        </div>

        {choices.data && people.length === 0 && circles.length === 0 ? (
          <>
            <p className="muted">{t('recordings.shareNoPeople')}</p>
            <p>
              <Link className="button" to="/circles" onClick={onClose}>
                {t('circles.open')}
              </Link>
            </p>
          </>
        ) : (
          <>
            <label>
              {t('recordings.shareWith')}
              <select value={choice} onChange={(event) => setChoice(event.target.value)}>
                <option value="">{t('recordings.shareChoose')}</option>
                {people.length > 0 && (
                  <optgroup label={t('recordings.sharePeople')}>
                    {people.map((person) => (
                      <option key={person.userId} value={`user:${person.userId}`}>
                        {personLabel(person)}
                      </option>
                    ))}
                  </optgroup>
                )}
                {circles.length > 0 && (
                  <optgroup label={t('recordings.shareCircles')}>
                    {circles.map((circle) => (
                      <option key={circle.id} value={`circle:${circle.id}`}>
                        {circleOption(circle)}
                      </option>
                    ))}
                  </optgroup>
                )}
              </select>
            </label>
            <label>
              {t('recordings.shareLevelLabel')}
              <select
                value={recordingShareLevel}
                onChange={(event) => setRecordingShareLevel(event.target.value as RecordingShareLevel)}
              >
                {RECORDING_SHARE_LEVELS.map((level) => (
                  <option key={level} value={level}>
                    {t(`recordings.shareLevelName.${level}` as Parameters<typeof t>[0])}
                  </option>
                ))}
              </select>
              <span className="muted">
                {t(`recordings.shareLevelHint.${recordingShareLevel}` as Parameters<typeof t>[0])}
              </span>
            </label>
          </>
        )}
      </FormDialog>
      {stopping ? (
        <ConfirmDialog
          title={t('sharing.stopTitle', { name: shareName(stopping, everyoneIn) })}
          body={t('sharing.stopBody', { name: shareName(stopping, everyoneIn), what: t('recordings.shareStopWhat') })}
          confirmLabel={t('sharing.stop')}
          busy={isBusy}
          error={problem}
          onConfirm={() =>
            void run(
              () => graphql(RECORDING_SHARE_DOCUMENTS.UnshareRecording, { recordingShareId: stopping.id }),
              t('sharing.stopped', { name: shareName(stopping, everyoneIn) }),
            ).then((isDone) => isDone && setStopping(null))
          }
          onClose={() => setStopping(null)}
        />
      ) : null}
    </>
  )
}

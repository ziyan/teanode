import { useParams } from 'react-router-dom'

import { graphql } from '../api'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { ErrorMessage, Loading, Tag } from '../components/common'
import { Pager, usePageInAddress } from '../components/pager'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'
import { formatRecordingLength } from './library'
import { RecordedAtText } from './libraryList'
import { groupSharedPassages, RECORDING_SHARE_DOCUMENTS, type SharedRecording } from './sharing'

// A recording somebody shared with the person, as far as the level they gave
// goes: the title, the date, the summary and who spoke, and at the transcript
// level the words, a page at a time. There is no audio, no way to name a
// speaker and no playing from a passage: none of that is the reader's.
export function SharedRecordingPage() {
  const { recordingId = '' } = useParams()
  const { t, plural } = useTranslation()
  const { pageIndex, pageSize, setPageIndex, setPageSize } = usePageInAddress()
  const reading = useQuery(
    () =>
      graphql<{ ReadSharedRecording: SharedRecording }>(RECORDING_SHARE_DOCUMENTS.ReadSharedRecording, {
        recordingId,
        passageOffset: pageIndex * pageSize,
        pageSize,
      }),
    [recordingId, pageIndex, pageSize],
    { refresh: false },
  )
  const shared = reading.data?.ReadSharedRecording ?? null
  useBreadcrumbDetail(shared ? shared.recordingTitle || t('recordings.untitled') : null)
  const isGone = !shared && Boolean(reading.error) && /not found/i.test(String(reading.error))
  if (!shared) {
    return (
      <div className="recording-page">
        {reading.loading ? <Loading /> : isGone ? <p className="muted">{t('common.notFound')}</p> : null}
        {isGone ? null : <ErrorMessage error={reading.error} />}
      </div>
    )
  }
  const turns = groupSharedPassages(shared.passages)
  const isTranscript = shared.recordingShareLevel === 'transcript'
  return (
    <div className="recording-page">
      <div className="recording-detail">
        <div className="recording-header">
          <div className="recording-header-text">
            <h2>{shared.recordingTitle || t('recordings.untitled')}</h2>
            <p className="recording-meta">
              {t('recordings.sharedBy', { name: shared.ownerName || shared.ownerUsername })}
              {' · '}
              <RecordedAtText recordedAt={shared.recordedAt} />
              {shared.timelineDurationMS > 0 ? (
                <span> · {formatRecordingLength(shared.timelineDurationMS)}</span>
              ) : null}{' '}
              <Tag value={t(`recordings.shareLevelName.${shared.recordingShareLevel}` as Parameters<typeof t>[0])} />
            </p>
          </div>
        </div>
        {shared.recordingSummary ? (
          <section className="recording-summary">
            <h3>{t('recordings.summary')}</h3>
            <p>{shared.recordingSummary}</p>
          </section>
        ) : null}
        {shared.speakerNames.length > 0 ? (
          <p className="muted">
            {plural(
              shared.speakerNames.length,
              { one: 'recordings.sharedSpeakersOne', other: 'recordings.sharedSpeakers' },
              { count: shared.speakerNames.length, names: shared.speakerNames.join(', ') },
            )}
          </p>
        ) : null}
        {!isTranscript ? <p className="muted">{t('recordings.sharedSummaryOnly')}</p> : null}
        {isTranscript ? (
          <div className="recording-conversation">
            {turns.length === 0 ? <p className="muted">{t('recordings.noTranscript')}</p> : null}
            {turns.map((turn) => (
              <div key={turn.turnKey} className="recording-turn">
                <div className={turn.speakerName ? 'recording-turn-name' : 'recording-turn-name muted'}>
                  {turn.speakerName} <span className="muted">{formatRecordingLength(turn.startMS)}</span>
                </div>
                <div className="agent-line assistant">
                  {turn.passages.map((passage) => passage.transcriptText.trim()).join(' ')}
                </div>
              </div>
            ))}
            {(pageIndex > 0 || shared.hasMorePassages) && (
              <div className="list-foot">
                <Pager
                  pageIndex={pageIndex}
                  pageSize={pageSize}
                  shownCount={shared.passages.length}
                  totalCount={shared.passageCount}
                  isLoading={reading.loading}
                  onPageIndex={setPageIndex}
                  onPageSize={setPageSize}
                />
              </div>
            )}
          </div>
        ) : null}
      </div>
    </div>
  )
}

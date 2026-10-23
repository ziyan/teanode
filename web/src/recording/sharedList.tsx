import { Link, useSearchParams } from 'react-router-dom'

import { graphql } from '../api'
import { Tag } from '../components/common'
import { Pager } from '../components/pager'
import { SettingsRow, SettingsSection } from '../components/settingsList'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'
import { formatRecordingLength } from './library'
import { RecordedAtText } from './libraryList'
import { RECORDING_SHARE_DOCUMENTS, type SharedRecording } from './sharing'

const PAGE_SIZE = 25

// The recordings other people shared with the person, under their own
// library. It draws nothing while there are none: most people have none, and a
// panel saying so on every visit is room taken for nothing. The page is in the
// address as the library's is (a cursor of the last recording of each page
// before), under its own name so the two lists page apart.
export function SharedRecordingsSection({ userId }: { userId: string }) {
  const { t } = useTranslation()
  const [parameters, setParameters] = useSearchParams()
  const cursors = ['', ...(parameters.get('sharedBefore')?.split(',').filter(Boolean) ?? [])]
  const beforeRecordingId = cursors[cursors.length - 1]
  const setCursors = (next: string[]) => {
    const written = new URLSearchParams(parameters)
    if (next.length > 1) written.set('sharedBefore', next.slice(1).join(','))
    else written.delete('sharedBefore')
    setParameters(written)
  }
  const shared = useQuery(
    () =>
      graphql<{ ListSharedRecordings: SharedRecording[] }>(RECORDING_SHARE_DOCUMENTS.ListSharedRecordings, {
        beforeRecordingId: beforeRecordingId || null,
        pageSize: PAGE_SIZE + 1,
      }),
    [beforeRecordingId, userId],
  )
  const fetched = shared.data?.ListSharedRecordings ?? []
  const rows = fetched.slice(0, PAGE_SIZE)
  if (rows.length === 0 && cursors.length === 1) return null
  return (
    <SettingsSection card title={t('recordings.sharedWithYou')} description={t('recordings.sharedWithYouHint')}>
      {rows.map((recording) => (
        <SettingsRow
          key={recording.recordingId}
          title={
            <Link className="recording-title-link" to={`/recordings/shared/${recording.recordingId}`}>
              {recording.recordingTitle || <span className="muted">{t('recordings.untitled')}</span>}
            </Link>
          }
          subtitle={
            <>
              {t('recordings.sharedBy', { name: recording.ownerName || recording.ownerUsername })}
              {' · '}
              <RecordedAtText recordedAt={recording.recordedAt} />
              {recording.timelineDurationMS > 0 ? ` · ${formatRecordingLength(recording.timelineDurationMS)}` : ''}
            </>
          }
          badge={
            <Tag value={t(`recordings.shareLevelName.${recording.recordingShareLevel}` as Parameters<typeof t>[0])} />
          }
        />
      ))}
      {(cursors.length > 1 || fetched.length > PAGE_SIZE) && (
        <div className="list-foot">
          <Pager
            pageIndex={cursors.length - 1}
            pageSize={PAGE_SIZE}
            shownCount={rows.length}
            totalCount={(cursors.length - 1) * PAGE_SIZE + fetched.length}
            isTotalCountLowerBound={fetched.length > PAGE_SIZE}
            hasNextPage={fetched.length > PAGE_SIZE}
            isLoading={shared.loading}
            onPageIndex={(nextPageIndex) =>
              setCursors(
                nextPageIndex < cursors.length - 1
                  ? cursors.slice(0, -1)
                  : [...cursors, rows[PAGE_SIZE - 1].recordingId],
              )
            }
          />
        </div>
      )}
    </SettingsSection>
  )
}

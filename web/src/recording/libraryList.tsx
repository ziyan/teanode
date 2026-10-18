import { useMemo } from 'react'
import { Link } from 'react-router-dom'
import { DataTable, type Column } from '../components/dataTable'
import { Tag } from '../components/common'
import { useTranslation } from '../i18n/i18n'
import { type Recording } from './api'
import {
  firstSummaryLine,
  formatRecordedAt,
  formatRecordingLength,
  recordingSortTime,
  recordingStatus,
  sortRecordingsNewestFirst,
  type RecordingStatusEvidence,
} from './library'

// The date a recording happened, or a muted "Date unknown". Never the upload
// date: a person looking for last Tuesday's meeting must not be shown the day
// somebody finally imported it.
export function RecordedAtText({ recordedAt }: { recordedAt: string | null }) {
  const { t, language } = useTranslation()
  const formatted = formatRecordedAt(recordedAt, language)
  return formatted ? <span>{formatted}</span> : <span className="muted">{t('recordings.dateUnknown')}</span>
}

export function RecordingStatusTag({ status }: { status: ReturnType<typeof recordingStatus> }) {
  const { t } = useTranslation()
  const tone =
    status === 'failed'
      ? 'bad'
      : status === 'ready'
        ? 'good'
        : status === 'notTranscribed' || status === 'noSpeech'
          ? undefined
          : 'warn'
  return <Tag value={t(`recordings.status.${status}` as Parameters<typeof t>[0])} tone={tone} />
}

// The library is a table like the dashboard's others: newest first by default,
// sortable and filterable, and still a table on a phone, where it scrolls
// sideways inside its surface and drops the summary column.
export function RecordingLibraryTable({
  recordings,
  evidenceByRecordingId,
  loading,
  search,
}: {
  recordings: Recording[]
  evidenceByRecordingId: Record<string, RecordingStatusEvidence>
  loading?: boolean
  // search is the page's box that searches the server, in the table's toolbar.
  search?: React.ReactNode
}) {
  const { t, plural } = useTranslation()
  const rows = useMemo(() => sortRecordingsNewestFirst(recordings), [recordings])
  const statusLabel = (recording: Recording) =>
    t(`recordings.status.${recordingStatus(recording, evidenceByRecordingId[recording.id])}` as Parameters<typeof t>[0])
  const columns: Column<Recording>[] = [
    {
      key: 'title',
      header: t('recordings.column.title'),
      truncate: false,
      filter: 'text',
      value: (recording) => recording.recordingTitle || t('recordings.untitled'),
      sort: (first, second) => first.recordingTitle.localeCompare(second.recordingTitle),
      render: (recording) => {
        const status = recordingStatus(recording, evidenceByRecordingId[recording.id])
        return (
          <>
            <Link className="recording-title-link" to={`/recordings/${recording.id}`}>
              {recording.recordingTitle ? (
                recording.recordingTitle
              ) : (
                <span className="muted">{t('recordings.untitled')}</span>
              )}
            </Link>
            {/* The Recorded, Length and Status columns are dropped on a
                phone, so the same three things go under the title there. */}
            <span className="recording-meta-line muted">
              <RecordedAtText recordedAt={recording.recordedAt} />
              {recording.timelineDurationMS > 0 && <span>{formatRecordingLength(recording.timelineDurationMS)}</span>}
              {status !== 'ready' && <RecordingStatusTag status={status} />}
            </span>
          </>
        )
      },
    },
    {
      key: 'recordedAt',
      header: t('recordings.column.recordedAt'),
      width: '190px',
      optional: true,
      sort: (first, second) => recordingSortTime(first) - recordingSortTime(second),
      render: (recording) => <RecordedAtText recordedAt={recording.recordedAt} />,
    },
    {
      key: 'length',
      header: t('recordings.column.length'),
      width: '90px',
      optional: true,
      numeric: true,
      sort: (first, second) => first.timelineDurationMS - second.timelineDurationMS,
      render: (recording) =>
        recording.timelineDurationMS > 0 ? formatRecordingLength(recording.timelineDurationMS) : null,
    },
    {
      key: 'summary',
      header: t('recordings.column.summary'),
      optional: true,
      truncate: true,
      value: (recording) => firstSummaryLine(recording.recordingSummary),
    },
    {
      key: 'status',
      header: t('recordings.column.status'),
      width: '140px',
      optional: true,
      filter: 'select',
      value: statusLabel,
      sort: (first, second) => statusLabel(first).localeCompare(statusLabel(second)),
      // Ready is what nearly every recording is, and a pill on every row
      // says nothing; the ones that are not ready are the ones to notice.
      render: (recording) => {
        const status = recordingStatus(recording, evidenceByRecordingId[recording.id])
        return status === 'ready' ? null : <RecordingStatusTag status={status} />
      },
    },
  ]
  return (
    <DataTable
      search={search}
      columns={columns}
      rows={rows}
      rowKey={(recording) => recording.id}
      rowLink={(recording) => `/recordings/${recording.id}`}
      loading={loading}
      emptyMessage={t('recordings.empty')}
      countLabel={(count) => plural(count, { one: 'recordings.countOne', other: 'recordings.count' }, { count })}
    />
  )
}

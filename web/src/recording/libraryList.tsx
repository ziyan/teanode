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
    status === 'failed' ? 'bad' : status === 'ready' ? 'good' : status === 'notTranscribed' ? undefined : 'warn'
  return <Tag value={t(`recordings.status.${status}` as Parameters<typeof t>[0])} tone={tone} />
}

// The library is a table like the dashboard's others: newest first by default,
// sortable and filterable, and still a table on a phone, where it scrolls
// sideways inside its surface and drops the summary column.
export function RecordingLibraryTable({
  recordings,
  evidenceByRecordingId,
  loading,
}: {
  recordings: Recording[]
  evidenceByRecordingId: Record<string, RecordingStatusEvidence>
  loading?: boolean
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
      render: (recording) => (
        <Link className="recording-title-link" to={`/recordings/${recording.id}`}>
          {recording.recordingTitle ? (
            recording.recordingTitle
          ) : (
            <span className="muted">{t('recordings.untitled')}</span>
          )}
        </Link>
      ),
    },
    {
      key: 'recordedAt',
      header: t('recordings.column.recordedAt'),
      width: '190px',
      sort: (first, second) => recordingSortTime(first) - recordingSortTime(second),
      render: (recording) => <RecordedAtText recordedAt={recording.recordedAt} />,
    },
    {
      key: 'length',
      header: t('recordings.column.length'),
      width: '90px',
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
      filter: 'select',
      value: statusLabel,
      sort: (first, second) => statusLabel(first).localeCompare(statusLabel(second)),
      render: (recording) => (
        <RecordingStatusTag status={recordingStatus(recording, evidenceByRecordingId[recording.id])} />
      ),
    },
  ]
  return (
    <DataTable
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

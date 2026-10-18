import { cleanup, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, expect, it } from 'vitest'
import { TranslationProvider } from '../i18n/i18n'
import { type Recording } from './api'
import { RecordingLibraryTable } from './libraryList'

afterEach(cleanup)
const recordingFixture = (overrides: Partial<Recording>): Recording =>
  ({
    id: 'fixture',
    createdAt: '2026-09-01T00:00:00Z',
    recordedAt: null,
    recordingTitle: '',
    recordingCaptureState: 'stopped',
    timelineDurationMS: 65_000,
    ...overrides,
  }) as Recording

const show = (recordings: Recording[], evidence = {}) =>
  render(
    <TranslationProvider>
      <MemoryRouter>
        <RecordingLibraryTable recordings={recordings} evidenceByRecordingId={evidence} />
      </MemoryRouter>
    </TranslationProvider>,
  )

it('is a table with the recorded date, Date unknown, length, summary line and one status', () => {
  show(
    [
      recordingFixture({
        id: 'dated',
        recordingTitle: 'Planning',
        recordedAt: '2026-10-13T09:41:00Z',
        recordingSummary: 'Agreed the plan.\nMore.',
      }),
      recordingFixture({ id: 'undated', createdAt: '2026-10-20T00:00:00Z' }),
    ],
    {
      dated: {
        mediaFiles: [{ recordingUploadState: 'complete' }],
        transcriptRuns: [{ versionNumber: 1, recordingProcessingState: 'complete' }],
      },
      undated: { mediaFiles: [{ recordingUploadState: 'pending' }], transcriptRuns: [] },
    },
  )
  expect(screen.getAllByRole('columnheader').map((header) => header.textContent)).toEqual([
    'Title',
    'Recorded',
    'Length',
    'Summary',
    'Status',
  ])
  const rows = screen.getAllByRole('row').slice(1)
  expect(rows).toHaveLength(2)
  // The undated recording was created later, so it sorts first.
  expect(within(rows[0]).getByText('Untitled recording').className).toContain('muted')
  // The date and the status are in the Recorded and Status columns, and again
  // under the title, which is where a phone shows them.
  expect(within(rows[0]).getAllByText('Date unknown')[0].className).toContain('muted')
  expect(within(rows[0]).getAllByText('Uploading')).toHaveLength(2)
  expect(within(rows[1]).getByText('Planning')).toBeTruthy()
  // The date is in the Recorded column and again in the meta line under the
  // title, which the stylesheet shows only on a phone (jsdom has no media
  // queries). Exactly one of the two is the meta line, and the other is the
  // column's cell, so a third copy that showed on a desktop would fail here.
  const dates = within(rows[1]).getAllByText(/2026/)
  expect(dates).toHaveLength(2)
  expect(dates.filter((date) => date.closest('.recording-meta-line'))).toHaveLength(1)
  expect(dates.filter((date) => date.closest('td.optional'))).toHaveLength(1)
  expect(within(rows[1]).queryByText('Date unknown')).toBeNull()
  expect(within(rows[1]).getByText('Agreed the plan.')).toBeTruthy()
  // Ready is what nearly every recording is, so it gets no pill.
  expect(within(rows[1]).queryByText('Ready')).toBeNull()
  expect(within(rows[1]).getAllByText('1:05')).toHaveLength(2)
})

it('orders by when it was recorded, not uploaded, and falls back to creation for undated ones', () => {
  show([
    recordingFixture({
      id: 'a',
      recordingTitle: 'Early',
      recordedAt: '2026-02-01T00:00:00Z',
      createdAt: '2026-09-01T00:00:00Z',
    }),
    recordingFixture({ id: 'b', recordingTitle: 'Middle', createdAt: '2026-04-01T00:00:00Z' }),
    recordingFixture({ id: 'c', recordingTitle: 'Late', recordedAt: '2026-06-01T00:00:00Z' }),
  ])
  const titles = screen.getAllByRole('link').map((link) => link.textContent)
  expect(titles).toEqual(['Late', 'Middle', 'Early'])
})

it('links an untitled recording to it, and the row opens it too', () => {
  show([recordingFixture({ id: 'no-title-fixture' })])
  const link = screen.getByRole('link', { name: 'Untitled recording' })
  expect(link.getAttribute('href')).toBe('/recordings/no-title-fixture')
  expect(screen.getAllByRole('row')[1].className).toContain('linked')
})

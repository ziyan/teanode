import { useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage } from '../../components/common'
import { ConfirmDialog } from '../../components/dialog'
import { SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { useTranslation } from '../../i18n/i18n'
import { ERASE_JOURNAL, JournalSettings, UPDATE_JOURNAL_SETTINGS } from './journalApi'

// The Settings tab of the Journal: whether each day gets its weather, whether
// TeaNode asks how the day was each evening and when, what the model sees,
// and erasing the whole journal.

export function JournalSettingsTab({
  settings,
  error,
  onChanged,
}: {
  settings: JournalSettings | null
  error: unknown
  onChanged: () => Promise<void>
}) {
  return (
    <>
      <ErrorMessage error={error} />
      <WhatItDoes settings={settings} onChanged={onChanged} />
      <EraseEverything />
    </>
  )
}

function WhatItDoes({ settings, onChanged }: { settings: JournalSettings | null; onChanged: () => Promise<void> }) {
  const { t } = useTranslation()
  const toast = useToast()
  const [isBusy, setIsBusy] = useState(false)
  // The time being typed, until it is saved.
  const [typedTime, setTypedTime] = useState<string | null>(null)
  const time = typedTime ?? settings?.eveningQuestionTime ?? ''

  async function change(changes: {
    isWeatherOn?: boolean
    isEveningQuestionOn?: boolean
    eveningQuestionTime?: string
  }) {
    // The box stays enabled while this runs, so keyboard focus is not lost
    // from it; a second change waits for the first.
    if (isBusy) return
    setIsBusy(true)
    try {
      await graphql(UPDATE_JOURNAL_SETTINGS, changes)
      toast.done(t('journal.settingsSaved'))
      setTypedTime(null)
      await onChanged()
    } catch (caught) {
      toast.failure(caught, t('journal.settingsFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <SettingsSection card title={t('journal.settingsTitle')} description={t('journal.settingsIntro')}>
      <label className="checkbox is-plain">
        <input
          type="checkbox"
          checked={settings?.isWeatherOn ?? false}
          disabled={!settings}
          aria-busy={isBusy}
          onChange={(event) => void change({ isWeatherOn: event.target.checked })}
        />
        <span>{t('journal.settingsWeather')}</span>
      </label>
      <label className="checkbox is-plain">
        <input
          type="checkbox"
          checked={settings?.isEveningQuestionOn ?? false}
          disabled={!settings}
          aria-busy={isBusy}
          onChange={(event) => void change({ isEveningQuestionOn: event.target.checked })}
        />
        <span>{t('journal.settingsEvening')}</span>
      </label>
      {settings?.isEveningQuestionOn && (
        <form
          className="form-narrow journal-evening-time"
          onSubmit={(event) => {
            event.preventDefault()
            void change({ eveningQuestionTime: time })
          }}
        >
          <label>
            {t('journal.settingsEveningTime')}
            <input type="time" value={time} onChange={(event) => setTypedTime(event.target.value)} />
          </label>
          <button type="submit" disabled={isBusy || typedTime === null || typedTime === settings.eveningQuestionTime}>
            {t('common.save')}
          </button>
        </form>
      )}
      <p className="muted">
        {settings?.modelProviderName
          ? t('journal.settingsProvider', { provider: settings.modelProviderName })
          : t('journal.settingsProviderUnknown')}
      </p>
    </SettingsSection>
  )
}

function EraseEverything() {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const [isConfirming, setIsConfirming] = useState(false)
  const [isBusy, setIsBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function erase() {
    setIsBusy(true)
    setError(null)
    try {
      const answer = await graphql<{ EraseJournal: { dayCount: number; periodCount: number; linkCount: number } }>(
        ERASE_JOURNAL,
      )
      setIsConfirming(false)
      toast.done(plural(answer.EraseJournal.dayCount, { one: 'journal.eraseDoneOne', other: 'journal.eraseDoneOther' }))
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : t('journal.eraseFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <SettingsSection card title={t('journal.eraseTitle')} description={t('journal.eraseBody')}>
      <div className="page-actions page-actions-end">
        <button type="button" className="danger" onClick={() => setIsConfirming(true)}>
          {t('journal.eraseButton')}
        </button>
      </div>
      {isConfirming && (
        <ConfirmDialog
          title={t('journal.eraseTitle')}
          body={t('journal.eraseConfirm')}
          confirmLabel={t('journal.eraseButton')}
          busy={isBusy}
          error={error}
          onConfirm={() => void erase()}
          onClose={() => setIsConfirming(false)}
        />
      )}
    </SettingsSection>
  )
}

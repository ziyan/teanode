import { useState } from 'react'

import { graphql } from '../api'
import { ErrorMessage, Loading, Tag, formatTime } from './common'
import { FormDialog } from './dialog'
import { Select } from './select'
import { SettingsEmpty, SettingsRow, SettingsSection } from './settingsList'
import { useToast } from './toast'
import { useQuery } from './useQuery'
import { Key, useTranslation } from '../i18n/i18n'
import { useSpendingCategoryDisplayName } from '../pages/finance/spendingCategoryName'

// The Alerts card on the agent's Overview: what the agent told the person
// unasked, with a Mute for each, and below them what they muted, with an
// Unmute for each. The same operations as the command line's agent alert
// and the agent's agent_profile tool; the switch and the night are in the
// agent's settings, beside speaking first.

export type AlertCovered = {
  mailId: string
  subject: string
  fromAddress: string
  candidateKind: string
  mailCategory: string
}

export type AgentAlert = {
  id: string
  alertText: string
  sentAt: string
  subjectKey: string
  isUrgent: boolean
  covered: AlertCovered[]
  muteChoices: MuteChoice[]
}

export type MuteChoice = { muteScope: MuteScope; muteTarget: string }

export type AlertMute = { id: string; muteScope: MuteScope; muteTarget: string; createdAt: string }

export type MuteScope = 'subjectKey' | 'sender' | 'domain' | 'kind' | 'spendingCategory'

const ALERTS = `query {
  ListAgentAlerts(first: 20) { id alertText sentAt subjectKey isUrgent
    covered { mailId subject fromAddress candidateKind mailCategory }
    muteChoices { muteScope muteTarget } }
  ListAgentAlertMutes { id muteScope muteTarget createdAt }
}`

const MUTE_ALERT = `mutation ($alertId: String, $muteScope: String) {
  MuteAgentAlert(alertId: $alertId, muteScope: $muteScope) { id muteScope muteTarget }
}`

const UNMUTE_ALERT = `mutation ($muteId: String!) { UnmuteAgentAlert(muteId: $muteId) }`

// The names of the person's spending categories, for a mute of one
// spending category's budget alerts: the mute holds its id, and an id is
// not something anybody recognizes.
const SPENDING_CATEGORY_NAMES = `query { SpendingCategories { id spendingCategoryName isTransfer } }`

// muteTargets is what each scope would mute for an alert, as the server
// works it out from what the alert covered, the default first: the burst
// for an alert about one, the sender for an alert about a message.
export function muteTargets(alert: AgentAlert): MuteChoice[] {
  return alert.muteChoices ?? []
}

export function AlertsCard() {
  const { t } = useTranslation()
  const categoryName = useSpendingCategoryDisplayName()
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const [muting, setMuting] = useState<AgentAlert | null>(null)
  const [muteScope, setMuteScope] = useState<MuteScope>('subjectKey')
  const { data, error, loading, reload } = useQuery(
    () => graphql<{ ListAgentAlerts: AgentAlert[]; ListAgentAlertMutes: AlertMute[] }>(ALERTS),
    [],
  )
  const isSpendingCategoryNamed = [
    ...(data?.ListAgentAlertMutes ?? []),
    ...(data?.ListAgentAlerts ?? []).flatMap((alert) => alert.muteChoices ?? []),
  ].some((mute) => mute.muteScope === 'spendingCategory')
  const spendingCategories = useQuery(
    () =>
      isSpendingCategoryNamed
        ? graphql<{ SpendingCategories: { id: string; spendingCategoryName: string; isTransfer: boolean }[] }>(SPENDING_CATEGORY_NAMES)
        : Promise.resolve({ SpendingCategories: [] }),
    [isSpendingCategoryNamed],
    { refresh: false },
  )

  if (loading && !data) return <Loading />
  const alerts = data?.ListAgentAlerts ?? []
  const mutes = data?.ListAgentAlertMutes ?? []

  async function act(work: () => Promise<unknown>, done: string) {
    setBusy(true)
    try {
      await work()
      toast.done(done)
      await reload()
      return true
    } catch (caught) {
      toast.failure(caught, t('alerts.failed'))
      return false
    } finally {
      setBusy(false)
    }
  }

  // targetLabel is what a mute names, as a person reads it: a spending
  // category by its name rather than its id, and the kind of alert a budget
  // crossing is by what it is called.
  const targetLabel = (scope: MuteScope, target: string): string => {
    if (scope === 'spendingCategory') {
      const found = spendingCategories.data?.SpendingCategories.find((category) => category.id === target)
      return found ? categoryName(found.spendingCategoryName, found.isTransfer) : t('alerts.deletedSpendingCategory')
    }
    if (scope === 'kind' && target === 'budget') return t('alerts.kindBudget')
    return target
  }
  const scopeLabel = (scope: MuteScope, target: string) =>
    t(`alerts.scope.${scope}` as Key, { target: targetLabel(scope, target) })

  return (
    <SettingsSection card title={t('alerts.title')} description={t('alerts.hint')}>
      <ErrorMessage error={error} />
      {alerts.length === 0 ? (
        <SettingsEmpty>{t('alerts.none')}</SettingsEmpty>
      ) : (
        alerts.map((alert) => (
          <SettingsRow
            key={alert.id}
            title={<span className="alert-text">{alert.alertText}</span>}
            badge={alert.isUrgent ? <Tag value={t('alerts.urgent')} tone="warn" /> : undefined}
            subtitle={
              <>
                {formatTime(alert.sentAt)}
                {alert.covered.map((covered) => (
                  <span key={covered.mailId} className="alert-covered">
                    {covered.subject || t('alerts.noSubject')} · {covered.fromAddress}
                  </span>
                ))}
              </>
            }
            actions={
              <button
                type="button"
                disabled={busy}
                aria-label={`${alert.subjectKey}: ${t('alerts.mute')}`}
                onClick={() => {
                  setMuteScope(muteTargets(alert)[0]?.muteScope ?? 'subjectKey')
                  setMuting(alert)
                }}
              >
                {t('alerts.mute')}
              </button>
            }
          />
        ))
      )}
      <div className="settings-subform">
        <h4>{t('alerts.muted')}</h4>
        <p className="muted">{t('alerts.mutedHint')}</p>
        {mutes.length === 0 ? (
          <SettingsEmpty>{t('alerts.noneMuted')}</SettingsEmpty>
        ) : (
          mutes.map((mute) => (
            <SettingsRow
              key={mute.id}
              title={scopeLabel(mute.muteScope, mute.muteTarget)}
              subtitle={t('alerts.mutedSince', { time: formatTime(mute.createdAt) })}
              actions={
                <button
                  type="button"
                  disabled={busy}
                  aria-label={`${mute.muteTarget}: ${t('alerts.unmute')}`}
                  onClick={() =>
                    void act(
                      () => graphql(UNMUTE_ALERT, { muteId: mute.id }),
                      t('alerts.unmuted', { target: targetLabel(mute.muteScope, mute.muteTarget) }),
                    )
                  }
                >
                  {t('alerts.unmute')}
                </button>
              }
            />
          ))
        )}
      </div>
      {muting ? (
        <FormDialog
          title={t('alerts.muteTitle')}
          submitLabel={t('alerts.mute')}
          busy={busy}
          onClose={() => setMuting(null)}
          onSubmit={() => {
            const target = muteTargets(muting).find((candidate) => candidate.muteScope === muteScope)
            void act(
              () => graphql(MUTE_ALERT, { alertId: muting.id, muteScope }),
              t('alerts.mutedDone', { target: target ? targetLabel(target.muteScope, target.muteTarget) : '' }),
            ).then((isDone) => {
              if (isDone) setMuting(null)
            })
          }}
        >
          <p className="muted">{muting.alertText}</p>
          <label>
            <span>{t('alerts.muteWhat')}</span>
            <Select
              block
              value={muteScope}
              label={t('alerts.muteWhat')}
              options={muteTargets(muting).map((target) => ({
                value: target.muteScope,
                label: scopeLabel(target.muteScope, target.muteTarget),
              }))}
              onChange={(value) => setMuteScope(value as MuteScope)}
            />
          </label>
        </FormDialog>
      ) : null}
    </SettingsSection>
  )
}

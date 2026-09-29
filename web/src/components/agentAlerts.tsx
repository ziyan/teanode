import { useState } from 'react'

import { graphql } from '../api'
import { ErrorMessage, Loading, Tag, formatTime } from './common'
import { FormDialog } from './dialog'
import { Select } from './select'
import { SettingsEmpty, SettingsRow, SettingsSection } from './settingsList'
import { useToast } from './toast'
import { useQuery } from './useQuery'
import { Key, useTranslation } from '../i18n/i18n'

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
}

export type AlertMute = { id: string; muteScope: MuteScope; muteTarget: string; createdAt: string }

export type MuteScope = 'subjectKey' | 'sender' | 'domain' | 'kind'

const ALERTS = `query {
  ListAgentAlerts(first: 20) { id alertText sentAt subjectKey isUrgent
    covered { mailId subject fromAddress candidateKind mailCategory } }
  ListAgentAlertMutes { id muteScope muteTarget createdAt }
}`

const MUTE_ALERT = `mutation ($alertId: String, $muteScope: String) {
  MuteAgentAlert(alertId: $alertId, muteScope: $muteScope) { id muteScope muteTarget }
}`

const UNMUTE_ALERT = `mutation ($muteId: String!) { UnmuteAgentAlert(muteId: $muteId) }`

// muteTargets is what each scope would mute for an alert, taken the way
// the server takes it: the subject key, and the sender, its domain and
// its kind from the first message the alert was about.
export function muteTargets(alert: AgentAlert): { muteScope: MuteScope; muteTarget: string }[] {
  const targets: { muteScope: MuteScope; muteTarget: string }[] = [
    { muteScope: 'subjectKey', muteTarget: alert.subjectKey },
  ]
  const first = alert.covered[0]
  if (!first) return targets
  if (first.fromAddress) {
    targets.push({ muteScope: 'sender', muteTarget: first.fromAddress })
    const domain = first.fromAddress.split('@')[1]
    if (domain) targets.push({ muteScope: 'domain', muteTarget: domain })
  }
  const kind = first.candidateKind === 'burst' ? 'burst' : first.mailCategory
  if (kind) targets.push({ muteScope: 'kind', muteTarget: kind })
  return targets
}

export function AlertsCard() {
  const { t } = useTranslation()
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const [muting, setMuting] = useState<AgentAlert | null>(null)
  const [muteScope, setMuteScope] = useState<MuteScope>('subjectKey')
  const { data, error, loading, reload } = useQuery(
    () => graphql<{ ListAgentAlerts: AgentAlert[]; ListAgentAlertMutes: AlertMute[] }>(ALERTS),
    [],
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

  const scopeLabel = (scope: MuteScope, target: string) => t(`alerts.scope.${scope}` as Key, { target })

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
                  setMuteScope('subjectKey')
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
                      t('alerts.unmuted', { target: mute.muteTarget }),
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
              t('alerts.mutedDone', { target: target?.muteTarget ?? '' }),
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

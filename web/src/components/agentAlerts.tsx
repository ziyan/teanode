import { useState } from 'react'

import { graphql } from '../api'
import { ErrorMessage, Loading, Tag, formatMoney, formatTime } from './common'
import { FormDialog } from './dialog'
import { Select } from './select'
import { SettingsEmpty, SettingsRow, SettingsSection } from './settingsList'
import { useToast } from './toast'
import { useQuery } from './useQuery'
import { Key, useTranslation } from '../i18n/i18n'
import { useSpendingCategoryDisplayName } from '../pages/finance/spendingCategoryName'

// The Alerts card on the agent's Overview: what the agent told the person
// unasked, with a Mute for each; below them what the agent is following (the
// bills, parcels, deadlines and events it keeps from mail), with a Stop for
// each; and last what they muted, with an Unmute for each. The same
// operations as the command line's agent alert and agent follow and the
// agent's agent_profile tool; the switch and the night are in the agent's
// settings, beside speaking first.

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

export type FollowUpKind = 'bill' | 'parcel' | 'deadline' | 'event'

// A follow-up as the server sends it: what a kind does not use is null. Only
// open ones are listed here, so followUpState is open, overdue or stalled.
export type FollowUp = {
  id: string
  followUpKind: FollowUpKind
  followUpTitle: string
  payee: string | null
  carrier: string | null
  amount: string | null
  currencyCode: string | null
  trackingNumber: string | null
  dueOn: string | null
  place: string | null
  followUpState: 'open' | 'overdue' | 'stalled'
}

export type MuteScope = 'subjectKey' | 'sender' | 'domain' | 'kind' | 'spendingCategory'

const ALERTS = `query {
  ListAgentAlerts(first: 20) { id alertText sentAt subjectKey isUrgent
    covered { mailId subject fromAddress candidateKind mailCategory }
    muteChoices { muteScope muteTarget } }
  ListAgentAlertMutes { id muteScope muteTarget createdAt }
  ListAgentFollowUps(isOpenOnly: true) { id followUpKind followUpTitle payee carrier amount currencyCode trackingNumber dueOn place followUpState }
}`

const MUTE_ALERT = `mutation ($alertId: String, $muteScope: String) {
  MuteAgentAlert(alertId: $alertId, muteScope: $muteScope) { id muteScope muteTarget }
}`

const MUTE_ALERT_KIND = `mutation ($muteTarget: String) {
  MuteAgentAlert(muteScope: "kind", muteTarget: $muteTarget) { id muteScope muteTarget }
}`

const STOP_FOLLOW_UP = `mutation ($followUpId: String!) { StopAgentFollowUp(followUpId: $followUpId) { id followUpState } }`

const UNMUTE_ALERT = `mutation ($muteId: String!) { UnmuteAgentAlert(muteId: $muteId) }`

// The names of the person's spending categories, for a mute of one
// spending category's budget alerts: the mute holds its id, and an id is
// not something anybody recognizes.
const SPENDING_CATEGORY_NAMES = `query { SpendingCategories { id spendingCategoryName isTransfer isOther } }`

// muteTargets is what each scope would mute for an alert, as the server
// works it out from what the alert covered, the default first: the burst
// for an alert about one, the sender for an alert about a message.
export function muteTargets(alert: AgentAlert): MuteChoice[] {
  return alert.muteChoices ?? []
}

// The kinds of alert about what the agent follows that a person can mute,
// all of them first.
export const FOLLOW_UP_MUTE_KINDS = ['follow_up', 'bill', 'parcel', 'deadline', 'event'] as const

// orderedFollowUps is the nearest day first, with the ones that have no day
// last, and the server's order within a day.
export function orderedFollowUps(followUps: FollowUp[]): FollowUp[] {
  return followUps
    .map((followUp, index) => ({ followUp, index }))
    .sort((left, right) => {
      const leftDay = left.followUp.dueOn ?? '9999-12-31'
      const rightDay = right.followUp.dueOn ?? '9999-12-31'
      return leftDay === rightDay ? left.index - right.index : leftDay < rightDay ? -1 : 1
    })
    .map((entry) => entry.followUp)
}

// followUpDay is a day written YYYY-MM-DD as the reader's calendar writes
// it. It is built from its parts rather than parsed, since a bare date read
// as UTC midnight is the evening before anywhere west of Greenwich.
export function followUpDay(dueOn: string): string {
  const [year, month, day] = dueOn.split('-').map(Number)
  const parsed = new Date(year, month - 1, day)
  if (!year || !month || !day || Number.isNaN(parsed.getTime())) return dueOn
  return parsed.toLocaleDateString(undefined, { weekday: 'short', year: 'numeric', month: 'short', day: 'numeric' })
}

// followUpDetails is what a kind has beyond its title, in the order a
// reader looks: who is to be paid and how much, who carries a parcel and
// its number, where an event is.
export function followUpDetails(followUp: FollowUp): string[] {
  const details: string[] = []
  if (followUp.payee) details.push(followUp.payee)
  if (followUp.amount) {
    const amount = Number(followUp.amount)
    details.push(
      Number.isFinite(amount) && followUp.currencyCode ? formatMoney(amount, followUp.currencyCode) : followUp.amount,
    )
  }
  const carrier = [followUp.carrier, followUp.trackingNumber].filter(Boolean).join(' ')
  if (carrier) details.push(carrier)
  if (followUp.place) details.push(followUp.place)
  return details
}

export function AlertsCard() {
  const { t } = useTranslation()
  const categoryName = useSpendingCategoryDisplayName()
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const [muting, setMuting] = useState<AgentAlert | null>(null)
  const [muteScope, setMuteScope] = useState<MuteScope>('subjectKey')
  const [isMutingFollowUps, setIsMutingFollowUps] = useState(false)
  const [followUpMuteKind, setFollowUpMuteKind] = useState<(typeof FOLLOW_UP_MUTE_KINDS)[number]>('follow_up')
  const { data, error, loading, reload } = useQuery(
    () =>
      graphql<{ ListAgentAlerts: AgentAlert[]; ListAgentAlertMutes: AlertMute[]; ListAgentFollowUps: FollowUp[] }>(
        ALERTS,
      ),
    [],
  )
  const isSpendingCategoryNamed = [
    ...(data?.ListAgentAlertMutes ?? []),
    ...(data?.ListAgentAlerts ?? []).flatMap((alert) => alert.muteChoices ?? []),
  ].some((mute) => mute.muteScope === 'spendingCategory')
  const spendingCategories = useQuery(
    () =>
      isSpendingCategoryNamed
        ? graphql<{
            SpendingCategories: { id: string; spendingCategoryName: string; isTransfer: boolean; isOther: boolean }[]
          }>(SPENDING_CATEGORY_NAMES)
        : Promise.resolve({ SpendingCategories: [] }),
    [isSpendingCategoryNamed],
    { refresh: false },
  )

  if (loading && !data) return <Loading />
  const alerts = data?.ListAgentAlerts ?? []
  const mutes = data?.ListAgentAlertMutes ?? []
  const followUps = orderedFollowUps(data?.ListAgentFollowUps ?? [])

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
      return found ? categoryName(found.spendingCategoryName, found) : t('alerts.deletedSpendingCategory')
    }
    if (scope === 'kind' && target === 'budget') return t('alerts.kindBudget')
    if (scope === 'kind' && (FOLLOW_UP_MUTE_KINDS as readonly string[]).includes(target)) {
      return t(`alerts.kind.${target}` as Key)
    }
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
                {alert.covered.map((covered, index) => (
                  // A message read from a mailbox this server does not
                  // host has no mail id here.
                  <span key={covered.mailId || `watched-${index}`} className="alert-covered">
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
        <h4>{t('alerts.following')}</h4>
        <p className="muted">{t('alerts.followingHint')}</p>
        {followUps.length === 0 ? (
          <SettingsEmpty>{t('alerts.followingNone')}</SettingsEmpty>
        ) : (
          followUps.map((followUp) => (
            <SettingsRow
              key={followUp.id}
              title={<span className="alert-text">{followUp.followUpTitle}</span>}
              badge={
                followUp.followUpState === 'overdue' || followUp.followUpState === 'stalled' ? (
                  <Tag
                    value={t(`alerts.followUpState.${followUp.followUpState}` as Key)}
                    tone={followUp.followUpState === 'overdue' ? 'bad' : 'warn'}
                  />
                ) : undefined
              }
              subtitle={[
                t(`alerts.followUpKind.${followUp.followUpKind}` as Key),
                followUp.dueOn ? followUpDay(followUp.dueOn) : t('alerts.followUpNoDay'),
                ...followUpDetails(followUp),
              ].join(' · ')}
              actions={
                <button
                  type="button"
                  disabled={busy}
                  aria-label={t('alerts.followUpStopLabel', { title: followUp.followUpTitle })}
                  onClick={() =>
                    void act(
                      () => graphql(STOP_FOLLOW_UP, { followUpId: followUp.id }),
                      t('alerts.followUpStopped', { title: followUp.followUpTitle }),
                    )
                  }
                >
                  {t('alerts.followUpStop')}
                </button>
              }
            />
          ))
        )}
        <div className="page-actions-end">
          <button type="button" disabled={busy} onClick={() => setIsMutingFollowUps(true)}>
            {t('alerts.followUpMute')}
          </button>
        </div>
      </div>
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
      {isMutingFollowUps ? (
        <FormDialog
          title={t('alerts.followUpMuteTitle')}
          submitLabel={t('alerts.mute')}
          busy={busy}
          onClose={() => setIsMutingFollowUps(false)}
          onSubmit={() =>
            void act(
              () => graphql(MUTE_ALERT_KIND, { muteTarget: followUpMuteKind }),
              t('alerts.mutedDone', { target: targetLabel('kind', followUpMuteKind) }),
            ).then((isDone) => {
              if (isDone) setIsMutingFollowUps(false)
            })
          }
        >
          <label>
            <span>{t('alerts.muteWhat')}</span>
            <Select
              block
              value={followUpMuteKind}
              label={t('alerts.muteWhat')}
              options={FOLLOW_UP_MUTE_KINDS.map((kind) => ({ value: kind, label: t(`alerts.kind.${kind}` as Key) }))}
              onChange={(value) => setFollowUpMuteKind(value as (typeof FOLLOW_UP_MUTE_KINDS)[number])}
            />
          </label>
        </FormDialog>
      ) : null}
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

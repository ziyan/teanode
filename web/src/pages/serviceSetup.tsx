import { useEffect, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { graphql, type ServiceCapabilities } from '../api'
import { ErrorMessage, Loading, Tag } from '../components/common'
import { useToast } from '../components/toast'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'
import type { HostedServiceKey } from '../services'
import { MailSetupPage } from './setup'

const CAPABILITIES = `isMailRoutingEnabled isMailboxEnabled isCalendarEnabled isRemindersEnabled isContactsEnabled isAssistantEnabled hasPendingServiceRestart hasCompletedSetup`
// Beside the services: what waits for a restart, and whether the person
// setting up has turned on an agent of their own, which is the last switch
// between an enabled assistant and a conversation.
const SETUP = `{
  GetServiceSetup { selectedServices { ${CAPABILITIES} } runningServices { ${CAPABILITIES} } hasModelProvider hasMailDomain mailServerName }
  GetServerStatus { pendingRestart }
}`
// Asked apart from the rest, and allowed to fail on its own: somebody who
// manages the server need not be somebody who may use an agent.
const READ_AGENT = `{ ReadAgent { agent { id enabled } allowed { enabled } } }`
const TURN_ON_AGENT = `mutation { UpdateAgent(enabled: true) { agent { id enabled } } }`
const UPDATE = `mutation ($isMailRoutingEnabled: Boolean, $isMailboxEnabled: Boolean, $isCalendarEnabled: Boolean, $isRemindersEnabled: Boolean, $isContactsEnabled: Boolean, $hasCompletedSetup: Boolean, $mailServerName: String) {
  UpdateServices(isMailRoutingEnabled: $isMailRoutingEnabled, isMailboxEnabled: $isMailboxEnabled, isCalendarEnabled: $isCalendarEnabled, isRemindersEnabled: $isRemindersEnabled, isContactsEnabled: $isContactsEnabled, hasCompletedSetup: $hasCompletedSetup, mailServerName: $mailServerName) { ${CAPABILITIES} }
}`

type SetupAnswer = {
  GetServiceSetup: ServiceSetup
  GetServerStatus: { pendingRestart: string[] }
}
type AgentAnswer = { ReadAgent: { agent?: { id: string; enabled: boolean } | null; allowed: { enabled: boolean } } }

type ServiceSetup = {
  selectedServices: ServiceCapabilities
  runningServices: ServiceCapabilities
  hasModelProvider: boolean
  hasMailDomain: boolean
  mailServerName: string
}
const hostedKeys: HostedServiceKey[] = [
  'isMailRoutingEnabled',
  'isMailboxEnabled',
  'isCalendarEnabled',
  'isRemindersEnabled',
  'isContactsEnabled',
]

export function SetupPage() {
  const { t } = useTranslation()
  const toast = useToast()
  const { data, error, loading, reload } = useQuery(() => graphql<SetupAnswer>(SETUP))
  const agentQuery = useQuery(() => graphql<AgentAnswer>(READ_AGENT).catch(() => null))
  const [choices, setChoices] = useState<ServiceCapabilities | null>(null)
  const [mailServerName, setMailServerName] = useState('')
  const [isReviewing, setIsReviewing] = useState(false)
  const [isSaving, setIsSaving] = useState(false)
  const [problem, setProblem] = useState<unknown>()
  const [isSaved, setIsSaved] = useState(false)
  useEffect(() => {
    if (data) {
      setChoices(data.GetServiceSetup.selectedServices)
      setMailServerName(data.GetServiceSetup.mailServerName)
    }
  }, [data])
  if (error) return <ErrorMessage error={error} />
  if (loading || !choices || !data) return <Loading />
  const setup = data.GetServiceSetup
  const pendingRestart = data.GetServerStatus?.pendingRestart ?? []
  const readAgent = agentQuery.data?.ReadAgent
  const isAgentOn = readAgent?.agent?.enabled === true
  const canTurnOnAgent = setup.runningServices.isAssistantEnabled && readAgent?.allowed.enabled === true
  async function turnOnAgent() {
    setIsSaving(true)
    try {
      await graphql(TURN_ON_AGENT)
      toast.done(t('services.turnedOnMine'))
      await agentQuery.reload()
      window.dispatchEvent(new Event('focus'))
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    } finally {
      setIsSaving(false)
    }
  }
  const hasUnsavedChoices =
    hostedKeys.some((serviceKey) => choices[serviceKey] !== setup.selectedServices[serviceKey]) ||
    (choices.isMailRoutingEnabled && mailServerName !== setup.mailServerName)
  const labels = {
    isMailRoutingEnabled: t('services.mail'),
    isMailboxEnabled: t('nav.mail'),
    isCalendarEnabled: t('nav.calendar'),
    isRemindersEnabled: t('services.remindersLabel'),
    isContactsEnabled: t('nav.contacts'),
  }
  const descriptions = {
    isMailRoutingEnabled: t('services.routing'),
    isMailboxEnabled: t('services.mailboxes'),
    isCalendarEnabled: t('services.calendar'),
    isRemindersEnabled: t('services.reminders'),
    isContactsEnabled: t('services.contacts'),
  }
  function preset(isMailSelected: boolean) {
    setChoices(
      (previous) =>
        previous && {
          ...previous,
          isMailRoutingEnabled: isMailSelected,
          isMailboxEnabled: isMailSelected,
          isCalendarEnabled: false,
          isRemindersEnabled: false,
          isContactsEnabled: false,
        },
    )
    setIsReviewing(false)
  }
  async function apply(hasCompletedSetup = false) {
    if (!choices) return
    setIsSaving(true)
    setProblem(undefined)
    try {
      const variables = Object.fromEntries(hostedKeys.map((serviceKey) => [serviceKey, choices[serviceKey]]))
      await graphql(UPDATE, {
        ...variables,
        ...(hasCompletedSetup ? { hasCompletedSetup: true } : {}),
        ...(choices.isMailRoutingEnabled ? { mailServerName } : {}),
      })
      setIsSaved(true)
      setIsReviewing(false)
      await reload()
      window.dispatchEvent(new Event('focus'))
    } catch (caught) {
      setProblem(caught)
    } finally {
      setIsSaving(false)
    }
  }
  return (
    <>
      <section className="card">
        <h2>{t('services.choose')}</h2>
        <div className="page-actions">
          <button type="button" onClick={() => preset(false)} disabled={isSaving}>
            {t('services.assistant')}
          </button>
          <button type="button" onClick={() => preset(true)} disabled={isSaving}>
            {t('services.mail')}
          </button>
        </div>
        <h3>{t('services.custom')}</h3>
        {hostedKeys.map((serviceKey) => (
          <label className="checkbox" key={serviceKey}>
            <input
              type="checkbox"
              checked={choices[serviceKey]}
              disabled={isSaving || (serviceKey === 'isMailboxEnabled' && !choices.isMailRoutingEnabled)}
              onChange={(event) => {
                setChoices({
                  ...choices,
                  [serviceKey]: event.target.checked,
                  ...(serviceKey === 'isMailRoutingEnabled' && !event.target.checked
                    ? { isMailboxEnabled: false }
                    : {}),
                })
                setIsReviewing(false)
              }}
            />
            <span>
              <strong>{labels[serviceKey]}</strong>
              <br />
              <span className="muted">{descriptions[serviceKey]}</span>
            </span>
          </label>
        ))}
        {choices.isMailRoutingEnabled && (
          <label>
            {t('services.mailHostname')}
            <input
              value={mailServerName}
              onChange={(event) => setMailServerName(event.target.value)}
              autoComplete="off"
            />
          </label>
        )}
        <button type="button" onClick={() => setIsReviewing(true)} disabled={isSaving}>
          {t('services.review')}
        </button>
        {isReviewing && (
          <div className="card">
            <h3>{t('services.review')}</h3>
            <ul>
              {hostedKeys
                .filter((serviceKey) => choices[serviceKey])
                .map((serviceKey) => (
                  <li key={serviceKey}>{labels[serviceKey]}</li>
                ))}
            </ul>
            {!hostedKeys.some((serviceKey) => choices[serviceKey]) && <p>{t('services.assistant')}</p>}
            <p>{t('services.preserve')}</p>
            <button className="primary" type="button" disabled={isSaving} onClick={() => void apply()}>
              {t('services.apply')}
            </button>
            <button type="button" disabled={isSaving} onClick={() => setIsReviewing(false)}>
              {t('common.cancel')}
            </button>
          </div>
        )}
        {isSaved && <p role="status">{t('services.saved')}</p>}
        <ErrorMessage error={problem} />
      </section>
      <section className="card">
        <h2>{t('services.next')}</h2>
        <p className="muted">{t('services.stepsIntro')}</p>
        <table>
          <tbody>
            <SetupStep
              isDone={setup.hasModelProvider}
              title={<Link to="/manage/agent/models">{t('services.model')}</Link>}
              note={t(setup.hasModelProvider ? 'services.providerReady' : 'services.providerMissing')}
            />
            <SetupStep
              isDone={setup.selectedServices.isAssistantEnabled}
              title={<Link to="/manage/agent/general">{t('services.enableAssistant')}</Link>}
              note={t(
                setup.selectedServices.isAssistantEnabled ? 'services.assistantReady' : 'services.assistantMissing',
              )}
            />
            <SetupStep
              isDone={pendingRestart.length === 0 && !setup.runningServices.hasPendingServiceRestart}
              title={<Link to="/manage/server/about">{t('services.restartStep')}</Link>}
              note={
                pendingRestart.length > 0
                  ? t('services.restartPending', { settings: pendingRestart.join(', ') })
                  : setup.runningServices.hasPendingServiceRestart
                    ? t('services.restart')
                    : t('services.restartNone')
              }
            />
            <SetupStep
              isDone={isAgentOn}
              isWaiting={!isAgentOn && !canTurnOnAgent}
              title={t('services.yourAgent')}
              note={t(
                isAgentOn
                  ? 'services.yourAgentReady'
                  : canTurnOnAgent
                    ? 'services.yourAgentMissing'
                    : 'services.yourAgentWaiting',
              )}
              action={
                !isAgentOn &&
                canTurnOnAgent && (
                  <button type="button" className="primary" disabled={isSaving} onClick={() => void turnOnAgent()}>
                    {t('services.turnOnMine')}
                  </button>
                )
              }
            />
          </tbody>
        </table>
        <p className="muted">{t('services.modelDetail')}</p>
        {setup.selectedServices.isMailRoutingEnabled && (
          <p role="status">
            {t(setup.hasMailDomain ? 'services.domainReady' : 'services.domainMissing')}{' '}
            <Link to="/manage/domains">{t('services.domains')}</Link>
          </p>
        )}
        {(setup.selectedServices.isCalendarEnabled ||
          setup.selectedServices.isRemindersEnabled ||
          setup.selectedServices.isContactsEnabled) && (
          <p>
            <Link to="/settings/devices">{t('services.sync')}</Link>
          </p>
        )}
        {setup.runningServices.isAssistantEnabled && isAgentOn && (
          <p>
            <Link to="/agent">{t('services.try')}</Link>
          </p>
        )}
        <button
          type="button"
          disabled={isSaving || hasUnsavedChoices || setup.runningServices.hasPendingServiceRestart}
          onClick={() => void apply(true)}
        >
          {t('services.finish')}
        </button>
      </section>
      {setup.runningServices.isMailRoutingEnabled && <MailSetupPage />}
    </>
  )
}

// SetupStep is one row of the list of what is left: the step, what it found,
// and whether it is done. A step that cannot start until an earlier one is
// done says so rather than counting as left to do.
function SetupStep({
  isDone,
  isWaiting = false,
  title,
  note,
  action,
}: {
  isDone: boolean
  isWaiting?: boolean
  title: ReactNode
  note: string
  action?: ReactNode
}) {
  const { t } = useTranslation()
  return (
    <tr>
      <td>
        <strong>{title}</strong>
        <div className="muted cell-note">{note}</div>
        {action && <div className="page-actions">{action}</div>}
      </td>
      <td className="shrink">
        {isDone ? (
          <Tag value={t('services.done')} tone="good" />
        ) : isWaiting ? (
          <Tag value={t('services.later')} />
        ) : (
          <Tag value={t('services.toDo')} tone="warn" />
        )}
      </td>
    </tr>
  )
}

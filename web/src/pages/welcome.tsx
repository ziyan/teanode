import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import { graphql } from '../api'
import { AuthCard, AuthField } from '../components/authCard'
import { openDrawerOnNextLoad } from '../components/agentDrawer'
import { ErrorMessage } from '../components/common'
import { CheckIcon } from '../components/icons'
import { restartServer } from '../components/serverRestart'
import { useToast } from '../components/toast'
import { useTranslation } from '../i18n/i18n'

// Which hosted services already run, and whether chosen ones wait for a
// restart: what the second way in offers. Asked apart from everything else
// and allowed to fail, since it only changes what that block says.
const RUNNING = `{ GetServiceCapabilities { isMailboxEnabled isCalendarEnabled isRemindersEnabled isContactsEnabled isPhotosEnabled hasPendingServiceRestart } }`

type RunningServices = {
  isMailboxEnabled: boolean
  isCalendarEnabled: boolean
  isRemindersEnabled: boolean
  isContactsEnabled: boolean
  isPhotosEnabled: boolean
  hasPendingServiceRestart: boolean
}

// Where the second way in leads when nothing runs yet: service setup with
// Photos already ticked.
const CHOOSE_PHOTOS = '/manage/server/setup?choose=photos'

const SET_UP = `
  mutation ($kind: String!, $apiKey: String, $baseUrl: String) {
    SetUpAssistant(kind: $kind, apiKey: $apiKey, baseUrl: $baseUrl) {
      provider defaultModel embeddingModel fastModel isAgentOn needsRestart
    }
  }`

// What the server says when the service refused the key; the same words as
// AssistantKeyRefused in internal/api/v1api/apigraph/assistant_setup.go.
const KEY_REFUSED = 'the service refused the key'

// modelName is a chosen model without the provider it is saved under.
function modelName(chosen: string): string {
  return chosen.split(':').slice(1).join(':')
}

type AssistantSetup = {
  provider: string
  defaultModel: string
  embeddingModel: string
  fastModel: string
  isAgentOn: boolean
  needsRestart: boolean
}

// The model services a new assistant can start on from here, and where each
// hands out its keys. A model server of one's own speaks OpenAI's protocol,
// as Ollama and vLLM do, and is told by its address.
const SERVICES = [
  { id: 'openai', kind: 'openai', name: 'OpenAI', keys: 'https://platform.openai.com/api-keys' },
  { id: 'anthropic', kind: 'anthropic', name: 'Anthropic', keys: 'https://console.anthropic.com/settings/keys' },
  { id: 'gemini', kind: 'gemini', name: 'Gemini', keys: 'https://aistudio.google.com/apikey' },
  { id: 'own', kind: 'openai', name: '', keys: '' },
] as const

type ServiceId = (typeof SERVICES)[number]['id']
type Stage = 'form' | 'checking' | 'starting' | 'opening' | 'restartFailed'

// WelcomePage is where the first account lands on a server whose assistant
// has no model yet: one question, which model service, and one button that
// does the rest. Every step it takes can be taken one field at a time under
// Manage; this is the way in for somebody who has not seen any of that yet.
// See docs/planning/assistant-welcome-execplan.md.
export function WelcomePage() {
  const { t } = useTranslation()
  const toast = useToast()
  const [serviceId, setServiceId] = useState<ServiceId>('openai')
  const [apiKey, setApiKey] = useState('')
  const [address, setAddress] = useState('')
  const [stage, setStage] = useState<Stage>('form')
  const [setup, setSetup] = useState<AssistantSetup | null>(null)
  const [problem, setProblem] = useState<unknown>(null)
  const [running, setRunning] = useState<RunningServices | null>(null)
  useEffect(() => {
    graphql<{ GetServiceCapabilities: RunningServices }>(RUNNING)
      .then((answer) => setRunning(answer.GetServiceCapabilities))
      .catch(() => undefined)
  }, [])
  // The services that run now, in the order a person who did not come for an
  // assistant is most likely to want them: Photos first, since it runs on a
  // new server without being chosen.
  const openable = running
    ? [
        running.isPhotosEnabled && { path: '/photos', name: t('nav.photos') },
        running.isMailboxEnabled && { path: '/mailbox', name: t('nav.mail') },
        running.isCalendarEnabled && { path: '/calendar', name: t('nav.calendar') },
        running.isRemindersEnabled && { path: '/reminders', name: t('services.remindersLabel') },
        running.isContactsEnabled && { path: '/contacts', name: t('nav.contacts') },
      ].filter((each) => each !== false)
    : []
  const isWaitingForRestart = running?.hasPendingServiceRestart === true
  const service = SERVICES.find((each) => each.id === serviceId)!
  const isOwnServer = serviceId === 'own'
  const isReady = isOwnServer ? address.trim() !== '' : apiKey.trim() !== ''

  async function start(event: React.FormEvent) {
    event.preventDefault()
    setProblem(null)
    // A refusal from an earlier try would sit over the progress of this one.
    toast.clear()
    setStage('checking')
    let done: AssistantSetup
    try {
      const answer = await graphql<{ SetUpAssistant: AssistantSetup }>(SET_UP, {
        kind: service.kind,
        apiKey: apiKey.trim() || null,
        baseUrl: isOwnServer ? address.trim() : null,
      })
      done = answer.SetUpAssistant
      setSetup(done)
    } catch (caught) {
      // A refused key is said in the reader's words; anything else as the
      // server put it, without the prefix that only says it was refused.
      const message = caught instanceof Error ? caught.message : String(caught)
      setProblem(
        new Error(
          message.includes(KEY_REFUSED)
            ? t('welcome.refused', { service: service.name || t('welcome.ownServer') })
            : message.replace(/^api: invalid arguments: /, ''),
        ),
      )
      setStage('form')
      return
    }
    if (done.needsRestart) {
      setStage('starting')
      // A refusal says why, such as a server not started in a way that lets
      // it restart; a server that did not come back has nothing to say.
      const isBack = await restartServer().catch((caught: unknown) => {
        setProblem(caught)
        return false
      })
      if (!isBack) {
        setStage('restartFailed')
        return
      }
    }
    open()
  }

  // The new process has the assistant; a fresh page meets it with the chat
  // open, which is where the agent's first greeting arrives.
  function open() {
    setStage('opening')
    openDrawerOnNextLoad()
    window.location.assign('/')
  }

  if (stage !== 'form') {
    const steps: { stage: Stage; label: string }[] = [
      { stage: 'checking', label: t('welcome.checking') },
      { stage: 'starting', label: t('welcome.starting') },
      { stage: 'opening', label: t('welcome.opening') },
    ]
    const reached = steps.findIndex((step) => step.stage === (stage === 'restartFailed' ? 'starting' : stage))
    return (
      <AuthCard purpose={t('welcome.working')} onSubmit={(event) => event.preventDefault()}>
        <ol className="welcome-progress" aria-live="polite">
          {steps.map((step, index) => (
            <li key={step.stage} className={index < reached ? 'done' : index === reached ? 'current' : ''}>
              <span className="welcome-progress-mark" aria-hidden="true">
                {index < reached ? (
                  <CheckIcon size={14} />
                ) : index === reached && stage !== 'restartFailed' ? (
                  <span className="welcome-spinner" />
                ) : null}
              </span>
              <span>
                {step.label}
                {step.stage === 'checking' && setup && (
                  <span className="muted welcome-progress-note">
                    {setup.fastModel
                      ? t('welcome.choseWithFast', {
                          model: modelName(setup.defaultModel),
                          fast: modelName(setup.fastModel),
                        })
                      : t('welcome.chose', { model: modelName(setup.defaultModel) })}
                  </span>
                )}
              </span>
            </li>
          ))}
        </ol>
        {stage === 'restartFailed' && (
          <>
            <ErrorMessage error={problem} />
            <p className="error" role="alert">
              {t('welcome.restartFailed')}
            </p>
            <button className="primary auth-button" type="button" onClick={open}>
              {t('welcome.restarted')}
            </button>
          </>
        )}
      </AuthCard>
    )
  }

  return (
    <AuthCard
      purpose={t('welcome.intro')}
      footnote={
        <>
          <Link className="button-quiet" to="/manage/agent/models">
            {t('welcome.otherModels')}
          </Link>{' '}
          <Link className="button-quiet" to="/manage/server/setup">
            {t('welcome.otherSetup')}
          </Link>
        </>
      }
      onSubmit={start}
    >
      <div className="auth-field">
        <span id="welcome-service">{t('welcome.service')}</span>
        <div className="welcome-services" role="group" aria-labelledby="welcome-service">
          {SERVICES.map((each) => (
            <button
              key={each.id}
              type="button"
              aria-pressed={each.id === serviceId}
              onClick={() => {
                setServiceId(each.id)
                setProblem(null)
                // One service's key is never sent to another.
                setApiKey('')
              }}
            >
              {each.name || t('welcome.ownServer')}
            </button>
          ))}
        </div>
      </div>
      {isOwnServer && (
        <AuthField
          label={t('welcome.address')}
          value={address}
          placeholder="http://192.168.1.20:11434/v1"
          onChange={(event) => setAddress(event.target.value)}
          autoComplete="off"
          spellCheck={false}
          hint={<span className="muted auth-hint">{t('welcome.addressHint')}</span>}
        />
      )}
      <AuthField
        label={isOwnServer ? t('welcome.keyOptional') : t('welcome.key', { service: service.name })}
        type="password"
        value={apiKey}
        onChange={(event) => setApiKey(event.target.value)}
        autoComplete="off"
        hint={
          isOwnServer ? undefined : (
            <span className="muted auth-hint">
              {t('welcome.keyHint')}{' '}
              <a href={service.keys} target="_blank" rel="noreferrer">
                {t('welcome.getKey')}
              </a>
            </span>
          )
        }
      />
      <ErrorMessage error={problem} />
      <button className="primary auth-button" type="submit" disabled={!isReady}>
        {t('welcome.start')}
      </button>
      {/* For somebody who came for their photos, mail, calendar or contacts
          and has no use for an assistant yet: what already runs, one click
          away, or the setup where it is chosen when nothing does. Links that
          look like buttons, since they go to another page. */}
      <div className="welcome-other">
        {openable.length > 0 ? (
          <>
            <p className="muted">{t('welcome.servicesRunning')}</p>
            {openable.map((each) => (
              <Link key={each.path} className="button auth-button" to={each.path}>
                {t('welcome.openService', { service: each.name })}
              </Link>
            ))}
          </>
        ) : (
          <>
            <p className="muted">{t(isWaitingForRestart ? 'welcome.servicesWaiting' : 'welcome.servicesPrompt')}</p>
            <Link className="button auth-button" to={CHOOSE_PHOTOS}>
              {t(isWaitingForRestart ? 'welcome.servicesContinue' : 'welcome.servicesButton')}
            </Link>
          </>
        )}
      </div>
    </AuthCard>
  )
}

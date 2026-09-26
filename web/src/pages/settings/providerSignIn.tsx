import { useEffect, useRef, useState } from 'react'

import { graphql } from '../../api'
import { useToast } from '../../components/toast'
import { useTranslation } from '../../i18n/i18n'

// Signing a provider in to a person's plan, for the kinds that run on one
// rather than on a key. The server asks the service for a one-time code;
// the operator types it on the service's own page, from any device, and
// this asks the server every little while whether it has been. Nothing
// the operator types here is a password.

type SignIn = {
  signInId: string
  userCode: string
  verificationAddress: string
  isSignedIn: boolean
  account: string
  plan: string
}

const FIELDS = 'signInId userCode verificationAddress isSignedIn account plan'
const BEGIN = `mutation ($provider: String!) { BeginAgentProviderSignIn(provider: $provider) { ${FIELDS} } }`
const FINISH = `mutation ($signInId: String!) { FinishAgentProviderSignIn(signInId: $signInId) { ${FIELDS} } }`

export function ProviderSignIn({
  provider,
  isSignedIn,
  disabled,
  onSignedIn,
}: {
  provider: string
  isSignedIn: boolean
  disabled: boolean
  onSignedIn: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [waiting, setWaiting] = useState<SignIn | null>(null)
  const [busy, setBusy] = useState(false)
  // Closing the dialog stops the asking; the code simply runs out.
  const isOpen = useRef(true)
  useEffect(
    () => () => {
      isOpen.current = false
    },
    [],
  )

  async function begin() {
    setBusy(true)
    try {
      const data = await graphql<{ BeginAgentProviderSignIn: SignIn }>(BEGIN, { provider: provider.trim() })
      const started = data.BeginAgentProviderSignIn
      setWaiting(started)
      // Each finish waits on the server for up to half a minute, so this
      // asks again as soon as one answers that the code is still unused.
      while (isOpen.current) {
        const answered = await graphql<{ FinishAgentProviderSignIn: SignIn }>(FINISH, { signInId: started.signInId })
        const finished = answered.FinishAgentProviderSignIn
        if (finished.isSignedIn) {
          toast.done(
            finished.plan
              ? t('agentSettings.signedInToPlan', { plan: finished.plan })
              : t('agentSettings.signedInDone'),
          )
          setWaiting(null)
          onSignedIn()
          return
        }
      }
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
      setWaiting(null)
    } finally {
      setBusy(false)
    }
  }

  if (waiting) {
    return (
      <div className="provider-sign-in">
        <p>{t('agentSettings.signInSteps')}</p>
        <p>
          <a href={waiting.verificationAddress} target="_blank" rel="noreferrer">
            {waiting.verificationAddress}
          </a>
        </p>
        <p className="provider-sign-in-code mono">{waiting.userCode}</p>
        <p className="muted">{t('agentSettings.signInWaiting')}</p>
      </div>
    )
  }
  return (
    <div className="provider-sign-in">
      <p className="muted">{isSignedIn ? t('agentSettings.signInAgainHint') : t('agentSettings.signInHint')}</p>
      <button
        type="button"
        className={isSignedIn ? '' : 'primary'}
        disabled={disabled || busy || !provider.trim()}
        onClick={() => void begin()}
      >
        {isSignedIn ? t('agentSettings.signInAgain') : t('agentSettings.signIn')}
      </button>
    </div>
  )
}

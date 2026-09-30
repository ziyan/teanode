import { useCallback, useEffect, useRef, useState } from 'react'

import { graphql } from '../api'
import { AuthCard } from '../components/authCard'
import { useToast } from '../components/toast'
import { useTranslation } from '../i18n/i18n'
import { COMPLETE_FINANCE_LINK, COMPLETE_FINANCE_REPAIR, CREATE_FINANCE_LINK_TOKEN } from './finance/financeApi'

// The page Plaid's window runs on, /finance-link.
//
// A page of its own, outside the dashboard's frame, because it is the one
// page whose security policy lets Plaid's script, Plaid's frame and Plaid's
// API in; the rest of the dashboard renders mail from strangers and keeps
// its stricter policy. The Finance tab opens it in a window of its own, with
// ?source=<id> to sign in to an existing finance source again rather than
// link a new one, and it closes itself when it is done.
//
// Only the one script is loaded, from the one address the policy allows.

const PLAID_SCRIPT = 'https://cdn.plaid.com/link/v2/stable/link-initialize.js'

// The little of Plaid's window this page uses. Plaid's own names, since
// they are what its script defines.
type PlaidInstitution = { institution_id?: string | null; name?: string | null }
type PlaidSuccessMetadata = { institution?: PlaidInstitution | null }
type PlaidExitError = { error_code?: string; error_message?: string; display_message?: string | null } | null
type PlaidHandler = { open: () => void; exit: () => void; destroy: () => void }
type PlaidLink = {
  create: (options: {
    token: string
    onSuccess: (publicToken: string, metadata: PlaidSuccessMetadata) => void
    onExit: (error: PlaidExitError) => void
  }) => PlaidHandler
}
type WindowWithPlaid = Window & { Plaid?: PlaidLink }

// loadPlaid puts Plaid's script on the page once and waits for it.
let loading: Promise<PlaidLink> | null = null
function loadPlaid(): Promise<PlaidLink> {
  const existing = (window as WindowWithPlaid).Plaid
  if (existing) return Promise.resolve(existing)
  if (loading) return loading
  loading = new Promise<PlaidLink>((resolve, reject) => {
    const script = document.createElement('script')
    script.src = PLAID_SCRIPT
    script.async = true
    script.onload = () => {
      const plaid = (window as WindowWithPlaid).Plaid
      if (plaid) {
        resolve(plaid)
      } else {
        reject(new Error('Plaid'))
      }
    }
    script.onerror = () => reject(new Error('Plaid'))
    document.head.appendChild(script)
  }).catch((caught) => {
    loading = null
    throw caught
  })
  return loading
}

type Phase = 'starting' | 'open' | 'finishing' | 'done' | 'stopped'

export function FinanceLinkPage() {
  const { t } = useTranslation()
  const toast = useToast()
  const sourceId = new URLSearchParams(window.location.search).get('source') ?? ''
  const [phase, setPhase] = useState<Phase>('starting')
  const handler = useRef<PlaidHandler | null>(null)
  const isOpenedByDashboard = !!window.opener && window.opener !== window

  const heading = sourceId ? t('financeLink.repairTitle') : t('financeLink.title')
  useEffect(() => {
    document.title = `${heading} · ${t('app.name')}`
  }, [heading, t])

  const finish = useCallback(
    async (publicToken: string, metadata: PlaidSuccessMetadata) => {
      setPhase('finishing')
      try {
        if (sourceId) {
          await graphql(COMPLETE_FINANCE_REPAIR, { sourceId })
        } else {
          await graphql(COMPLETE_FINANCE_LINK, {
            publicToken,
            institutionId: metadata.institution?.institution_id ?? undefined,
            institutionName: metadata.institution?.name ?? undefined,
          })
        }
        setPhase('done')
        toast.done(sourceId ? t('financeLink.repaired') : t('financeLink.linked'))
        // Back to the Finance tab that opened this, which reads its list
        // again when this window closes. A browser that will not let a
        // page close itself leaves the button below.
        if (isOpenedByDashboard) window.setTimeout(() => window.close(), 1500)
      } catch (caught) {
        setPhase('stopped')
        toast.failure(caught, t('financeLink.failed'))
      }
    },
    [sourceId, toast, t, isOpenedByDashboard],
  )

  const start = useCallback(async () => {
    setPhase('starting')
    try {
      const answer = await graphql<{ CreateFinanceLinkToken: { linkToken: string } }>(CREATE_FINANCE_LINK_TOKEN, {
        sourceId: sourceId || undefined,
      })
      const plaid = await loadPlaid().catch(() => {
        throw new Error(t('financeLink.scriptFailed'))
      })
      handler.current?.destroy()
      handler.current = plaid.create({
        token: answer.CreateFinanceLinkToken.linkToken,
        onSuccess: (publicToken, metadata) => void finish(publicToken, metadata),
        onExit: (error) => {
          setPhase('stopped')
          if (error) toast.failed(error.display_message || error.error_message || t('financeLink.failed'))
        },
      })
      handler.current.open()
      setPhase('open')
    } catch (caught) {
      setPhase('stopped')
      toast.failure(caught, t('financeLink.failed'))
    }
  }, [sourceId, finish, toast, t])

  useEffect(() => {
    void start()
    return () => handler.current?.destroy()
    // Once, when the page opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const back = () => {
    if (isOpenedByDashboard) {
      window.close()
      return
    }
    window.location.assign('/settings/agent/finance/sources')
  }

  return (
    <AuthCard purpose={heading} onSubmit={(event) => event.preventDefault()}>
      {phase === 'starting' || phase === 'open' ? <p className="muted">{t('financeLink.opening')}</p> : null}
      {phase === 'finishing' ? <p className="muted">{t('financeLink.finishing')}</p> : null}
      {phase === 'done' ? <p>{sourceId ? t('financeLink.repairedBody') : t('financeLink.linkedBody')}</p> : null}
      {phase === 'stopped' ? <p className="muted">{t('financeLink.stopped')}</p> : null}
      <div className="auth-actions">
        {phase === 'stopped' ? (
          <button type="button" className="primary" onClick={() => void start()}>
            {t('financeLink.tryAgain')}
          </button>
        ) : null}
        <button type="button" className={phase === 'done' ? 'primary' : undefined} onClick={back}>
          {t('financeLink.back')}
        </button>
      </div>
    </AuthCard>
  )
}

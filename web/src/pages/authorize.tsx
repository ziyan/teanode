import React, { useCallback, useEffect, useState } from 'react'

import { graphql } from '../api'
import { AuthCard } from '../components/authCard'
import { ErrorMessage } from '../components/common'
import { useTranslation } from '../i18n/i18n'

// The page a program sends somebody to when it wants to act for them.
//
// A harness -- a coding agent, an editor -- discovered this server, registered
// itself, and opened this address in a browser. Nothing has been granted yet:
// a registration proves nothing, and until somebody presses Allow here the
// program can do nothing at all.
//
// Drawn the way the sign-in form and the command line page are, one card and
// nothing else, because it is the same kind of moment. The reader was brought
// here by something else to answer one question, and a rail would be a list of
// other places to go. It also means the page needs no separate sign-in: when
// nobody is signed in the shell shows the login form at this address instead,
// with passkeys and single sign-on, and this page renders once they are.

const REQUEST = `
  query ($clientId: String!, $redirectUri: String!) {
    ReadOAuthAuthorizationRequest(clientId: $clientId, redirectUri: $redirectUri) {
      clientName
      redirectHost
      username
      registered
    }
  }`

const APPROVE = `
  mutation ($clientId: String!, $redirectUri: String!, $codeChallenge: String!, $state: String, $resource: String) {
    ApproveOAuthAuthorization(
      clientId: $clientId
      redirectUri: $redirectUri
      codeChallenge: $codeChallenge
      state: $state
      resource: $resource
    ) {
      redirectUrl
    }
  }`

type AuthorizationRequest = {
  clientName: string
  redirectHost: string
  username: string
  registered: string
}

type Phase = 'reading' | 'asking' | 'working' | 'sending'

export function AuthorizePage() {
  const { t } = useTranslation()

  const query = new URLSearchParams(window.location.search)
  const clientId = query.get('client_id') ?? ''
  const redirectUri = query.get('redirect_uri') ?? ''
  const codeChallenge = query.get('code_challenge') ?? ''
  const codeChallengeMethod = query.get('code_challenge_method') ?? ''
  const responseType = query.get('response_type') ?? ''
  const state = query.get('state')
  const resource = query.get('resource')

  const [phase, setPhase] = useState<Phase>('reading')
  const [request, setRequest] = useState<AuthorizationRequest | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    document.title = `${t('authorize.title')} · ${t('app.name')}`
  }, [t])

  // What the program asked for has to be usable before anybody is asked to
  // approve it. These are checked here so a malformed request is a sentence
  // the reader can act on, rather than a failure after they have pressed
  // Allow.
  const asked =
    clientId !== '' &&
    redirectUri !== '' &&
    responseType === 'code' &&
    codeChallenge !== '' &&
    codeChallengeMethod === 'S256'

  useEffect(() => {
    if (!asked) return
    let current = true
    graphql<{ ReadOAuthAuthorizationRequest: AuthorizationRequest }>(REQUEST, { clientId, redirectUri })
      .then((data) => {
        if (!current) return
        setRequest(data.ReadOAuthAuthorizationRequest)
        setPhase('asking')
      })
      .catch((caught: unknown) => {
        if (!current) return
        setError(caught instanceof Error ? caught.message : t('authorize.unknownProgram'))
        setPhase('asking')
      })
    return () => {
      current = false
    }
  }, [asked, clientId, redirectUri, t])

  const allow = useCallback(
    async (event: React.FormEvent) => {
      event.preventDefault()
      setError(null)
      setPhase('working')
      try {
        const data = await graphql<{ ApproveOAuthAuthorization: { redirectUrl: string } }>(APPROVE, {
          clientId,
          redirectUri,
          codeChallenge,
          state,
          resource,
        })
        // Leaving the dashboard for the program's own address. Replaced
        // rather than pushed, so the back button does not return to a page
        // whose approval has already been spent.
        setPhase('sending')
        window.location.replace(data.ApproveOAuthAuthorization.redirectUrl)
      } catch (caught) {
        setError(caught instanceof Error ? caught.message : t('authorize.failed'))
        setPhase('asking')
      }
    },
    [clientId, redirectUri, codeChallenge, state, resource, t],
  )

  // Refusing is not reported back to the program. Telling it that it was
  // refused, at an address it chose, is telling whoever sent the reader here
  // that somebody was home and said no.
  const refuse = useCallback(() => {
    window.location.replace('/')
  }, [])

  if (!asked) {
    return (
      <AuthCard purpose={t('authorize.title')} onSubmit={(event) => event.preventDefault()}>
        <p className="muted">{t('authorize.notOpenedByProgram')}</p>
      </AuthCard>
    )
  }

  if (phase === 'reading') {
    return (
      <AuthCard purpose={t('authorize.title')} onSubmit={(event) => event.preventDefault()}>
        <p className="muted">{t('authorize.reading')}</p>
      </AuthCard>
    )
  }

  if (!request) {
    // Nothing can be approved, so the reason is the whole of the page rather
    // than the outcome of something the reader pressed. A toast alone left
    // an empty card once it faded, which reads as a page that failed to load.
    // The toast still carries the server's own words; the card says what the
    // reader can check, which is where the program asked to be sent.
    return (
      <AuthCard purpose={t('authorize.title')} onSubmit={(event) => event.preventDefault()}>
        <p className="muted">{t('authorize.unknownProgram')}</p>
        {redirectHostOf(redirectUri) && (
          <p className="authorize-asked muted">
            {t('authorize.askedFor')} <Host name={redirectHostOf(redirectUri)} />
          </p>
        )}
        <ErrorMessage error={error} />
      </AuthCard>
    )
  }

  if (phase === 'sending') {
    return (
      <AuthCard purpose={t('authorize.title')} onSubmit={(event) => event.preventDefault()}>
        <p className="muted">{t('authorize.sending')}</p>
      </AuthCard>
    )
  }

  return (
    <AuthCard purpose={t('authorize.title')} onSubmit={allow}>
      <p className="authorize-intro">{t('authorize.intro')}</p>

      <dl className="authorize-facts">
        <dt>{t('authorize.program')}</dt>
        <dd>
          <span className="authorize-name">{request.clientName || t('authorize.unnamed')}</span>
          {/* Anybody may register under any name, so the page says the name
              is claimed rather than presenting it as established. The address
              below it is the part a reader can actually judge. */}
          <span className="authorize-claimed muted">{t('authorize.claimedName')}</span>
        </dd>

        <dt>{t('authorize.sendsTo')}</dt>
        <dd>
          <Host name={request.redirectHost} />
        </dd>

        <dt>{t('authorize.actsAs')}</dt>
        <dd>{request.username}</dd>
      </dl>

      <p className="authorize-grant muted">{t('authorize.grant')}</p>

      <ErrorMessage error={error} />

      <div className="authorize-actions">
        <button className="primary auth-button" type="submit" disabled={phase === 'working'}>
          {phase === 'working' ? t('authorize.working') : t('authorize.allow')}
        </button>
        <button className="auth-button" type="button" onClick={refuse} disabled={phase === 'working'}>
          {t('authorize.refuse')}
        </button>
      </div>
    </AuthCard>
  )
}

// redirectHostOf is the host an address names, or '' when it names none. The
// address came from a query string anybody could write, so it is shown only
// as a host, and only when it parses.
function redirectHostOf(address: string): string {
  try {
    return new URL(address).host
  } catch {
    return ''
  }
}

// Host is a host name that may break after each dot and nowhere else, unless
// one of its labels is longer than the card. A reader checks a host by its
// labels, so a break in the middle of one makes it harder to read
// at exactly the moment it is being judged.
function Host({ name }: { name: string }) {
  const labels = name.split('.')
  return (
    <code className="authorize-host">
      {labels.map((label, index) => (
        <React.Fragment key={index}>
          {label}
          {index < labels.length - 1 && (
            <>
              .<wbr />
            </>
          )}
        </React.Fragment>
      ))}
    </code>
  )
}

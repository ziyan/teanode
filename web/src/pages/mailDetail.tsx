import { useEffect, useMemo, useState } from 'react'
import { useParams } from 'react-router-dom'

import { AuthenticationResults, Delivery, Mail, MailContent, MailOpens, graphql } from '../api'
import {
  ErrorMessage,
  KindTag,
  Loading,
  Tag,
  formatBytes,
  formatTime,
  toneFor,
  useEnumLabel,
} from '../components/common'
import { useQuery } from '../components/useQuery'
import { MessageFrame } from '../components/messageFrame'
import { HighlightedHtml } from '../components/highlightHtml'
import { useBreadcrumbDetail } from '../components/breadcrumb'
import { Key, useTranslation } from '../i18n/i18n'
import { useResolvedTheme } from '../components/theme'

// Teaching the built-in filter. The classifier is the part that does most of
// the work and it learns nothing on its own, so marking a message has to be
// one press from reading it.
const MARK = `
  mutation ($mailId: String!, $label: String!) {
    MarkMail(mailId: $mailId, label: $label) { mailId label learnedSpam learnedHam }
  }`

const FORGET = `
  mutation ($mailId: String!) {
    ForgetMail(mailId: $mailId) { mailId label learnedSpam learnedHam }
  }`

const MAIL = `
  query ($mailId: String!) {
    GetSpamTraining(mailId: $mailId) { label }
    GetSettings { antispam { effectiveEngine bayesEnabled bayesMinimumMessages bayesLearnedSpam bayesLearnedHam } }
    GetMail(mailId: $mailId) {
      id sender from subject recipients status kind size receivedAt
      ip rdns hello messageId envelopeId tlsVersion tlsCipherSuite
      location { city country }
      authenticationResults {
        senderMx { domain mailServers }
        fromMx { domain mailServers }
        spf { domain ip result }
        dmarc { domain policy subdomainPolicy dkimAlignment spfAlignment result }
        dkims { domain selector identifier result }
        arc { result instances }
        spamFilter { score result symbols checks { symbol score description } }
        antivirus { viruses }
        contentFilter { unsafeExtensions }
        errors
      }
    }
    GetMailContent(mailId: $mailId) {
      mailId available text html hasRemoteContent size rawHeaders
      headers { key value }
      attachments { index filename contentType size inline }
    }
    GetMailOpens(mailId: $mailId) {
      trackable opened openedAt lastOpenedAt openCount ip
    }
    ListDeliveriesByMail(mailId: $mailId) {
      id recipient kind status size attempts error method destination
      attemptedAt deliveredAt droppedAt notifiedAt retryAt
      deliveryStatuses {
        reportingMta
        recipientStatuses { finalRecipient action status remoteMta diagnosticCode lastAttemptDate }
      }
    }
  }`

type Response = {
  GetSpamTraining: { label: string } | null
  GetSettings: {
    antispam: {
      effectiveEngine: string
      bayesEnabled: boolean
      bayesMinimumMessages: number
      bayesLearnedSpam: number
      bayesLearnedHam: number
    }
  }
  GetMail: Mail
  GetMailContent: MailContent
  GetMailOpens: MailOpens
  ListDeliveriesByMail: Delivery[]
}

// A check is one thing that was verified about this message: what it is, what
// it decided, and — the part a row of bare tags never carried — what it
// actually looked at to decide. "DKIM pass" does not say which key signed,
// and the key is what anybody chasing a forgery needs.
type Check = {
  label: string
  verdict: string
  tone?: 'good' | 'bad' | 'warn'
  detail?: React.ReactNode
}

type Tab = 'rendered' | 'text' | 'html' | 'source' | 'raw'

// A score is a sum of floating-point weights, and a sum of floats is
// 11.606000000000002. One decimal: the threshold is a whole number and the
// question a reader has is "how far past it", which 11.6 answers and 11.606
// only obscures. Through Number so that 3.0 reads as 3 and -1.0 as -1.
function formatScore(score: number): string {
  return String(Number(score.toFixed(1)))
}

// What the two training mutations return.
type Training = { mailId: string; label: string; learnedSpam: number; learnedHam: number }

export function MailDetailPage() {
  const { t, plural } = useTranslation()
  const label = useEnumLabel()
  const { mailId } = useParams()
  const { data, error, loading } = useQuery(() => graphql<Response>(MAIL, { mailId }), [mailId])
  const [chosen, setChosen] = useState<Tab | null>(null)
  const [loadRemote, setLoadRemote] = useState(false)

  // Reading in the dark. The frame's document is built as a string, so the
  // dashboard's theme has to be resolved here and written in as literals.
  // "darkened" is the reader's choice to invert a message that paints its
  // own light ground, remembered per reader rather than per message: whoever
  // wants dark mail wants it for the next message too.
  const resolvedTheme = useResolvedTheme()
  const dark = resolvedTheme === 'dark'
  const [darkened, setDarkened] = useState(() => {
    try {
      return window.localStorage.getItem(DARKENED_KEY) === '1'
    } catch {
      return false
    }
  })
  const [alreadyDark, setAlreadyDark] = useState(false)
  const chooseDarkened = (next: boolean) => {
    setDarkened(next)
    try {
      window.localStorage.setItem(DARKENED_KEY, next ? '1' : '0')
    } catch {
      // A browser that refuses storage still gets the choice for this visit.
    }
  }
  const [marked, setMarked] = useState<string | null>(null)
  const [marking, setMarking] = useState(false)
  const [markError, setMarkError] = useState<string | null>(null)
  const [learned, setLearned] = useState<{ spam: number; ham: number } | null>(null)

  // What the server already knows: whether this message was taught, and how
  // far the classifier has got. Local state only ever knew about a marking
  // made on this visit, so a reload offered to teach a message again.
  useEffect(() => {
    setMarked(data?.GetSpamTraining?.label ?? null)
    const antispam = data?.GetSettings?.antispam
    setLearned(antispam ? { spam: antispam.bayesLearnedSpam, ham: antispam.bayesLearnedHam } : null)
  }, [data])

  // Marked here rather than re-fetched: the score on this message is what it
  // was when it arrived, and marking it does not change that. What changes is
  // what the next message will be scored against.
  const mark = async (label: 'spam' | 'ham' | null) => {
    if (!mailId) return
    setMarking(true)
    setMarkError(null)
    try {
      const result = label
        ? await graphql<{ MarkMail: Training }>(MARK, { mailId, label })
        : await graphql<{ ForgetMail: Training }>(FORGET, { mailId })
      const training = 'MarkMail' in result ? result.MarkMail : result.ForgetMail
      setMarked(label)
      setLearned({ spam: training.learnedSpam, ham: training.learnedHam })
    } catch (failure) {
      setMarkError(failure instanceof Error ? failure.message : String(failure))
    } finally {
      setMarking(false)
    }
  }

  useBreadcrumbDetail(data?.GetMail?.subject || (data ? t('mail.noSubject') : null))

  const content = data?.GetMailContent
  const document = useMemo(
    () =>
      content?.html ? buildDocument(content.html, loadRemote ? mailId : undefined, dark, dark && darkened) : '',
    [content?.html, loadRemote, mailId, dark, darkened],
  )

  if (loading) {
    return <Loading />
  }
  if (error) {
    return <ErrorMessage error={error} />
  }
  if (!data) {
    return <p className="muted">{t('common.notFound')}</p>
  }

  const mail = data.GetMail
  const results = mail.authenticationResults

  // A message with no HTML has no rendered view and no markup behind one, so
  // those two tabs are not offered rather than offered and disabled — a
  // control that can never be used is a thing to wonder about. Plain text
  // opens on its text, which is the whole of what it is.
  const hasHtml = Boolean(content?.html)
  const tab = chosen ?? (hasHtml ? 'rendered' : 'text')

  return (
    <>
      {/* What happened to this message, before anything else about it. The
          page used to open with the subject and the sender, which are the two
          things a reader already knew — they clicked the row. */}
      <div className="verdict">
        <Tag value={label.status(mail.status) || t('status.unknown')} tone={toneFor(mail.status)} />
        <KindTag value={mail.kind} />
        <span className="muted">{verdictLine(t, plural, mail, data.ListDeliveriesByMail)}</span>
      </div>

      {/* Teaching the filter, only when something reads what it is taught:
          the built-in filter with its classifier on. Offered on every message
          rather than only on ones it got wrong, because the classifier needs
          examples of ordinary mail at least as much as examples of spam.

          Two toggles rather than two links and an undo: the pressed one is
          the message's current label, and pressing it again clears it. */}
      {data.GetSettings?.antispam?.effectiveEngine === 'builtin' && data.GetSettings.antispam.bayesEnabled ? (
        <div className="mark-spam">
          <div className="segmented" role="group" aria-label={t('mailDetail.markPrompt')}>
            <button
              type="button"
              className={marked === 'spam' ? 'active bad' : ''}
              aria-pressed={marked === 'spam'}
              disabled={marking}
              onClick={() => mark(marked === 'spam' ? null : 'spam')}
            >
              {t('mailDetail.markSpam')}
            </button>
            <button
              type="button"
              className={marked === 'ham' ? 'active good' : ''}
              aria-pressed={marked === 'ham'}
              disabled={marking}
              onClick={() => mark(marked === 'ham' ? null : 'ham')}
            >
              {t('mailDetail.markHam')}
            </button>
          </div>
          <span className="muted">
            {markError
              ? markError
              : marked
                ? marked === 'spam'
                  ? t('mailDetail.markedSpam')
                  : t('mailDetail.markedHam')
                : t('mailDetail.markPrompt')}
            {learned
              ? ' · ' +
                (learned.spam + learned.ham >= data.GetSettings.antispam.bayesMinimumMessages
                  ? t('mailDetail.classifierReady', { learned: learned.spam + learned.ham })
                  : t('mailDetail.classifierLearning', {
                      learned: learned.spam + learned.ham,
                      minimum: data.GetSettings.antispam.bayesMinimumMessages,
                    }))
              : ''}
          </span>
        </div>
      ) : null}

      <div className="card">
        <table className="detail">
          <tbody>
            <Field label={t('mail.from')}>{mail.from || mail.sender}</Field>
            <Field label={t('mailDetail.envelopeSender')} mono>
              {mail.sender}
            </Field>
            <Field label={t('mailDetail.to')}>{(mail.recipients ?? []).join(', ')}</Field>
            <Field label={t('mail.received')}>
              {t('mailDetail.receivedFrom', { time: formatTime(mail.receivedAt), ip: mail.ip ?? '' })}
              {mail.rdns ? ` (${mail.rdns.replace(/\.$/, '')})` : ''}
              {mail.location?.country ? ` · ${[mail.location.city, mail.location.country].filter(Boolean).join(', ')}` : ''}
            </Field>
            {/* Whether the hop into this server was encrypted, and how the
                sender introduced itself. Both are part of "who sent this",
                and neither was shown anywhere. */}
            <Field label={t('mailDetail.connection')}>
              {mail.tlsVersion
                ? `${mail.tlsVersion}${mail.tlsCipherSuite ? ` · ${mail.tlsCipherSuite}` : ''}`
                : t('mailDetail.noTls')}
              {mail.hello ? ` · HELO ${mail.hello}` : ''}
            </Field>
            <Field label={t('mailDetail.messageId')} mono>
              {mail.messageId}
            </Field>
            <Field label={t('mail.size')}>{formatBytes(mail.size)}</Field>
          </tbody>
        </table>
      </div>

      {results && <Authentication results={results} />}

      <Opens opens={data.GetMailOpens} />

      <div className="card">
        <h3>{t('mailDetail.deliveries')}</h3>
        {data.ListDeliveriesByMail.length === 0 ? (
          <p className="muted" style={{ margin: 0 }}>
            {t('mailDetail.noDeliveries')}
          </p>
        ) : (
          data.ListDeliveriesByMail.map((delivery) => (
            <DeliveryDetail key={delivery.id} delivery={delivery} />
          ))
        )}
      </div>

      <h3>{t('mailDetail.message')}</h3>
      {!content?.available ? (
        <p className="muted">{t('mailDetail.notStored')}</p>
      ) : (
        <>
          <div className="tabs">
            {hasHtml && (
              <button className={tab === 'rendered' ? 'active' : ''} onClick={() => setChosen('rendered')}>
                {t('mailDetail.rendered')}
              </button>
            )}
            <button
              className={tab === 'text' ? 'active' : ''}
              onClick={() => setChosen('text')}
              disabled={!content.text}
            >
              {t('mailDetail.text')}
            </button>
            {/* The markup behind the rendered view. What the frame shows has
                been sanitised and rewritten; when it looks wrong, this is the
                only way to see what it is actually rendering. */}
            {hasHtml && (
              <button className={tab === 'html' ? 'active' : ''} onClick={() => setChosen('html')}>
                {t('mailDetail.html')}
              </button>
            )}
            <button className={tab === 'source' ? 'active' : ''} onClick={() => setChosen('source')}>
              {t('mailDetail.headers')}
            </button>
            <button
              className={tab === 'raw' ? 'active' : ''}
              onClick={() => setChosen('raw')}
              disabled={!content.rawHeaders}
            >
              {t('mailDetail.rawHeaders')}
            </button>
            {/* A real link, so the browser saves it the way it saves anything
                else, and middle-click and "save as" both work. */}
            <a className="tab-action" href={`/api/v1/mail/${mail.id}/raw`} download>
              {t('mailDetail.download')}
            </a>
          </div>

          {tab === 'rendered' && content.html && (
            <>
              {content.hasRemoteContent && !loadRemote && (
                <div className="banner">
                  {t('mailDetail.remoteBlocked')}{' '}
                  <button className="link" onClick={() => setLoadRemote(true)}>
                    {t('mailDetail.loadRemote')}
                  </button>
                </div>
              )}
              {/* Only in the dark theme: in the light one there is nothing
                  to fix, and the control would be a question nobody asked.
                  A plain message is already dark from the frame's ground;
                  this is for one that paints its own, which the inversion
                  darkens while keeping its pictures the right way round. */}
              {dark && (
                <div className="frame-mode">
                  <div className="segmented" role="group" aria-label={t('mailDetail.frameMode')}>
                    <button
                      type="button"
                      className={darkened ? '' : 'active'}
                      aria-pressed={!darkened}
                      onClick={() => chooseDarkened(false)}
                    >
                      {t('mailDetail.asSent')}
                    </button>
                    <button
                      type="button"
                      className={darkened && !alreadyDark ? 'active' : ''}
                      aria-pressed={darkened && !alreadyDark}
                      disabled={alreadyDark}
                      title={alreadyDark ? t('mailDetail.alreadyDark') : undefined}
                      onClick={() => chooseDarkened(true)}
                    >
                      {t('mailDetail.darkened')}
                    </button>
                  </div>
                </div>
              )}
              {/* Rendered in a sandbox that permits no scripts, on top of
                  the server-side sanitising and a policy of default-src
                  'none' inside the frame. It is mail from a stranger. */}
              <MessageFrame
                document={document}
                title={t('mailDetail.message')}
                darkened={dark && darkened}
                onGroundMeasured={setAlreadyDark}
              />
            </>
          )}

          {tab === 'text' && <pre className="message-text">{content.text}</pre>}

          {/* The sanitised markup, not the original: it is what the frame
              above is rendering, which is the thing being explained. The
              untouched original is in the .eml behind Download. */}
          {tab === 'html' && <HighlightedHtml source={content.html ?? ''} />}

          {tab === 'raw' && <pre className="message-text">{content.rawHeaders}</pre>}

          {tab === 'source' && (
            <table>
              <tbody>
                {(content.headers ?? []).map((header, index) => (
                  <tr key={index}>
                    <td className="shrink muted">{header.key}</td>
                    <td className="mono wrap">{header.value}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}

          {content.attachments?.length ? (
            <div className="card" style={{ marginTop: 16 }}>
              <h3>{t('mailDetail.attachments')}</h3>
              <table>
                <tbody>
                  {content.attachments.map((attachment, index) => (
                    <tr key={index}>
                      <td>
                        {/* A real link, so saving it works the way saving
                            anything else does. */}
                        <a
                          href={`/api/v1/mail/${mail.id}/attachment/${attachment.index}`}
                          download={attachment.filename}
                        >
                          {attachment.filename}
                        </a>
                      </td>
                      <td className="shrink muted">{attachment.contentType}</td>
                      <td className="shrink muted">{formatBytes(attachment.size)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </>
      )}
    </>
  )
}

// --- opens -----------------------------------------------------------------
//
// Whether a picture this server put in the message has been fetched.
//
// Shown only for a message that carries one. For everything else the honest
// answer is not "not opened" but that there is nothing to go on, and a card
// saying "Not fetched" beside a plain-text message would be read as the
// former.
//
// The caveat is not fine print to be trimmed later. An operator who reads
// "Opened" as "read" will draw conclusions about a recipient that the data
// does not support, in both directions, and the sentence underneath is the
// only thing standing between the number and that mistake.
function Opens({ opens }: { opens?: MailOpens }) {
  const { t, plural } = useTranslation()
  if (!opens?.trackable) {
    return null
  }

  return (
    <div className="card">
      <h3>{t('mailDetail.opens')}</h3>
      {/* The tag says which of the two states this is, in one word, the same
          word the list uses. Everything that needs a number or a time is
          beside it rather than inside it: a tag that reads as a sentence
          stops working as a tag. */}
      <div className="verdict" style={{ marginBottom: 8 }}>
        <Tag value={opens.opened ? t('mail.opened') : t('mailDetail.notOpened')} tone={opens.opened ? 'good' : undefined} />
        {opens.opened && (
          <span className="muted">
            {[
              t('mailDetail.openedAt', { time: formatTime(opens.openedAt) }),
              opens.lastOpenedAt && opens.lastOpenedAt !== opens.openedAt
                ? t('mailDetail.openedAgain', { time: formatTime(opens.lastOpenedAt) })
                : '',
              plural(opens.openCount ?? 0, { one: 'mailDetail.openedCountOne', other: 'mailDetail.openedCount' }),
              opens.ip ? t('mailDetail.opensFetchedBy', { ip: opens.ip }) : '',
            ]
              .filter(Boolean)
              .join(' · ')}
          </span>
        )}
      </div>
      <p className="muted" style={{ margin: 0 }}>
        {t('mailDetail.opensCaveat')}
      </p>
    </div>
  )
}

// A row of the summary table, skipped when there is nothing to put in it. A
// label with an em dash beside it is a row that costs a line and says nothing.
function Field({ label, mono, children }: { label: string; mono?: boolean; children?: React.ReactNode }) {
  if (children === undefined || children === null || children === '' || children === false) {
    return null
  }
  return (
    <tr>
      <td className="shrink muted">{label}</td>
      <td className={mono ? 'mono wrap' : 'wrap'}>{children}</td>
    </tr>
  )
}

// --- authentication --------------------------------------------------------
//
// Every check as a row: the mechanism, its verdict, and what it examined.
// The previous version was a line of tags reading "SPF pass DKIM pass DMARC
// pass", which is enough to know nothing went wrong and never enough to work
// out why something did.
function Authentication({ results }: { results: AuthenticationResults }) {
  const { t } = useTranslation()
  const checks: Check[] = []

  if (results.spf) {
    checks.push({
      label: 'SPF',
      verdict: results.spf.result || t('common.none'),
      tone: toneFor(results.spf.result),
      // SPF is a question about one pair: may this address send for this
      // domain. Saying which pair is most of the answer.
      detail: t('mailDetail.spfDetail', {
        domain: results.spf.domain ?? '—',
        ip: results.spf.ip ?? '—',
      }),
    })
  }

  for (const dkim of results.dkims ?? []) {
    checks.push({
      label: 'DKIM',
      verdict: dkim.result || t('common.none'),
      tone: toneFor(dkim.result),
      // The name of the record that was looked up: paste it into dig and you
      // are looking at the key that either verified or did not.
      detail: (
        <>
          <span className="mono">
            {dkim.selector && dkim.domain ? `${dkim.selector}._domainkey.${dkim.domain}` : dkim.domain || '—'}
          </span>
          {dkim.identifier ? ` · ${t('mailDetail.dkimIdentifier', { identifier: dkim.identifier })}` : ''}
        </>
      ),
    })
  }

  if (results.dmarc) {
    const alignment = [
      results.dmarc.dkimAlignment ? t('mailDetail.alignmentDkim', { mode: alignmentMode(t, results.dmarc.dkimAlignment) }) : '',
      results.dmarc.spfAlignment ? t('mailDetail.alignmentSpf', { mode: alignmentMode(t, results.dmarc.spfAlignment) }) : '',
    ]
      .filter(Boolean)
      .join(', ')
    checks.push({
      label: 'DMARC',
      verdict: results.dmarc.result || t('common.none'),
      tone: toneFor(results.dmarc.result),
      // The policy is what a receiver was told to do with a failure, and the
      // alignment modes are what "aligned" meant for this domain. Both were
      // in the record and neither was on the page.
      detail: [
        results.dmarc.domain ?? '',
        results.dmarc.policy ? `p=${results.dmarc.policy}` : '',
        results.dmarc.subdomainPolicy ? `sp=${results.dmarc.subdomainPolicy}` : '',
        alignment,
      ]
        .filter(Boolean)
        .join(' · '),
    })
  }

  if (results.arc) {
    checks.push({
      label: 'ARC',
      verdict: results.arc.result || t('common.none'),
      tone: toneFor(results.arc.result),
      detail: results.arc.instances
        ? t('mailDetail.arcInstances', { count: results.arc.instances })
        : t('mailDetail.arcNone'),
    })
  }

  if (results.spamFilter) {
    checks.push({
      label: t('mailDetail.spam'),
      verdict: formatScore(results.spamFilter.score),
      tone: results.spamFilter.result === 'fail' ? 'bad' : 'good',
      // The breakdown is the whole explanation of the score: which check
      // fired and what it cost. An external daemon reports only names, so
      // fall back to those when that is all there is.
      detail: results.spamFilter.checks?.length ? (
        <span className="spam-checks">
          {results.spamFilter.checks.map((check) => (
            <span key={check.symbol} className="spam-check" title={check.description ?? ''}>
              <span className="mono">{check.symbol}</span>
              <span className={check.score < 0 ? 'spam-check-good' : 'spam-check-bad'}>
                {check.score > 0 ? `+${formatScore(check.score)}` : formatScore(check.score)}
              </span>
            </span>
          ))}
        </span>
      ) : results.spamFilter.symbols?.length ? (
        <span className="mono wrap">{results.spamFilter.symbols.join(' ')}</span>
      ) : (
        t('mailDetail.noSymbols')
      ),
    })
  }

  if (results.antivirus) {
    const viruses = results.antivirus.viruses ?? []
    checks.push({
      label: t('mailDetail.virus'),
      verdict: viruses.length ? t('mailDetail.infected') : t('mailDetail.clean'),
      tone: viruses.length ? 'bad' : 'good',
      detail: viruses.join(', '),
    })
  }

  if (results.contentFilter?.unsafeExtensions?.length) {
    checks.push({
      label: t('mailDetail.attachmentsCheck'),
      verdict: t('mailDetail.unsafe'),
      tone: 'bad',
      detail: results.contentFilter.unsafeExtensions.join(', '),
    })
  }

  const senderMx = results.senderMx?.mailServers?.length ? results.senderMx : undefined
  const fromMx = results.fromMx?.mailServers?.length ? results.fromMx : undefined

  return (
    <div className="card">
      <h3>{t('mailDetail.authentication')}</h3>

      {checks.length === 0 ? (
        <p className="muted" style={{ margin: 0 }}>
          {t('mailDetail.noChecks')}
        </p>
      ) : (
        <table className="checks">
          <tbody>
            {checks.map((check, index) => (
              <tr key={index}>
                <td className="shrink">{check.label}</td>
                <td className="shrink">
                  <Tag value={check.verdict} tone={check.tone} />
                </td>
                <td className="muted wrap">{check.detail}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {/* Where the domains this message claims actually receive their mail.
          It is the quickest way to see that a sender claiming to be a large
          provider is not one. */}
      {(senderMx || fromMx) && (
        <table className="detail" style={{ marginTop: 12 }}>
          <tbody>
            {senderMx && (
              <Field label={t('mailDetail.senderMx', { domain: senderMx.domain ?? '' })} mono>
                {(senderMx.mailServers ?? []).join(', ')}
              </Field>
            )}
            {fromMx && fromMx.domain !== senderMx?.domain && (
              <Field label={t('mailDetail.fromMx', { domain: fromMx.domain ?? '' })} mono>
                {(fromMx.mailServers ?? []).join(', ')}
              </Field>
            )}
          </tbody>
        </table>
      )}

      {results.errors?.length ? (
        <p className="error" style={{ marginBottom: 0 }}>
          {results.errors.join('; ')}
        </p>
      ) : null}
    </div>
  )
}

// --- deliveries ------------------------------------------------------------
//
// One block per recipient rather than one row: a failed delivery carries a
// remote MTA, an enhanced status code and a diagnostic string from the far
// end, and those are the three things somebody debugging a bounce wants. None
// of them fitted in a five-column table, so none of them were shown.
function deliveryMethodKey(method: NonNullable<Delivery['method']>): Key {
  switch (method) {
    case 'email':
      return 'mailDetail.methodEmail'
    case 'mailServer':
      return 'mailDetail.methodMailServer'
    case 'webhook':
      return 'mailDetail.methodWebhook'
    default:
      return 'mailDetail.methodSmtp'
  }
}

function DeliveryDetail({ delivery }: { delivery: Delivery }) {
  const { t } = useTranslation()
  const label = useEnumLabel()
  const statuses = (delivery.deliveryStatuses ?? []).flatMap((status) =>
    (status.recipientStatuses ?? []).map((recipient) => ({ ...recipient, reportingMta: status.reportingMta })),
  )

  return (
    <div className="delivery">
      <div className="delivery-head">
        <Tag value={label.status(delivery.status) || '—'} tone={toneFor(delivery.status)} />
        <span className="delivery-recipient">{delivery.recipient}</span>
        <KindTag value={delivery.kind} />
      </div>
      {/* How it is handed on, and where. The recipient above is who the
          message was for; this is what was done about it — a forward to an
          address by looking up its mail servers, a relay to a configured
          host, a POST to a URL — which is the thing to check when a
          delivery is stuck. */}
      {delivery.method && delivery.destination ? (
        <p className="muted delivery-method">
          {t(deliveryMethodKey(delivery.method), { destination: delivery.destination })}
        </p>
      ) : null}

      <table className="detail">
        <tbody>
          <Field label={t('mailDetail.attempts')}>
            {delivery.attempts
              ? t('mailDetail.attemptsAt', {
                  count: delivery.attempts,
                  time: formatTime(delivery.attemptedAt),
                })
              : t('mailDetail.notAttempted')}
          </Field>
          <Field label={t('mailDetail.deliveredAt')}>
            {delivery.deliveredAt && formatTime(delivery.deliveredAt)}
          </Field>
          <Field label={t('mailDetail.droppedAt')}>
            {delivery.droppedAt && formatTime(delivery.droppedAt)}
          </Field>
          <Field label={t('mailDetail.retryAtLabel')}>
            {delivery.retryAt && formatTime(delivery.retryAt)}
          </Field>
          {/* When the sender was told this failed. A bounce that was never
              sent is a different problem from one that was. */}
          <Field label={t('mailDetail.notifiedAt')}>
            {delivery.notifiedAt && formatTime(delivery.notifiedAt)}
          </Field>
          <Field label={t('mail.size')}>{delivery.size ? formatBytes(delivery.size) : undefined}</Field>
          <Field label={t('mailDetail.lastError')}>{delivery.error}</Field>
        </tbody>
      </table>

      {statuses.length > 0 && (
        <table className="detail">
          <tbody>
            {statuses.map((status, index) => (
              <Field
                key={index}
                label={status.status ? `${t('mailDetail.remoteSaid')} ${status.status}` : t('mailDetail.remoteSaid')}
              >
                <>
                  {status.remoteMta && <span className="mono">{status.remoteMta}</span>}
                  {status.action && <> · {status.action}</>}
                  {status.diagnosticCode && (
                    <div className="mono wrap">{status.diagnosticCode}</div>
                  )}
                </>
              </Field>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

// --- wording ---------------------------------------------------------------


// One sentence saying what became of the message, because "rejected" alone
// invites the next question and the answer is already on the page.
function verdictLine(
  t: (key: Key, values?: Record<string, string | number>) => string,
  plural: (count: number, forms: { one: Key; other: Key }, values?: Record<string, string | number>) => string,
  mail: Mail,
  deliveries: Delivery[],
): string {
  if (mail.status === 'rejected') {
    const results = mail.authenticationResults
    if (results?.antivirus?.viruses?.length) {
      return t('mailDetail.whyVirus', { viruses: results.antivirus.viruses.join(', ') })
    }
    if (results?.spamFilter?.result === 'fail') {
      return t('mailDetail.whySpam', { score: formatScore(results.spamFilter.score) })
    }
    if (results?.dmarc?.result && toneFor(results.dmarc.result) === 'bad') {
      return t('mailDetail.whyDmarc', { policy: results.dmarc.policy ?? 'none' })
    }
    if (results?.errors?.length) {
      return results.errors.join('; ')
    }
    return t('mailDetail.whyRejected')
  }

  if (deliveries.length === 0) {
    return t('mailDetail.whyNoDeliveries')
  }

  const delivered = deliveries.filter((delivery) => delivery.status === 'delivered').length
  const failed = deliveries.filter((delivery) => delivery.status === 'failed' || delivery.status === 'dropped').length
  if (failed > 0) {
    return t('mailDetail.whyPartly', { delivered, failed, total: deliveries.length })
  }
  if (delivered === deliveries.length) {
    return plural(delivered, { one: 'mailDetail.whyDeliveredOne', other: 'mailDetail.whyDelivered' })
  }
  return plural(deliveries.length - delivered, {
    one: 'mailDetail.whyPendingOne',
    other: 'mailDetail.whyPending',
  })
}

// DMARC alignment is published as one letter. Nobody reads "adkim=r" and
// thinks "relaxed", and the difference decides whether a subdomain counts.
function alignmentMode(t: (key: Key) => string, mode: string): string {
  return mode === 's' ? t('mailDetail.alignmentStrict') : t('mailDetail.alignmentRelaxed')
}

// buildDocument wraps the sanitised HTML in a complete document for the frame.
//
// The content security policy is the second layer: even if something got past
// the server-side sanitiser, it cannot execute or call home from here. When
// remote images are not being loaded, img-src is restricted to data URLs so a
// tracking pixel cannot fire.
const DARKENED_KEY = 'teanode.mail.darkened'

// buildDocument writes the whole document the frame shows, colours included.
//
// The frame cannot read the dashboard's tokens — it is a separate document
// built from a string — so the two colours are written here as literals,
// taken from the dark palette at the top of style.css so they match rather
// than approximate. In the dark theme the ground is dark and the text light,
// which a plain message inherits; a message that sets its own colours keeps
// them, since a default is exactly what it overrides.
//
// "darkened" is the reader's choice to invert the message. The inversion
// itself is applied to the frame element by MessageFrame; what has to be
// written in here is the other half — pictures inverted back so they come
// out the right way round, and a light ground so a message with none
// inverts to dark rather than to nothing. Imperfect on gradients, which is
// why it is a choice.
function buildDocument(html: string, mailId?: string, dark = false, darkened = false): string {
  // Under inversion the document is built light, not dark. The frame element
  // is what gets inverted, and it inverts everything in it — a dark ground
  // built in here came out as a thick white border around the inverted
  // message. Light in, dark out.
  const darkGround = dark && !darkened
  // 'self' covers every image that can appear in here: the ones the message
  // carried with it, which the server rewrote from cid: to its own attachment
  // endpoint, and the remote ones, which go through the server too once the
  // reader asks for them. Nothing in a message reaches the network from the
  // reader's browser, so this never has to widen to https:.
  const policy = [
    "default-src 'none'",
    "img-src data: 'self'",
    "style-src 'unsafe-inline'",
    'font-src data:',
  ].join('; ')

  const body = mailId ? restoreRemoteImages(html, mailId) : html

  return `<!DOCTYPE html><html><head><meta charset="utf-8">
<meta http-equiv="Content-Security-Policy" content="${policy}">
<style>#teanode-content{overflow:hidden}body{margin:0;padding:14px;font:15px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;color:${
    darkGround ? '#f4f4f5' : '#16161a'
  };background:${darkGround ? '#1a1a1d' : '#fff'};word-wrap:break-word${
    darkGround ? ';color-scheme:dark' : ''
  }}img{max-width:100%;height:auto}table{max-width:100%}${
    darkened
      ? '.darkened img,.darkened video,.darkened [style*="background-image"]{filter:invert(1) hue-rotate(180deg)}'
      : ''
  }</style>
</head><body><div id="teanode-content"${darkened ? ' class="darkened"' : ''}>${body}</div></body></html>`
}

// restoreRemoteImages points the blocked images at this server, once the
// reader has asked for them.
//
// Through the server rather than straight out of the browser. Fetching them
// here hands the sender the reader's address, the reader's user agent and the
// exact moment the message was opened — which is what a tracking pixel is
// for. The proxy tells them only that the mail server looked.
function restoreRemoteImages(html: string, mailId: string): string {
  return html.replace(
    /data-blocked-src="([^"]*)"\s+src="[^"]*"/g,
    (_match, source: string) =>
      `src="/api/v1/mail/${encodeURIComponent(mailId)}/remote?url=${encodeURIComponent(decodeEntities(source))}"`,
  )
}

// The source came out of an HTML attribute the server wrote, so an ampersand
// in a query string arrives as &amp;. Putting that through the proxy verbatim
// would fetch a different address than the message named.
function decodeEntities(value: string): string {
  return value
    .replace(/&amp;/g, '&')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
}

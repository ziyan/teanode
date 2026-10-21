import { useMemo } from 'react'

import { useTranslation } from '../../i18n/i18n'
import { siteFromText } from './passwordsEditor'
import { siteHost } from './passwordsList'
import { OneTimeCodeValue, PlainValue, SecretValue } from './passwordsParts'
import { PasswordItemDraft } from './passwordsSession'

// isWebAddress says whether a site may be a link: only http and https, so a
// saved "javascript:" address is shown as text and never run.
function isWebAddress(site: string): boolean {
  try {
    const protocol = new URL(site).protocol
    return protocol === 'https:' || protocol === 'http:'
  } catch {
    return false
  }
}

// The most sites one login lists. A login can carry thousands, and a page of
// them is no use to read; the rest are counted.
const MOST_SITES_SHOWN = 50

// sitesToShow are the sites as they are listed: a bare "example.com" becomes
// an https address, as the editor keeps it, and a site written twice is shown
// once, so each is a link and the list's keys are different. At most
// MOST_SITES_SHOWN are returned, with how many more there are.
export function sitesToShow(sites: string[]): { shown: string[]; hiddenCount: number } {
  const seen = new Set<string>()
  const shown: string[] = []
  for (const site of sites) {
    if (site.trim() === '') continue
    // Text that does not make a web address even with https in front stays as
    // it was saved.
    const normalized = siteFromText(site)
    const address = isWebAddress(normalized) ? normalized : site.trim()
    if (seen.has(address)) continue
    seen.add(address)
    if (shown.length < MOST_SITES_SHOWN) shown.push(address)
  }
  return { shown, hiddenCount: seen.size - shown.length }
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="passwords-field">
      <dt className="muted">{label}</dt>
      <dd>{children}</dd>
    </div>
  )
}

// PasswordsFieldList draws the fields of an item of any kind, hidden until
// shown, with a copy button on each. The item on the screen and an earlier
// version of it are drawn the same way, so a version reads like the item did.
export function PasswordsFieldList({ view }: { view: PasswordItemDraft }) {
  const { t, language } = useTranslation()
  const sites = useMemo(
    () => (view.itemKind === 'login' ? sitesToShow(view.fields.sites) : { shown: [], hiddenCount: 0 }),
    [view],
  )
  return (
    <dl className="passwords-fields">
      {view.itemKind === 'login' && (
        <>
          {view.fields.userName && (
            <Row label={t('passwords.fieldUserName')}>
              <PlainValue label={t('passwords.fieldUserName')} value={view.fields.userName} />
            </Row>
          )}
          {view.fields.password && (
            <Row label={t('passwords.fieldPassword')}>
              <SecretValue label={t('passwords.fieldPassword')} value={view.fields.password} />
            </Row>
          )}
          {view.fields.oneTimeCodeUri && (
            <Row label={t('passwords.fieldOneTimeCode')}>
              <OneTimeCodeValue uri={view.fields.oneTimeCodeUri} />
            </Row>
          )}
          {sites.shown.length > 0 && (
            <Row label={t('passwords.fieldSites')}>
              <ul className="passwords-sites">
                {sites.shown.map((site) => (
                  <li key={site}>
                    {isWebAddress(site) ? (
                      <a href={site} target="_blank" rel="noopener noreferrer">
                        {siteHost(site)}
                      </a>
                    ) : (
                      site
                    )}
                  </li>
                ))}
                {sites.hiddenCount > 0 && (
                  <li className="muted">{t('passwords.sitesMore', { count: sites.hiddenCount })}</li>
                )}
              </ul>
            </Row>
          )}
          {view.fields.passkeys.length > 0 && (
            <Row label={t('passwords.fieldPasskeys')}>
              <ul className="passwords-passkeys">
                {view.fields.passkeys.map((passkey) => (
                  <li key={passkey.credentialId}>
                    <strong>{passkey.relyingPartyId}</strong>
                    {passkey.userName && <span className="muted"> · {passkey.userName}</span>}
                    <span className="muted">
                      {' · '}
                      {t('passwords.passkeyCreated', {
                        date: new Date(passkey.createdAt).toLocaleDateString(language),
                      })}
                    </span>
                  </li>
                ))}
              </ul>
            </Row>
          )}
        </>
      )}
      {view.itemKind === 'card' && (
        <>
          {view.fields.cardholderName && (
            <Row label={t('passwords.fieldCardholderName')}>
              <PlainValue label={t('passwords.fieldCardholderName')} value={view.fields.cardholderName} />
            </Row>
          )}
          {view.fields.number && (
            <Row label={t('passwords.fieldCardNumber')}>
              <SecretValue label={t('passwords.fieldCardNumber')} value={view.fields.number} />
            </Row>
          )}
          {(view.fields.expiryMonth || view.fields.expiryYear) && (
            <Row label={t('passwords.fieldExpiry')}>
              <PlainValue
                label={t('passwords.fieldExpiry')}
                value={[view.fields.expiryMonth, view.fields.expiryYear].filter(Boolean).join('/')}
              />
            </Row>
          )}
          {view.fields.securityCode && (
            <Row label={t('passwords.fieldSecurityCode')}>
              <SecretValue label={t('passwords.fieldSecurityCode')} value={view.fields.securityCode} />
            </Row>
          )}
          {view.fields.brand && (
            <Row label={t('passwords.fieldBrand')}>
              <PlainValue label={t('passwords.fieldBrand')} value={view.fields.brand} />
            </Row>
          )}
        </>
      )}
      {view.itemKind === 'note' && view.fields.text && (
        <Row label={t('passwords.fieldNoteText')}>
          <SecretValue label={t('passwords.fieldNoteText')} value={view.fields.text} isMultiline />
        </Row>
      )}
      {view.itemKind !== 'note' && view.fields.notes && (
        <Row label={t('passwords.fieldNotes')}>
          <SecretValue label={t('passwords.fieldNotes')} value={view.fields.notes} isMultiline />
        </Row>
      )}
      {view.fields.folder && <Row label={t('passwords.fieldFolder')}>{view.fields.folder}</Row>}
    </dl>
  )
}

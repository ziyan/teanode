import { Trans, useTranslation } from '../i18n/i18n'

// Where the deployment guide explains how to put the server on an https
// address; the section is written for the same people who see this card.
const HTTPS_GUIDE_URL =
  'https://github.com/ziyan/teanode/blob/main/docs/reference/deployment.md#photos-and-the-iphone-app'

// canPhoneUseAddress says whether an iPhone away from this computer could
// sign in at the address, which the TeaNode app only does over https. A page
// loaded over plain http is not one, and neither is a page that is only
// reachable from the computer or the network it is on: localhost, a private
// IPv4 address, a link-local or unique-local IPv6 address, or a name with no
// dot or a .local ending, which only a local network resolves.
export function canPhoneUseAddress(address: { protocol: string; hostname: string }): boolean {
  if (address.protocol !== 'https:') return false
  const hostname = address.hostname.toLowerCase().replace(/^\[|\]$/g, '')
  if (hostname === '' || hostname === 'localhost' || hostname.endsWith('.localhost') || hostname.endsWith('.local')) {
    return false
  }
  if (!hostname.includes('.') && !hostname.includes(':')) return false
  const octets = hostname.split('.').map(Number)
  if (octets.length === 4 && octets.every((octet) => Number.isInteger(octet) && octet >= 0 && octet <= 255)) {
    const [first, second] = octets
    return !(
      first === 10 ||
      first === 127 ||
      first === 0 ||
      (first === 169 && second === 254) ||
      (first === 172 && second >= 16 && second <= 31) ||
      (first === 192 && second === 168)
    )
  }
  if (hostname.includes(':')) {
    return !(hostname === '::1' || hostname === '::' || /^f[cd]/.test(hostname) || /^fe[89ab]/.test(hostname))
  }
  return true
}

// PhotoBackupGuide says how to back an iPhone up to this server: what to
// install, the address to sign in with (this page's own, which is what the
// phone has to reach), and where the switch is. When that address is one a
// phone cannot use, it says so plainly, since the app would otherwise fail to
// connect with nothing on either side saying why.
export function PhotoBackupGuide({ title }: { title?: string }) {
  const { t } = useTranslation()
  const address = window.location.origin
  return (
    <section className="card photo-backup">
      <h3>{title ?? t('photoBackup.title')}</h3>
      <ol className="photo-backup-steps">
        <li>{t('photoBackup.install')}</li>
        <li>
          <Trans k="photoBackup.signIn" nodes={{ address: <code>{address}</code> }} />
        </li>
        <li>{t('photoBackup.turnOn')}</li>
      </ol>
      {!canPhoneUseAddress(window.location) && (
        <p className="notice warn" role="note">
          <Trans
            k="photoBackup.needsHttps"
            values={{ address }}
            nodes={{
              guide: (
                <a href={HTTPS_GUIDE_URL} target="_blank" rel="noreferrer">
                  {t('photoBackup.httpsGuide')}
                </a>
              ),
            }}
          />
        </p>
      )}
    </section>
  )
}

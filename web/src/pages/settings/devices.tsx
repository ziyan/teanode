import { useTranslation } from '../../i18n/i18n'
import { SyncPage } from '../sync'
import { PhonesPage } from './phones'

// DevicesPage is the one place for connecting a device. Two sections that
// were pages of their own, because a person looking for "my phone" could not
// tell which of them held it: the TeaNode app (the phones it is connected
// on) and the built-in apps (mail, calendars and contacts on an iPhone or
// Mac). The sections are the old pages as they were, so the setup dialog
// still opens from ?setup=1&mailboxId=.
export function DevicesPage() {
  const { t } = useTranslation()
  return (
    <>
      <section className="devices-group" aria-labelledby="devices-app-title">
        <h2 id="devices-app-title">{t('devices.appTitle')}</h2>
        <PhonesPage />
      </section>
      <section className="devices-group" aria-labelledby="devices-built-in-title">
        <h2 id="devices-built-in-title">{t('devices.builtInTitle')}</h2>
        <SyncPage />
      </section>
    </>
  )
}

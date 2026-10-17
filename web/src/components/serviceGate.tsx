import type { ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { graphql, type ServiceCapabilities } from '../api'
import { hasPermission, useSession } from '../session'
import { homePath, isPathAvailable, serviceForPath, type HostedServiceKey } from '../services'
import { useTranslation } from '../i18n/i18n'
import type { Key } from '../i18n/i18n'
import { Loading } from './common'
import { useRestartNow } from './restartNow'
import { useQuery } from './useQuery'

// What an operator is told a service is called, by the key the server uses.
const SERVICE_NAMES: Record<HostedServiceKey, Key> = {
  isMailRoutingEnabled: 'services.mail',
  isMailboxEnabled: 'nav.mail',
  isCalendarEnabled: 'nav.calendar',
  isRemindersEnabled: 'services.remindersLabel',
  isContactsEnabled: 'nav.contacts',
  isPhotosEnabled: 'nav.photos',
}

const CHOSEN = `{ GetServiceSetup { selectedServices { isMailRoutingEnabled isMailboxEnabled isCalendarEnabled isRemindersEnabled isContactsEnabled isPhotosEnabled } } }`

export function ServiceGate({ children }: { children: ReactNode }) {
  const session = useSession()
  const location = useLocation()
  if (isPathAvailable(location.pathname, session.services)) return children
  const serviceKey = serviceForPath(location.pathname)
  // An operator is told more than "off": whether it is chosen and only waits
  // for the restart that starts it, and where to choose it.
  const isOperator = hasPermission(session.permissions, 'server:manage')
  if (isOperator && serviceKey && session.services?.hasPendingServiceRestart) {
    return <ServiceWaitingForRestart serviceKey={serviceKey} />
  }
  return <ServiceOff isOperator={isOperator} />
}

function ServiceOff({ isOperator }: { isOperator: boolean }) {
  const session = useSession()
  const { t } = useTranslation()
  return (
    <div className="card">
      <h2>{t('services.unavailable')}</h2>
      <p>{t('services.unavailableDetail')}</p>
      <p>
        <Link to={homePath(session)}>{t('services.back')}</Link>
        {isOperator && (
          <>
            {' · '}
            <Link to="/manage/server/setup">{t('services.openSetup')}</Link>
          </>
        )}
      </p>
    </div>
  )
}

// ServiceWaitingForRestart is what an operator sees at the page of a service
// they chose and the server has not started yet: that it is chosen, that a
// restart starts it, and the button for the restart. A service that is not
// chosen gets the plain "off" page, with the way to choose it.
function ServiceWaitingForRestart({ serviceKey }: { serviceKey: HostedServiceKey }) {
  const { t } = useTranslation()
  const chosen = useQuery(
    () =>
      graphql<{ GetServiceSetup: { selectedServices: ServiceCapabilities } }>(CHOSEN).then(
        (answer) => answer.GetServiceSetup.selectedServices[serviceKey] === true,
      ),
    [serviceKey],
  )
  const { isRestarting, restart } = useRestartNow()
  if (chosen.data === null) return chosen.error ? <ServiceOff isOperator /> : <Loading />
  if (!chosen.data) return <ServiceOff isOperator />
  return (
    <div className="card waiting-restart">
      <h2>{t('services.waitingTitle', { service: t(SERVICE_NAMES[serviceKey]) })}</h2>
      <p>{t('services.waitingDetail')}</p>
      <div className="page-actions">
        <button type="button" className="primary" disabled={isRestarting} onClick={() => void restart()}>
          {isRestarting ? t('server.restarting') : t('services.restartNow')}
        </button>
        <Link className="button" to="/manage/server/setup">
          {t('services.openSetup')}
        </Link>
      </div>
    </div>
  )
}

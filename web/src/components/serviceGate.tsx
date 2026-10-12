import type { ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { useSession } from '../session'
import { homePath, isPathAvailable } from '../services'
import { useTranslation } from '../i18n/i18n'

export function ServiceGate({ children }: { children: ReactNode }) {
  const session = useSession()
  const location = useLocation()
  const { t } = useTranslation()
  if (isPathAvailable(location.pathname, session.services)) return children
  return (
    <div className="card">
      <h2>{t('services.unavailable')}</h2>
      <p>{t('services.unavailableDetail')}</p>
      <Link to={homePath(session)}>{t('services.back')}</Link>
    </div>
  )
}

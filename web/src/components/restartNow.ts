import { useState } from 'react'
import { useTranslation } from '../i18n/i18n'
import { restartServer } from './serverRestart'
import { useToast } from './toast'

// useRestartNow is the "Restart now" a page offers beside what waits for one:
// it restarts the server, waits for the new process, says whether it came
// back, and tells the rest of the dashboard to ask the server again what is
// running, since that is what has just changed. onRestarted is for the page's
// own reloading.
export function useRestartNow(onRestarted?: () => Promise<unknown> | void) {
  const { t } = useTranslation()
  const toast = useToast()
  const [isRestarting, setIsRestarting] = useState(false)
  async function restart() {
    setIsRestarting(true)
    try {
      if (await restartServer()) {
        toast.done(t('server.cameBack'))
        await onRestarted?.()
        window.dispatchEvent(new Event('focus'))
      } else {
        toast.failed(t('server.didNotComeBack'))
      }
    } catch (caught) {
      toast.failed(caught instanceof Error ? caught.message : String(caught))
    } finally {
      setIsRestarting(false)
    }
  }
  return { isRestarting, restart }
}

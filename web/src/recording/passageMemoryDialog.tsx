import { useNavigate } from 'react-router-dom'

import { ErrorMessage, Loading } from '../components/common'
import { FormDialog } from '../components/dialog'
import { useTranslation } from '../i18n/i18n'
import { MemoryPageLinks, memoryPathOf, pagesOf, useRecordMemoryLinks } from '../memoryLinks'

// The pages of the agent's memory linked to what was said in a bubble: the
// people who spoke in it, and whatever the agent filed from this stretch of
// the recording. Asked for when the bubble's action is chosen, not for every
// bubble of the transcript, which would be a question per bubble.
export function PassageMemoryDialog({
  recordingId,
  passageStartMS,
  passageEndMS,
  onClose,
}: {
  recordingId: string
  passageStartMS: number
  passageEndMS: number
  onClose: () => void
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const memory = useRecordMemoryLinks('recording_passage', recordingId, passageStartMS, passageEndMS)
  const links = memory.data?.ReadRecordMemoryLinks ?? []
  const pages = pagesOf(links)
  return (
    <FormDialog
      title={t('memoryLinks.inMemory')}
      submitLabel={t('memoryLinks.openPage')}
      canSubmit={pages.length > 0}
      closeLabel={t('common.close')}
      onClose={onClose}
      onSubmit={() => {
        if (pages.length > 0) navigate(memoryPathOf(pages[0].nodePath))
      }}
    >
      <p className="muted">{t('memoryLinks.passageHint')}</p>
      <ErrorMessage error={memory.error} />
      {memory.loading && !memory.data ? <Loading /> : null}
      {memory.data && links.length === 0 ? <p className="muted">{t('memoryLinks.passageEmpty')}</p> : null}
      {links.length > 0 ? <MemoryPageLinks links={links} /> : null}
    </FormDialog>
  )
}

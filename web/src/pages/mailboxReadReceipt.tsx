import { graphql } from '../api'
import { useToast } from '../components/toast'
import { useTranslation } from '../i18n/i18n'
import { READ_RECEIPT } from './finance/financeApi'

// ReadAsReceiptMenuItem is the message menu's "Read as receipt": it asks
// the agent to read the message as a receipt in the background, as teanode
// finance read-receipt --mailbox-item and the finance tool's read_receipt
// do. Offered on every message; the server says when it cannot be done
// (finance not offered, a mailbox the agent is not granted), and the
// toast carries what it said. Once read, the receipt is matched to its
// charge by the matcher, and shows on the Finance page's Receipts.
export function ReadAsReceiptMenuItem({ mailboxItemId, onChosen }: { mailboxItemId: string; onChosen: () => void }) {
  const { t } = useTranslation()
  const toast = useToast()
  const readAsReceipt = async () => {
    onChosen()
    try {
      await graphql(READ_RECEIPT, { mailboxItemId })
      toast.done(t('mailbox.readingAsReceipt'))
    } catch (caught) {
      toast.failure(caught, t('mailbox.readAsReceiptFailed'))
    }
  }
  return (
    <button type="button" role="menuitem" onClick={() => void readAsReceipt()}>
      {t('mailbox.readAsReceipt')}
    </button>
  )
}

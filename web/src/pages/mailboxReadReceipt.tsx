import { graphql } from '../api'
import { useToast } from '../components/toast'
import { useTranslation } from '../i18n/i18n'
import { READ_RECEIPT, READ_TAX_DOCUMENT, TaxDocumentReading } from './finance/financeApi'

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

// ReadAsTaxDocumentMenuItem is the message menu's "Read as paystub or W-2":
// it copies each PDF and picture the message carries into the agent's
// uploads and has each read as a paystub or a W-2 in the background, as
// teanode finance read-tax-document --mailbox-item and the finance tool's
// read_tax_document do. The toast says how many files are being read, or
// what the server said when it cannot (a message with no such file, a
// mailbox the agent is not granted). Once read, they show on the Finance
// page's Taxes.
export function ReadAsTaxDocumentMenuItem({
  mailboxItemId,
  onChosen,
}: {
  mailboxItemId: string
  onChosen: () => void
}) {
  const { t, plural, language } = useTranslation()
  const toast = useToast()
  const readAsTaxDocument = async () => {
    onChosen()
    try {
      const answer = await graphql<{ ReadTaxDocument?: TaxDocumentReading | null }>(READ_TAX_DOCUMENT, {
        mailboxItemId,
      })
      const count = answer?.ReadTaxDocument?.agentAttachmentIds.length ?? 0
      toast.done(
        plural(
          count,
          { one: 'mailbox.readingAsTaxDocumentOne', other: 'mailbox.readingAsTaxDocumentOther' },
          { count: count.toLocaleString(language) },
        ),
      )
    } catch (caught) {
      toast.failure(caught, t('mailbox.readAsTaxDocumentFailed'))
    }
  }
  return (
    <button type="button" role="menuitem" onClick={() => void readAsTaxDocument()}>
      {t('mailbox.readAsTaxDocument')}
    </button>
  )
}

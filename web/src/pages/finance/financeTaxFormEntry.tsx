import { useState } from 'react'

import { graphql } from '../../api'
import { FormDialog } from '../../components/dialog'
import { CloseIcon } from '../../components/icons'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useTranslation } from '../../i18n/i18n'
import { TAX_DOCUMENT_UPLOAD_PATH, TaxFormFinding, taxFormKindName } from './financeApi'
import { RECORD_TAX_FORM, TAX_FORM_KINDS, TaxFormEntryInput } from './financeEntryApi'
import { SourceFile, SourceFileField, blankToUndefined, isOptionalDecimal } from './financeEntryCommon'

// The dialog that records a tax form by hand: the Add tax form button on
// the Taxes page opens it. It asks for the form as RecordTaxForm takes it
// (its kind and year, who paid and who received, and each box as printed:
// its number, what tells boxes of one number apart, and its amount, text
// or mark) and the PDF or picture it was typed from, which the form is kept
// against, marked as a tax document so its text is never kept.

type EntryDraft = {
  key: number
  boxCode: string
  boxQualifier: string
  amount: string
  textValue: string
  isChecked: boolean
}

function emptyEntry(key: number): EntryDraft {
  return { key, boxCode: '', boxQualifier: '', amount: '', textValue: '', isChecked: false }
}

function taxFormEntryOf(entry: EntryDraft): TaxFormEntryInput {
  return {
    boxCode: entry.boxCode.trim(),
    boxQualifier: blankToUndefined(entry.boxQualifier),
    amount: blankToUndefined(entry.amount),
    textValue: blankToUndefined(entry.textValue),
    isChecked: entry.isChecked ? true : undefined,
  }
}

export function TaxFormEntryDialog({
  taxYear,
  onRecorded,
  onClose,
}: {
  // The year the Taxes page is showing, which the form is most likely of.
  taxYear: number
  onRecorded: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [taxFormKind, setTaxFormKind] = useState('w2')
  const [typedTaxYear, setTypedTaxYear] = useState(String(taxYear))
  const [isCorrected, setIsCorrected] = useState(false)
  const [payerName, setPayerName] = useState('')
  const [payerTaxId, setPayerTaxId] = useState('')
  const [recipientName, setRecipientName] = useState('')
  const [recipientTaxIdMask, setRecipientTaxIdMask] = useState('')
  const [entries, setEntries] = useState<EntryDraft[]>([emptyEntry(1)])
  const [nextEntryKey, setNextEntryKey] = useState(2)
  const [sourceFile, setSourceFile] = useState<SourceFile | null>(null)
  const [isBusy, setIsBusy] = useState(false)

  const isValid =
    sourceFile !== null &&
    /^\d{4}$/.test(typedTaxYear.trim()) &&
    payerName.trim() !== '' &&
    recipientName.trim() !== '' &&
    (recipientTaxIdMask.trim() === '' || /^\d{4}$/.test(recipientTaxIdMask.trim())) &&
    entries.length > 0 &&
    entries.every((entry) => entry.boxCode.trim() !== '' && isOptionalDecimal(entry.amount))

  const changeEntry = (key: number, change: Partial<EntryDraft>) =>
    setEntries((previous) => previous.map((entry) => (entry.key === key ? { ...entry, ...change } : entry)))
  const addEntry = () => {
    setEntries((previous) => [...previous, emptyEntry(nextEntryKey)])
    setNextEntryKey(nextEntryKey + 1)
  }
  const removeEntry = (key: number) => setEntries((previous) => previous.filter((entry) => entry.key !== key))

  const submit = async () => {
    setIsBusy(true)
    try {
      const answer = await graphql<{ RecordTaxForm: { taxFormFindings: TaxFormFinding[] } }>(RECORD_TAX_FORM, {
        agentAttachmentId: sourceFile?.agentAttachmentId,
        taxFormKind,
        taxYear: Number(typedTaxYear.trim()),
        isCorrected,
        payerName: payerName.trim(),
        payerTaxId: blankToUndefined(payerTaxId),
        recipientName: recipientName.trim(),
        recipientTaxIdMask: blankToUndefined(recipientTaxIdMask),
        taxFormEntries: entries.map(taxFormEntryOf),
      })
      toast.done(
        [
          t('finance.entryTaxFormRecorded', {
            form: taxFormKindName(taxFormKind),
            year: typedTaxYear.trim(),
            payer: payerName.trim(),
          }),
          ...answer.RecordTaxForm.taxFormFindings
            .filter((finding) => finding.isAgreeing === false)
            .map((finding) => finding.findingText),
        ].join(' '),
      )
      onRecorded()
      onClose()
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <FormDialog
      wide
      title={t('finance.entryTaxFormTitle')}
      submitLabel={t('finance.entryRecordTaxForm')}
      busy={isBusy}
      canSubmit={isValid && !isBusy}
      onSubmit={() => void submit()}
      onClose={() => {
        if (!isBusy) onClose()
      }}
    >
      <p className="muted">{t('finance.entryTaxFormHint')}</p>
      <div className="finance-entry-fields">
        <label>
          <span>{t('finance.taxFormKind')}</span>
          <select value={taxFormKind} onChange={(event) => setTaxFormKind(event.target.value)}>
            {TAX_FORM_KINDS.map((kind) => (
              <option key={kind} value={kind}>
                {taxFormKindName(kind)}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>{t('finance.taxYear')}</span>
          <input
            inputMode="numeric"
            maxLength={4}
            value={typedTaxYear}
            onChange={(event) => setTypedTaxYear(event.target.value)}
          />
        </label>
        <label>
          <span>{t('finance.entryPayerName')}</span>
          <input value={payerName} onChange={(event) => setPayerName(event.target.value)} />
        </label>
        <label>
          <span>{t('finance.entryPayerTaxId')}</span>
          <input value={payerTaxId} onChange={(event) => setPayerTaxId(event.target.value)} />
        </label>
        <label>
          <span>{t('finance.entryRecipientName')}</span>
          <input value={recipientName} onChange={(event) => setRecipientName(event.target.value)} />
        </label>
        <label>
          <span>{t('finance.entryRecipientTaxIdMask')}</span>
          <input
            inputMode="numeric"
            maxLength={4}
            value={recipientTaxIdMask}
            onChange={(event) => setRecipientTaxIdMask(event.target.value)}
          />
        </label>
      </div>
      <label className="checkbox">
        <input type="checkbox" checked={isCorrected} onChange={(event) => setIsCorrected(event.target.checked)} />
        {t('finance.entryIsCorrected')}
      </label>
      <fieldset className="finance-entry-lines">
        <legend>{t('finance.entryTaxFormEntries')}</legend>
        <p className="muted field-hint">{t('finance.entryTaxFormEntriesHint')}</p>
        {entries.map((entry, index) => (
          <div
            key={entry.key}
            className="finance-entry-line"
            role="group"
            aria-label={t('finance.entryBoxNumber', { number: index + 1 })}
          >
            <strong className="finance-entry-line-number muted">
              {t('finance.entryBoxNumber', { number: index + 1 })}
            </strong>
            <label>
              <span>{t('finance.entryBoxCode')}</span>
              <input
                value={entry.boxCode}
                onChange={(event) => changeEntry(entry.key, { boxCode: event.target.value })}
              />
            </label>
            <label>
              <span>{t('finance.entryBoxQualifier')}</span>
              <input
                value={entry.boxQualifier}
                onChange={(event) => changeEntry(entry.key, { boxQualifier: event.target.value })}
              />
            </label>
            <label>
              <span>{t('finance.entryBoxAmount')}</span>
              <input
                inputMode="decimal"
                value={entry.amount}
                onChange={(event) => changeEntry(entry.key, { amount: event.target.value })}
              />
            </label>
            <label className="finance-entry-wide">
              <span>{t('finance.entryBoxTextValue')}</span>
              <input
                value={entry.textValue}
                onChange={(event) => changeEntry(entry.key, { textValue: event.target.value })}
              />
            </label>
            <label className="checkbox">
              <input
                type="checkbox"
                checked={entry.isChecked}
                onChange={(event) => changeEntry(entry.key, { isChecked: event.target.checked })}
              />
              {t('finance.entryBoxIsChecked')}
            </label>
            <Tooltip label={t('common.remove')}>
              <button
                type="button"
                className="icon-action danger finance-entry-line-remove"
                disabled={entries.length === 1}
                aria-label={`${t('finance.entryBoxNumber', { number: index + 1 })}: ${t('common.remove')}`}
                onClick={() => removeEntry(entry.key)}
              >
                <CloseIcon size={16} />
              </button>
            </Tooltip>
          </div>
        ))}
        <button type="button" onClick={addEntry}>
          {t('finance.entryAddBox')}
        </button>
      </fieldset>
      <SourceFileField
        label="finance.entryTaxFormFile"
        hint="finance.entryTaxDocumentFileHint"
        accept="application/pdf,image/*"
        uploadPath={TAX_DOCUMENT_UPLOAD_PATH}
        notStoredKey="finance.entryFileNotStored"
        isDisabled={isBusy}
        onChange={setSourceFile}
      />
    </FormDialog>
  )
}

import { useRef, useState } from 'react'

import { Tag } from '../../components/common'
import { FormDialog } from '../../components/dialog'
import { CloseIcon, PaperclipIcon } from '../../components/icons'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useTranslation } from '../../i18n/i18n'
import {
  addShoppingItems,
  createShoppingList,
  previewShoppingImport,
  ShoppingImportItem,
  ShoppingItemEntry,
  ShoppingList,
} from './shoppingApi'
import { MAXIMUM_UPLOAD_BYTES, plainNumber, quantityText, uploadAgentAttachment } from './shoppingItems'

// Importing a list: paste it or add pictures of it, say where it goes, and
// read what the model made of it before anything is added. The preview shows
// each item with the aisle it was under, and whether it is new or raises the
// count of one already on the list; nothing is added until Add, and an item
// unticked is left out. "A new list" makes the list only when Add is pressed.

const NEW_LIST = 'new'

export function ShoppingImportDialog({
  shoppingList,
  onAdded,
  onClose,
}: {
  shoppingList: ShoppingList
  // Called after items were added, with the list they went on, which is a new
  // one when the person chose that.
  onAdded: (shoppingListId: string) => void
  onClose: () => void
}) {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const [importText, setImportText] = useState('')
  const [files, setFiles] = useState<File[]>([])
  const [destination, setDestination] = useState(shoppingList.id)
  const [newListName, setNewListName] = useState('')
  const [readItems, setReadItems] = useState<ShoppingImportItem[] | null>(null)
  const [includedIndexes, setIncludedIndexes] = useState<ReadonlySet<number>>(new Set())
  const [isBusy, setIsBusy] = useState(false)
  const fileInput = useRef<HTMLInputElement>(null)
  // A picture is sent once however many times the list is read again.
  const attachmentIds = useRef(new Map<File, string>())

  const isNewList = destination === NEW_LIST
  const hasInput = importText.trim() !== '' || files.length > 0
  const canRead = hasInput && (!isNewList || newListName.trim() !== '')

  async function read() {
    setIsBusy(true)
    try {
      const agentAttachmentIds: string[] = []
      for (const file of files) {
        let agentAttachmentId = attachmentIds.current.get(file)
        if (!agentAttachmentId) {
          agentAttachmentId = await uploadAgentAttachment(file)
          attachmentIds.current.set(file, agentAttachmentId)
        }
        agentAttachmentIds.push(agentAttachmentId)
      }
      const items = await previewShoppingImport(isNewList ? null : destination, importText, agentAttachmentIds)
      setReadItems(items)
      setIncludedIndexes(new Set(items.keys()))
    } catch (caught) {
      toast.failure(caught, t('shopping.failed'))
    } finally {
      setIsBusy(false)
    }
  }

  async function add() {
    if (!readItems) return
    const entries: ShoppingItemEntry[] = readItems
      .filter((_item, index) => includedIndexes.has(index))
      .map((item) => ({
        itemDescription: item.itemDescription,
        itemDetails: item.itemDetails,
        storeName: item.storeName,
        isStoreBrand: item.isStoreBrand,
        aisleName: item.aisleName,
        neededQuantity: item.neededQuantity,
        quantityUnit: item.quantityUnit,
      }))
    setIsBusy(true)
    try {
      const shoppingListId = isNewList ? (await createShoppingList(newListName.trim())).id : destination
      await addShoppingItems(shoppingListId, entries)
      toast.done(plural(entries.length, { one: 'shopping.importAddedOne', other: 'shopping.importAddedOther' }))
      onAdded(shoppingListId)
      onClose()
    } catch (caught) {
      toast.failure(caught, t('shopping.failed'))
    } finally {
      setIsBusy(false)
    }
  }

  function chooseFiles(chosen: FileList | null) {
    const accepted: File[] = []
    for (const file of Array.from(chosen ?? [])) {
      if (file.size > MAXIMUM_UPLOAD_BYTES) {
        toast.failed(t('shopping.fileTooLarge', { name: file.name, size: String(MAXIMUM_UPLOAD_BYTES >> 20) }))
      } else {
        accepted.push(file)
      }
    }
    setFiles((previous) => [...previous, ...accepted])
  }

  function toggle(index: number) {
    setIncludedIndexes((previous) => {
      const next = new Set(previous)
      if (next.has(index)) next.delete(index)
      else next.add(index)
      return next
    })
  }

  if (readItems) {
    return (
      <FormDialog
        wide
        title={t('shopping.importPreviewTitle')}
        submitLabel={plural(includedIndexes.size, { one: 'shopping.importAddOne', other: 'shopping.importAddOther' })}
        busy={isBusy}
        canSubmit={includedIndexes.size > 0}
        onSubmit={() => void add()}
        onClose={onClose}
        otherAction={
          <button type="button" disabled={isBusy} onClick={() => setReadItems(null)}>
            {t('shopping.back')}
          </button>
        }
      >
        <p className="muted">{t('shopping.importPreviewHint')}</p>
        {readItems.length === 0 ? <p className="muted">{t('shopping.importNothingRead')}</p> : null}
        {readItems.length > 0 ? (
          <div className="table-wrap">
            <table className="shopping-table">
              <thead>
                <tr>
                  <th />
                  <th>{t('shopping.aisle')}</th>
                  <th>{t('shopping.need')}</th>
                  <th>{t('shopping.itemDescription')}</th>
                  <th>{t('shopping.importStatus')}</th>
                </tr>
              </thead>
              <tbody>
                {readItems.map((item, index) => (
                  <tr key={index}>
                    <td className="shrink">
                      <input
                        type="checkbox"
                        checked={includedIndexes.has(index)}
                        aria-label={t('shopping.importInclude', { itemDescription: item.itemDescription })}
                        onChange={() => toggle(index)}
                      />
                    </td>
                    <td>{item.aisleName}</td>
                    <td>{quantityText(item)}</td>
                    <td>
                      <div className="shopping-item-name">
                        <strong>{item.itemDescription}</strong>
                        {item.itemDetails ? <div className="muted">{item.itemDetails}</div> : null}
                        {item.storeName ? <div className="muted">{item.storeName}</div> : null}
                      </div>
                    </td>
                    <td>
                      <ImportStatus item={item} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </FormDialog>
    )
  }

  return (
    <FormDialog
      wide
      title={t('shopping.importTitle')}
      submitLabel={isBusy ? t('shopping.importReading') : t('shopping.importRead')}
      busy={isBusy}
      canSubmit={canRead}
      onSubmit={() => void read()}
      onClose={onClose}
    >
      <p className="muted">{t('shopping.importHint')}</p>
      <label>
        {t('shopping.importPaste')}
        <textarea
          rows={7}
          value={importText}
          placeholder={t('shopping.importPastePlaceholder')}
          onChange={(event) => setImportText(event.target.value)}
        />
      </label>
      <div className="shopping-import-files">
        <button type="button" disabled={isBusy} onClick={() => fileInput.current?.click()}>
          <PaperclipIcon size={16} /> {t('shopping.importAddPictures')}
        </button>
        <input
          ref={fileInput}
          type="file"
          accept="image/*"
          multiple
          hidden
          aria-label={t('shopping.importAddPictures')}
          onChange={(event) => {
            chooseFiles(event.target.files)
            event.target.value = ''
          }}
        />
        {files.map((file, index) => (
          <span key={`${file.name}-${index}`} className="shopping-import-file">
            {file.name}
            <Tooltip label={t('shopping.importRemovePicture', { name: file.name })}>
              <button
                type="button"
                className="icon-action"
                aria-label={t('shopping.importRemovePicture', { name: file.name })}
                disabled={isBusy}
                onClick={() => setFiles((previous) => previous.filter((_file, position) => position !== index))}
              >
                <CloseIcon size={14} />
              </button>
            </Tooltip>
          </span>
        ))}
      </div>
      <div className="row">
        <label>
          {t('shopping.importInto')}
          <select value={destination} onChange={(event) => setDestination(event.target.value)}>
            <option value={shoppingList.id}>{shoppingList.listName}</option>
            <option value={NEW_LIST}>{t('shopping.importNewList')}</option>
          </select>
        </label>
        {isNewList ? (
          <label>
            {t('shopping.listName')}
            <input
              value={newListName}
              maxLength={120}
              placeholder={t('shopping.listNamePlaceholder')}
              onChange={(event) => setNewListName(event.target.value)}
            />
          </label>
        ) : null}
      </div>
      {isBusy ? <p className="muted">{t('shopping.importReadingHint')}</p> : null}
    </FormDialog>
  )
}

// ImportStatus says what adding an item would do: make a new one, or raise
// the count of one already on the list to the larger of the two.
function ImportStatus({ item }: { item: ShoppingImportItem }) {
  const { t } = useTranslation()
  if (!item.mergedShoppingItemId) return <Tag value={t('shopping.importNew')} tone="good" />
  const from = plainNumber(item.mergedNeededQuantity)
  const to = String(Math.max(Number(item.mergedNeededQuantity) || 0, Number(item.neededQuantity) || 0))
  return (
    <Tag
      value={
        item.mergedShoppingItemState === 'not_needed'
          ? t('shopping.importOnListAgain', { to })
          : t('shopping.importOnList', { from, to })
      }
      tone="warn"
    />
  )
}

import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'

import { useBreadcrumbDetail } from '../../components/breadcrumb'
import { ErrorMessage, formatMoney, Loading, Tag } from '../../components/common'
import { ConfirmDialog } from '../../components/dialog'
import { Select } from '../../components/select'
import { SettingsEmpty } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { useQuery } from '../../components/useQuery'
import { Key, useTranslation } from '../../i18n/i18n'
import { hasPermission, useOptionalSession } from '../../session'
import { formatDay } from '../finance/financeApi'
import {
  addShoppingItemFromReceiptLine,
  decideShoppingReceiptMatch,
  listShoppingLists,
  listShoppingReceiptMatches,
  matchShoppingReceipt,
  matchShoppingReceiptLine,
  ReceiptMatchState,
  ShoppingList,
  ShoppingReceipt,
  ShoppingReceiptLine,
  undoShoppingReceipt,
} from './shoppingApi'
import { shoppingReceiptAddress } from './shoppingItems'
import { ShoppingPicture } from './shoppingParts'
import { useShoppingEvents } from './useShoppingEvents'

// What one receipt did to the person's shopping lists. Every receipt Finance
// keeps is matched against the lists, whatever way it arrived: the lines the
// matcher was sure of checked their items off and remembered the price; the
// ones it was unsure of wait here to be applied or dismissed. Each line can be
// tied to another item, or added to a list when nothing matched it, and the
// check-offs can all be undone.

const STATE_WORDS: Record<ReceiptMatchState, Key> = {
  applied: 'shopping.matchApplied',
  proposed: 'shopping.matchProposed',
  dismissed: 'shopping.matchDismissed',
}

const STATE_TONES: Record<ReceiptMatchState, 'good' | 'warn' | undefined> = {
  applied: 'good',
  proposed: 'warn',
  dismissed: undefined,
}

// ShoppingReceiptPage is the receipt's own address, /shopping/receipts/:id.
export function ShoppingReceiptPage() {
  const { financeReceiptId = '' } = useParams()
  const { t } = useTranslation()
  const lists = useQuery(() => listShoppingLists(), [], { refresh: false })
  useBreadcrumbDetail(t('shopping.receiptTitle'))
  return (
    <section className="card shopping-list-card">
      <ErrorMessage error={lists.error} />
      <ShoppingReceiptMatches financeReceiptId={financeReceiptId} shoppingLists={lists.data ?? []} />
    </section>
  )
}

// ShoppingReceiptSection is the "Shopping lists" part of Finance's receipt
// dialog: the same matches, with the way to the full view. It draws nothing
// for a person who does not use shopping lists.
export function ShoppingReceiptSection({ financeReceiptId }: { financeReceiptId: string }) {
  const session = useOptionalSession()
  if (!session || !hasPermission(session.permissions, 'shopping:use')) return null
  return <ShoppingReceiptSectionContent financeReceiptId={financeReceiptId} />
}

function ShoppingReceiptSectionContent({ financeReceiptId }: { financeReceiptId: string }) {
  const { t } = useTranslation()
  const lists = useQuery(() => listShoppingLists(), [], { refresh: false })
  // A person with no list has nothing for a receipt to be matched to, and the
  // receipt's matches are not read until it is known that they have one.
  if (!lists.data || lists.data.length === 0) return <ErrorMessage error={lists.error} />
  return (
    <section className="shopping-receipt-section" aria-label={t('shopping.receiptSection')}>
      <div className="shopping-receipt-section-head">
        <strong>{t('shopping.receiptSection')}</strong>
        <Link className="button-quiet" to={shoppingReceiptAddress(financeReceiptId)}>
          {t('shopping.receiptFullView')}
        </Link>
      </div>
      <ErrorMessage error={lists.error} />
      <ShoppingReceiptMatches financeReceiptId={financeReceiptId} shoppingLists={lists.data ?? []} />
    </section>
  )
}

export function ShoppingReceiptMatches({
  financeReceiptId,
  shoppingLists,
}: {
  financeReceiptId: string
  // The lists the person reaches, for choosing where a line with no match goes.
  shoppingLists: ShoppingList[]
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const receipt = useQuery(() => listShoppingReceiptMatches(financeReceiptId), [financeReceiptId], {
    refresh: false,
  })
  const [isBusy, setIsBusy] = useState(false)
  const [isUndoing, setIsUndoing] = useState(false)
  const [chosenListId, setChosenListId] = useState('')

  useShoppingEvents(() => void receipt.reload(true))

  async function act(work: () => Promise<unknown>, done?: string): Promise<boolean> {
    setIsBusy(true)
    try {
      await work()
      if (done) toast.done(done)
      await receipt.reload(true)
      return true
    } catch (caught) {
      toast.failure(caught, t('shopping.failed'))
      return false
    } finally {
      setIsBusy(false)
    }
  }

  if (receipt.loading && !receipt.data) {
    return (
      <>
        <ErrorMessage error={receipt.error} />
        <Loading />
      </>
    )
  }
  const view: ShoppingReceipt | null = receipt.data
  if (!view) {
    return (
      <>
        <ErrorMessage error={receipt.error} />
        <SettingsEmpty>{t('shopping.receiptNotFound')}</SettingsEmpty>
      </>
    )
  }

  const matches = view.shoppingReceiptLines.flatMap((line) => line.shoppingReceiptMatches)
  const hasApplied = matches.some((match) => match.shoppingReceiptMatch.receiptMatchState === 'applied')
  // Where a line with no match is added: the list of an item the receipt
  // already touched, else the first list the person owns.
  const matchedListId = matches.find((match) => match.shoppingItem)?.shoppingItem?.shoppingListId ?? ''
  const addListId =
    chosenListId || matchedListId || shoppingLists.find((each) => each.isOwner)?.id || shoppingLists[0]?.id || ''
  const hasUnmatchedLine = view.shoppingReceiptLines.some((line) => !hasLiveMatch(line))

  return (
    <div className="shopping-receipt">
      <ErrorMessage error={receipt.error} />
      <div className="shopping-receipt-head">
        <p className="muted">
          {[
            view.merchantName,
            view.purchasedOn ? formatDay(view.purchasedOn) : '',
            view.totalAmount ? formatMoney(Number(view.totalAmount), view.currencyCode) : '',
          ]
            .filter(Boolean)
            .join(' · ')}
        </p>
        <div className="row-actions">
          <button
            type="button"
            disabled={isBusy}
            onClick={() => void act(() => matchShoppingReceipt(financeReceiptId), t('shopping.matchingAgain'))}
          >
            {t('shopping.matchAgain')}
          </button>
          <button type="button" disabled={isBusy || !hasApplied} onClick={() => setIsUndoing(true)}>
            {t('shopping.undoReceipt')}
          </button>
        </div>
      </div>

      {view.shoppingReceiptLines.length === 0 ? <SettingsEmpty>{t('shopping.receiptNoLines')}</SettingsEmpty> : null}
      {view.shoppingReceiptLines.length > 0 ? (
        <>
          {hasUnmatchedLine && shoppingLists.length > 0 ? (
            <div className="shopping-receipt-add-to">
              <span className="muted">{t('shopping.receiptAddTo')}</span>
              <Select
                label={t('shopping.receiptAddTo')}
                value={addListId}
                options={shoppingLists.map((each) => ({ value: each.id, label: each.listName }))}
                onChange={setChosenListId}
              />
            </div>
          ) : null}
          <div className="table-wrap">
            <table className="shopping-table">
              <thead>
                <tr>
                  <th>{t('shopping.receiptLine')}</th>
                  <th>{t('shopping.receiptMatch')}</th>
                  <th>{t('shopping.receiptOther')}</th>
                </tr>
              </thead>
              <tbody>
                {view.shoppingReceiptLines.map((line) => (
                  <tr key={line.financeReceiptLineId}>
                    <td className="wrap">
                      <div className="shopping-item-name">
                        <strong>{line.description}</strong>
                        <div className="muted">
                          {/* The line's amount is what all of them cost: "2 for $6.98". */}
                          {line.quantity && Number(line.quantity) !== 1 && line.lineAmount
                            ? t('shopping.receiptCountFor', {
                                count: String(Number(line.quantity)),
                                amount: formatMoney(Number(line.lineAmount), view.currencyCode),
                              })
                            : line.lineAmount
                              ? formatMoney(Number(line.lineAmount), view.currencyCode)
                              : ''}
                        </div>
                      </div>
                    </td>
                    <td className="wrap">
                      {line.shoppingReceiptMatches.length === 0 ? (
                        <span className="muted">{t('shopping.receiptNoMatch')}</span>
                      ) : (
                        <ul className="shopping-receipt-matches">
                          {line.shoppingReceiptMatches.map(({ shoppingReceiptMatch, shoppingItem }) => (
                            <li key={shoppingReceiptMatch.id}>
                              {shoppingItem ? <ShoppingPicture item={shoppingItem} /> : null}
                              <div className="shopping-item-name">
                                <strong>{shoppingItem?.itemDescription ?? ''}</strong>
                                <div className="muted">{shoppingItem?.listName ?? ''}</div>
                              </div>
                              <Tag
                                value={t(STATE_WORDS[shoppingReceiptMatch.receiptMatchState])}
                                tone={STATE_TONES[shoppingReceiptMatch.receiptMatchState]}
                              />
                              <span className="row-actions">
                                {shoppingReceiptMatch.receiptMatchState !== 'applied' ? (
                                  <button
                                    type="button"
                                    className="button-quiet"
                                    disabled={isBusy}
                                    onClick={() =>
                                      void act(() => decideShoppingReceiptMatch(shoppingReceiptMatch.id, true))
                                    }
                                  >
                                    {t('shopping.matchApply')}
                                  </button>
                                ) : null}
                                {shoppingReceiptMatch.receiptMatchState !== 'dismissed' ? (
                                  <button
                                    type="button"
                                    className="button-quiet"
                                    disabled={isBusy}
                                    onClick={() =>
                                      void act(() => decideShoppingReceiptMatch(shoppingReceiptMatch.id, false))
                                    }
                                  >
                                    {t('shopping.matchDismiss')}
                                  </button>
                                ) : null}
                              </span>
                            </li>
                          ))}
                        </ul>
                      )}
                    </td>
                    <td className="wrap">
                      <div className="shopping-receipt-other">
                        {view.stillNeededItems.length > 0 ? (
                          <Select
                            label={`${line.description}: ${t('shopping.receiptTieToItem')}`}
                            value=""
                            placeholder={t('shopping.receiptTieToItem')}
                            options={view.stillNeededItems.map((item) => ({
                              value: item.id,
                              label: `${item.itemDescription} (${item.listName})`,
                            }))}
                            disabled={isBusy}
                            onChange={(shoppingItemId) =>
                              void act(
                                () => matchShoppingReceiptLine(shoppingItemId, line.financeReceiptLineId),
                                t('shopping.receiptTied'),
                              )
                            }
                          />
                        ) : null}
                        {!hasLiveMatch(line) && addListId ? (
                          <button
                            type="button"
                            className="button-quiet"
                            disabled={isBusy}
                            onClick={() =>
                              void act(
                                () => addShoppingItemFromReceiptLine(line.financeReceiptLineId, addListId),
                                t('shopping.receiptAdded', { description: line.description }),
                              )
                            }
                          >
                            {t('shopping.receiptAddToList')}
                          </button>
                        ) : null}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      ) : null}

      {view.stillNeededItems.length > 0 ? (
        <>
          <h4 className="shopping-share-heading">{t('shopping.receiptStillNeeded')}</h4>
          <ul className="shopping-receipt-matches">
            {view.stillNeededItems.map((item) => (
              <li key={item.id}>
                <ShoppingPicture item={item} />
                <div className="shopping-item-name">
                  <strong>{item.itemDescription}</strong>
                  <div className="muted">{item.listName}</div>
                </div>
              </li>
            ))}
          </ul>
        </>
      ) : null}

      {isUndoing ? (
        <ConfirmDialog
          destructive={false}
          title={t('shopping.undoReceiptTitle')}
          body={t('shopping.undoReceiptBody')}
          confirmLabel={t('shopping.undoReceipt')}
          busy={isBusy}
          onConfirm={() =>
            void (async () => {
              const isDone = await act(() => undoShoppingReceipt(financeReceiptId), t('shopping.undoReceiptDone'))
              if (isDone) setIsUndoing(false)
            })()
          }
          onClose={() => setIsUndoing(false)}
        />
      ) : null}
    </div>
  )
}

// hasLiveMatch says whether a line is tied to an item that was applied or is
// waiting to be: one whose every match was dismissed is a line with none.
function hasLiveMatch(line: ShoppingReceiptLine): boolean {
  return line.shoppingReceiptMatches.some((match) => match.shoppingReceiptMatch.receiptMatchState !== 'dismissed')
}

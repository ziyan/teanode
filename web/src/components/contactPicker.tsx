import { useState } from 'react'

import { graphql } from '../api'
import { useQuery } from './useQuery'
import { useDebounced } from './preview'
import { useTranslation } from '../i18n/i18n'

// ContactPicker is the search box and the list of the person's contacts under
// it, one of which is chosen, for a dialog that ties something to a contact.
// The contacts of every address book are listed, each with the book it is in,
// since whatever the dialog saves names both.

type Book = { id: string }

export type ContactChoice = {
  addressBookId: string
  id: string
  name?: string
  organization?: string
  emails: string[]
  accountUserId?: string | null
}

const BOOKS = `query { ListAddressBooks { id } }`
const CONTACTS = `query ($addressBookId: String!, $query: String, $first: Int) {
  ListContacts(addressBookId: $addressBookId, query: $query, first: $first) { id name organization emails accountUserId }
}`

// How many contacts of each book the picker lists: more than fits without
// scrolling, few enough that the search is how a contact is found in a big book.
const CONTACT_CHOICES = 20

export function contactChoiceLabel(choice: ContactChoice): string {
  return choice.name || choice.emails[0] || choice.organization || choice.id
}

export function contactChoiceKey(choice: ContactChoice): string {
  return `${choice.addressBookId}/${choice.id}`
}

export function ContactPicker({
  chosen,
  isLinkedLeftOut = false,
  detailOf,
  rankOf,
  onChoose,
  onFailed,
}: {
  chosen: ContactChoice | null
  // Leave out a contact that is somebody in the circles already, when the dialog
  // is about making that link: a contact is one person.
  isLinkedLeftOut?: boolean
  // A line under a contact's name, besides its address.
  detailOf?: (choice: ContactChoice) => string
  // Higher first; the rest stay in the order the book lists them.
  rankOf?: (choice: ContactChoice) => number
  onChoose: (choice: ContactChoice) => void
  onFailed?: (failure: unknown) => void
}) {
  const { t } = useTranslation()
  const [typed, setTyped] = useState('')
  const searchText = useDebounced(typed.trim(), 250)
  const choices = useQuery(
    async () => {
      try {
        const books = (await graphql<{ ListAddressBooks: Book[] }>(BOOKS)).ListAddressBooks
        const found = await Promise.all(
          books.map(async (book) => {
            const answer = await graphql<{ ListContacts: Omit<ContactChoice, 'addressBookId'>[] }>(CONTACTS, {
              addressBookId: book.id,
              query: searchText || null,
              first: CONTACT_CHOICES,
            })
            return answer.ListContacts.map((contact) => ({ ...contact, addressBookId: book.id }))
          }),
        )
        return found.flat()
      } catch (failure) {
        onFailed?.(failure)
        throw failure
      }
    },
    [searchText],
    { refresh: false },
  )
  const contacts = (choices.data ?? [])
    .filter((choice) => !(isLinkedLeftOut && choice.accountUserId))
    .sort((first, second) => (rankOf?.(second) ?? 0) - (rankOf?.(first) ?? 0))

  return (
    <>
      <label>
        {t('memoryLinks.contactSearch')}
        <input
          type="search"
          value={typed}
          placeholder={t('memoryLinks.contactSearchPlaceholder')}
          autoComplete="off"
          onChange={(event) => setTyped(event.target.value)}
        />
      </label>
      <div className="contact-picker" role="radiogroup" aria-label={t('memoryLinks.contactSearch')}>
        {contacts.length === 0 && !choices.loading ? (
          <p className="muted contact-picker-empty">{t('memoryLinks.noContacts')}</p>
        ) : null}
        {contacts.map((choice) => {
          const isChosen = chosen !== null && contactChoiceKey(chosen) === contactChoiceKey(choice)
          const secondary = [
            detailOf?.(choice) ?? '',
            choice.emails[0] && contactChoiceLabel(choice) !== choice.emails[0] ? choice.emails[0] : '',
          ].filter(Boolean)
          return (
            <label
              key={contactChoiceKey(choice)}
              className={isChosen ? 'contact-picker-row is-chosen' : 'contact-picker-row'}
            >
              <input type="radio" name="picked-contact" checked={isChosen} onChange={() => onChoose(choice)} />
              <span className="contact-picker-text">
                <span className="contact-picker-name">{contactChoiceLabel(choice)}</span>
                {secondary.length > 0 && <span className="contact-picker-detail">{secondary.join(' · ')}</span>}
              </span>
            </label>
          )
        })}
      </div>
    </>
  )
}

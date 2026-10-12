import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import { personReaction, ReactionBadges, ReactionPicker, withReaction } from './agentReactions'

vi.mock('../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, string>) => (values?.emoji ? `${key} ${values.emoji}` : key),
  }),
}))
afterEach(cleanup)

// A reactor has one reaction on a message: reacting again replaces it, an
// empty emoji takes it away, and the other reactor's is left alone.
it('keeps one reaction per reactor', () => {
  const agentOnly = withReaction(undefined, 'agent', '🙏')
  const both = withReaction(agentOnly, 'person', '👍')
  expect(both.map((reaction) => reaction.reactorKind)).toEqual(['person', 'agent'])
  const replaced = withReaction(both, 'person', '🔥')
  expect(personReaction(replaced)).toBe('🔥')
  expect(replaced).toHaveLength(2)
  const removed = withReaction(replaced, 'person', '')
  expect(personReaction(removed)).toBe('')
  expect(removed).toEqual([{ emoji: '🙏', reactorKind: 'agent', reactedAt: undefined }])
})

// Choosing the reaction the person already has takes it away; choosing
// another puts that one on.
it('takes the current reaction away when it is chosen again', () => {
  const picked = vi.fn()
  render(<ReactionPicker current="👍" onPick={picked} onClose={() => undefined} />)
  fireEvent.click(screen.getByRole('menuitemcheckbox', { name: 'agentDrawer.reactionYours 👍' }))
  fireEvent.click(screen.getByRole('menuitemcheckbox', { name: 'agentDrawer.reactWith 🎉' }))
  expect(picked.mock.calls).toEqual([[''], ['🎉']])
})

// The person's own badge takes their reaction away; the agent's is only a
// badge.
it('lets the person take their own badge away and not the agent’s', () => {
  const removed = vi.fn()
  render(
    <ReactionBadges
      reactions={[
        { emoji: '👍', reactorKind: 'person' },
        { emoji: '🙏', reactorKind: 'agent' },
      ]}
      onRemove={removed}
    />,
  )
  fireEvent.click(screen.getByRole('button', { name: 'agentDrawer.reactionYours 👍' }))
  expect(removed).toHaveBeenCalledOnce()
  expect(screen.queryByRole('button', { name: 'agentDrawer.reactionAgent 🙏' })).toBeNull()
  expect(screen.getByLabelText('agentDrawer.reactionAgent 🙏')).toBeTruthy()
})

// A reaction that is not one of the quick ones is shown, marked, at the end
// of the row, so the person sees what they chose and can take it away.
it('shows a reaction outside the quick row as chosen', () => {
  const picked = vi.fn()
  render(<ReactionPicker current="🦄" onPick={picked} onClose={() => undefined} />)
  fireEvent.click(screen.getByRole('menuitemcheckbox', { name: 'agentDrawer.reactionYours 🦄' }))
  expect(picked).toHaveBeenCalledWith('')
})

// More opens every other emoji; one chosen from its grid is the reaction.
it('reacts with an emoji chosen from the full set', () => {
  const picked = vi.fn()
  render(<ReactionPicker current="" onPick={picked} onClose={() => undefined} />)
  expect(screen.queryByRole('dialog')).toBeNull()
  const more = screen.getByRole('menuitem', { name: 'agentDrawer.reactMore' })
  expect(more.getAttribute('aria-expanded')).toBe('false')
  fireEvent.click(more)
  expect(more.getAttribute('aria-expanded')).toBe('true')
  expect(screen.getByRole('dialog', { name: 'agentDrawer.reactMoreTitle' })).toBeTruthy()
  fireEvent.click(screen.getByRole('button', { name: 'agentDrawer.reactWith 🦄' }))
  expect(picked).toHaveBeenCalledWith('🦄')
})

// A typed or pasted entry reacts only when it is one or two emoji, and says
// what is wrong with it otherwise.
it('takes a typed emoji and refuses anything else with a hint', () => {
  const picked = vi.fn()
  render(<ReactionPicker current="" onPick={picked} onClose={() => undefined} />)
  fireEvent.click(screen.getByRole('menuitem', { name: 'agentDrawer.reactMore' }))
  const box = screen.getByRole('textbox', { name: 'agentDrawer.emojiEntryLabel' })
  const submit = screen.getByRole('button', { name: 'agentDrawer.emojiEntrySubmit' }) as HTMLButtonElement
  expect(submit.disabled).toBe(true)
  expect(screen.queryByRole('status')).toBeNull()

  fireEvent.change(box, { target: { value: 'yes' } })
  expect(submit.disabled).toBe(true)
  expect(screen.getByRole('status').textContent).toBe('agentDrawer.emojiEntryHintNotEmoji')
  fireEvent.change(box, { target: { value: '👍🔥🎉' } })
  expect(screen.getByRole('status').textContent).toBe('agentDrawer.emojiEntryHintTooMany')
  fireEvent.submit(box.closest('form') as HTMLFormElement)
  expect(picked).not.toHaveBeenCalled()

  fireEvent.change(box, { target: { value: ' 🇯🇵 ' } })
  expect(screen.queryByRole('status')).toBeNull()
  expect(submit.disabled).toBe(false)
  fireEvent.click(submit)
  expect(picked).toHaveBeenCalledWith('🇯🇵')
})

// Escape closes More first and returns to its button; a second one closes
// the picker.
it('closes More on the first Escape and the picker on the second', () => {
  const closed = vi.fn()
  render(<ReactionPicker current="" onPick={() => undefined} onClose={closed} />)
  const more = screen.getByRole('menuitem', { name: 'agentDrawer.reactMore' })
  fireEvent.click(more)
  fireEvent.keyDown(document, { key: 'Escape' })
  expect(screen.queryByRole('dialog')).toBeNull()
  expect(document.activeElement).toBe(more)
  expect(closed).not.toHaveBeenCalled()
  fireEvent.keyDown(document, { key: 'Escape' })
  expect(closed).toHaveBeenCalledOnce()
})

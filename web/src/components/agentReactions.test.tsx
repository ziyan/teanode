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

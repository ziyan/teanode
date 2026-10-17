import { cleanup, render } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import { Avatar, avatarColorIndexOf } from './avatar'
import { fitWithin } from './avatarPicture'

afterEach(cleanup)

describe('Avatar', () => {
  it('shows the first letter on the color the server chose when there is no picture', () => {
    const { container } = render(<Avatar name="alex" colorIndex={7} />)
    const avatar = container.querySelector('.avatar') as HTMLElement
    expect(avatar.textContent).toBe('A')
    expect(avatar.classList.contains('avatar-color-7')).toBe(true)
    expect(container.querySelector('img')).toBeNull()
  })

  it('shows the picture, asked for at the small size, when there is one', () => {
    const { container } = render(
      <Avatar name="alex" avatarUrl="/api/v1/avatar/user/abc?v=3" colorIndex={2} size={28} />,
    )
    const picture = container.querySelector('img') as HTMLImageElement
    expect(picture.getAttribute('src')).toBe('/api/v1/avatar/user/abc?v=3&size=64')
    expect(container.querySelector('.avatar')?.textContent).toBe('')
  })

  it('asks for the large rendering at a large size', () => {
    const { container } = render(<Avatar name="alex" avatarUrl="/api/v1/avatar/user/abc?v=3" size={64} />)
    expect(container.querySelector('img')?.getAttribute('src')).toBe('/api/v1/avatar/user/abc?v=3&size=512')
  })

  it('does not cut a name that begins outside the basic plane in half', () => {
    const { container } = render(<Avatar name="🌱 seedling" colorIndex={0} />)
    expect(container.querySelector('.avatar')?.textContent).toBe('🌱')
  })

  it('falls back to a color computed from the name when the server sent none', () => {
    const { container } = render(<Avatar name="user-a" />)
    expect(container.querySelector('.avatar')?.classList.contains('avatar-color-0')).toBe(true)
  })
})

// The same three vectors as AvatarColor in the iPhone app and InitialColor on
// the server, so the three cannot drift apart.
describe('avatarColorIndexOf', () => {
  it('matches the shared vectors', () => {
    expect(avatarColorIndexOf('01JABCDEFGHJKMNPQRSTVWXYZ0')).toBe(10)
    expect(avatarColorIndexOf('01HZZZZZZZZZZZZZZZZZZZZZZZ')).toBe(4)
    expect(avatarColorIndexOf('user-a')).toBe(0)
  })
})

describe('fitWithin', () => {
  it('scales the longer side down to the limit and keeps the proportions', () => {
    expect(fitWithin(4000, 3000, 2048)).toEqual({ width: 2048, height: 1536 })
    expect(fitWithin(1000, 4096, 2048)).toEqual({ width: 500, height: 2048 })
  })

  it('never enlarges a picture that is small enough', () => {
    expect(fitWithin(640, 480, 2048)).toEqual({ width: 640, height: 480 })
  })
})

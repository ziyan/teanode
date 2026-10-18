import { describe, expect, it } from 'vitest'
import { findProblems } from './check-link-buttons.mjs'

describe('check-link-buttons', () => {
  it('flags a button with the link class and no reason', () => {
    const problems = findProblems('<button type="button" className="link" onClick={go}>Go</button>')
    expect(problems).toHaveLength(1)
  })

  it('flags the tag that carries the class, not the first tag on the line', () => {
    const problems = findProblems('<p><button type="button" className="link">Go</button></p>')
    expect(problems).toHaveLength(1)
  })

  it('allows a reason on the same line or the line above', () => {
    expect(findProblems('<button /* link-button: words inside a sentence */ className="link">Go</button>')).toEqual([])
    expect(
      findProblems(
        '{/* link-button: words inside a sentence */}\n<button\n  type="button"\n  className="link"\n>Go</button>',
      ),
    ).toEqual([])
  })

  it('does not accept a reason further away', () => {
    const text =
      '// link-button: not this one\n\n\n<button\n  type="button"\n  onClick={go}\n  className="link"\n>Go</button>'
    expect(findProblems(text)).toHaveLength(1)
  })

  it('finds the class in a template and in a ternary', () => {
    expect(findProblems('<button className={`link ${extra}`}>Go</button>')).toHaveLength(1)
    expect(findProblems('<button className={`${extra} link`}>Go</button>')).toHaveLength(1)
    expect(findProblems("<button className={open ? 'link' : 'muted'}>Go</button>")).toHaveLength(1)
    expect(findProblems('<button className={open ? "muted" : "link small"}>Go</button>')).toHaveLength(1)
  })

  it('does not take a longer class for the link class', () => {
    expect(findProblems('<button className="linked">Go</button>')).toEqual([])
    expect(findProblems('<button className={`photo-link ${extra}`}>Go</button>')).toEqual([])
  })

  it('flags an anchor with no href and a router Link, not an anchor with an href', () => {
    expect(findProblems('<a className="link" onClick={go}>Go</a>')).toHaveLength(1)
    expect(findProblems('<Link className="link" to="/x">Go</Link>')).toHaveLength(1)
    expect(findProblems('<a className="link" href={path} download>Go</a>')).toEqual([])
  })
})

const { test } = require('node:test')
const assert = require('node:assert/strict')
const braces = require('braces')
const uri = require('fast-uri')

test('deep brace and parenthesis patterns are refused before recursive walking', () => {
  for (const [opening, closing] of [
    ['{', '}'],
    ['(', ')'],
  ]) {
    const pattern = opening.repeat(256) + 'a,b' + closing.repeat(256)
    for (const operation of [braces.parse, braces.compile, braces.expand, braces.stringify]) {
      assert.throws(() => operation(pattern), {
        name: 'SyntaxError',
        message: 'Brace pattern nesting exceeds 128 levels',
      })
    }
  }
  assert.deepEqual(braces.expand('folder/{a,b}/{1,2}'), ['folder/a/1', 'folder/a/2', 'folder/b/1', 'folder/b/2'])
  assert.equal(braces.compile('folder/{a,b}'), 'folder/(a|b)')
})
test('encoded uppercase hosts have the same normalized host', () => {
  assert.equal(uri.parse('//%41.example.com').host, 'a.example.com')
  assert.equal(uri.equal('//%41.example.com', '//a.example.com'), true)
})

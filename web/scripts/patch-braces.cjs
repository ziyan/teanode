// The upstream parser creates arbitrarily deep trees for recursive walkers.
// Keep this temporary guard until the advisory has a published patched release.
const { readFileSync, writeFileSync } = require('node:fs')
const { createHash } = require('node:crypto')
const { join, dirname } = require('node:path')
const packageDirectory = dirname(require.resolve('braces/package.json'))
const parserPath = join(packageDirectory, 'lib/parse.js')
const parserSource = readFileSync(parserPath, 'utf8')
const marker = '// Bounded nesting for GHSA-vfj7-8cjw-p6xm.'
if (!parserSource.includes(marker)) {
  const expectedHash = 'e572166565f15fa6ad9865ae49d678218e32aabfd1b3720f6d0d43d39800d310'
  if (createHash('sha256').update(parserSource).digest('hex') !== expectedHash) {
    throw new Error('The braces parser changed; review the nesting guard before installing.')
  }
  const guardedSource = parserSource.replaceAll(
    'stack.push(block);',
    `${marker}\n      if (stack.length >= 128) throw new SyntaxError('Brace pattern nesting exceeds 128 levels');\n      stack.push(block);`,
  )
  if (guardedSource === parserSource) throw new Error('The braces parser guard was not applied.')
  writeFileSync(parserPath, guardedSource)
}

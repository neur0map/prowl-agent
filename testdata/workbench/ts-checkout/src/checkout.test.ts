import assert from 'node:assert/strict'
import test from 'node:test'

import { checkout } from './checkout.js'

test('adds tax to a cart subtotal', () => {
  assert.deepEqual(checkout([{ sku: 'tea', unitPriceCents: 500, quantity: 2 }], 0.1), {
    subtotalCents: 1000,
    taxCents: 100,
    totalCents: 1100,
  })
})

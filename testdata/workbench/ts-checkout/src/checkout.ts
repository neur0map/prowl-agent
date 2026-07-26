import { subtotalCents, type CartItem } from './cart.js'

export type Checkout = {
  subtotalCents: number
  taxCents: number
  totalCents: number
}

export function checkout(items: readonly CartItem[], taxRate: number): Checkout {
  const subtotal = subtotalCents(items)
  const tax = Math.round(subtotal * taxRate)
  return { subtotalCents: subtotal, taxCents: tax, totalCents: subtotal + tax }
}

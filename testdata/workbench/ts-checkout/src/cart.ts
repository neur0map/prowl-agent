export type CartItem = {
  sku: string
  unitPriceCents: number
  quantity: number
}

export function subtotalCents(items: readonly CartItem[]): number {
  return items.reduce((total, item) => total + item.unitPriceCents * item.quantity, 0)
}

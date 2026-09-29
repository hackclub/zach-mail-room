import type { Item, Line } from './types';

/** item id -> quantity */
export type Cart = Record<number, number>;

export const emptyCart = (): Cart => ({});

export function setQuantity(cart: Cart, item: Item, qty: number): Cart {
	const next = { ...cart };
	const q = Math.min(Math.max(Math.floor(qty) || 0, 0), item.max_per_request);
	if (q === 0) delete next[item.id];
	else next[item.id] = q;
	return next;
}

export const totalQuantity = (cart: Cart): number => Object.values(cart).reduce((a, b) => a + b, 0);

export function cartLines(cart: Cart, items: Item[]): Line[] {
	return items.filter((i) => cart[i.id]).map((i) => ({ item_id: i.id, quantity: cart[i.id] }));
}

/** Client-side mirror of the server's checks, for instant feedback. The server is authoritative. */
export function cartErrors(cart: Cart, maxItemsPerRequest: number): string[] {
	const total = totalQuantity(cart);
	if (total === 0) return ['Pick at least one item.'];
	if (maxItemsPerRequest > 0 && total > maxItemsPerRequest)
		return [`You can request at most ${maxItemsPerRequest} items at a time.`];
	return [];
}

import { describe, expect, it } from 'vitest';
import { cartErrors, cartLines, emptyCart, setQuantity, totalQuantity } from './cart';
import type { Item } from './types';

const items: Item[] = [
	{ id: 1, sku: 'Sti/A', name: 'Stickers', description: '', image_url: '', visible: true, sort_order: 0, max_per_request: 3, max_per_user: null },
	{ id: 2, sku: 'Swa/Sox', name: 'Socks', description: '', image_url: '', visible: true, sort_order: 1, max_per_request: 1, max_per_user: 1 }
];

describe('cart', () => {
	it('clamps quantity to the per-request max and drops zero lines', () => {
		let c = emptyCart();
		c = setQuantity(c, items[0], 10);
		expect(c[1]).toBe(3);
		c = setQuantity(c, items[0], 0);
		expect(c[1]).toBeUndefined();
		c = setQuantity(c, items[1], -4);
		expect(c[2]).toBeUndefined();
	});

	it('builds API lines in catalog order', () => {
		let c = emptyCart();
		c = setQuantity(c, items[1], 1);
		c = setQuantity(c, items[0], 2);
		expect(cartLines(c, items)).toEqual([
			{ item_id: 1, quantity: 2 },
			{ item_id: 2, quantity: 1 }
		]);
		expect(totalQuantity(c)).toBe(3);
	});

	it('reports empty and over-limit carts', () => {
		expect(cartErrors(emptyCart(), 5)).toEqual(['Pick at least one item.']);
		let c = setQuantity(emptyCart(), items[0], 3);
		c = setQuantity(c, items[1], 1);
		expect(cartErrors(c, 3)).toEqual(['You can request at most 3 items at a time.']);
		expect(cartErrors(c, 0)).toEqual([]);
	});
});

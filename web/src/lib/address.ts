import type { Address } from './types';

export const blankAddress = (): Address => ({
	first_name: '', last_name: '', line_1: '', line_2: '', city: '', state: '', postal_code: '', country: 'US', phone_number: ''
});

/**
 * Fill empty fields from the person's Hack Club Auth address. Anything already
 * typed wins. The default "US" country counts as empty.
 */
export function prefillAddress(current: Address, from: Address | null | undefined): Address {
	if (!from) return current;
	const out = { ...current };
	const blank = blankAddress();
	for (const k of Object.keys(out) as (keyof Address)[]) {
		const untouched = out[k].trim() === '' || (k === 'country' && out[k] === blank.country);
		if (untouched && from[k]) out[k] = from[k];
	}
	return out;
}

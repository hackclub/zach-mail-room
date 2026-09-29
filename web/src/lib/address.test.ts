import { describe, expect, it } from 'vitest';
import { blankAddress, prefillAddress } from './address';

describe('prefillAddress', () => {
	const hca = {
		first_name: 'Orpheus', last_name: 'Dino', line_1: '15 Falls Rd', line_2: 'Apt 2', city: 'Shelburne',
		state: 'VT', postal_code: '05482', country: 'US', phone_number: '+18025550199'
	};

	it('fills a blank form from the Hack Club Auth address', () => {
		expect(prefillAddress(blankAddress(), hca)).toEqual(hca);
	});

	it('never overwrites what the person already typed', () => {
		const typed = { ...blankAddress(), line_1: '1 Other St', city: 'Burlington' };
		const out = prefillAddress(typed, hca);
		expect(out.line_1).toBe('1 Other St');
		expect(out.city).toBe('Burlington');
		expect(out.first_name).toBe('Orpheus');
	});

	it('treats the default US country as blank so a non-US address can prefill it', () => {
		expect(prefillAddress(blankAddress(), { ...hca, country: 'GB' }).country).toBe('GB');
	});

	it('is a no-op without an address', () => {
		expect(prefillAddress(blankAddress(), undefined)).toEqual(blankAddress());
	});
});

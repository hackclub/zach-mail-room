import { afterEach, describe, expect, it, vi } from 'vitest';
import { api, ApiError, formatCents, isInternational } from './api';

afterEach(() => vi.unstubAllGlobals());

function stubFetch(status: number, body: unknown) {
	const fn = vi.fn().mockResolvedValue(
		new Response(body === undefined ? null : JSON.stringify(body), {
			status,
			headers: { 'Content-Type': 'application/json' }
		})
	);
	vi.stubGlobal('fetch', fn);
	return fn;
}

describe('api', () => {
	it('sends JSON with same-origin credentials', async () => {
		const fetch = stubFetch(201, { id: 7 });
		const out = await api.post<{ id: number }>('/api/requests', { a: 1 });
		expect(out.id).toBe(7);
		const [url, init] = fetch.mock.calls[0];
		expect(url).toBe('/api/requests');
		expect(init.method).toBe('POST');
		expect(init.credentials).toBe('same-origin');
		expect(init.headers['Content-Type']).toBe('application/json');
		expect(JSON.parse(init.body)).toEqual({ a: 1 });
	});

	it('throws ApiError carrying the server message', async () => {
		stubFetch(422, { error: 'you requested swag recently' });
		await expect(api.get('/api/requests')).rejects.toEqual(new ApiError(422, 'you requested swag recently'));
	});

	it('handles empty 204 responses', async () => {
		stubFetch(204, undefined);
		await expect(api.post('/auth/logout', {})).resolves.toBeUndefined();
	});
});

describe('helpers', () => {
	it('formats cents as USD', () => {
		expect(formatCents(1500)).toBe('$15.00');
		expect(formatCents(0)).toBe('$0.00');
	});
	it('detects international addresses', () => {
		expect(isInternational('US')).toBe(false);
		expect(isInternational('us')).toBe(false);
		expect(isInternational('CA')).toBe(true);
		expect(isInternational('')).toBe(false);
	});
});

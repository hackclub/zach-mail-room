import { api } from './api';
import type { Me } from './types';

export const session = $state<{ me: Me | null; loaded: boolean }>({ me: null, loaded: false });

export async function loadSession() {
	try {
		session.me = await api.get<Me>('/api/me');
	} catch {
		session.me = { user: null, roles: { author: false, admin: false } };
	}
	session.loaded = true;
}

export async function logout() {
	await api.post('/auth/logout');
	await loadSession();
}

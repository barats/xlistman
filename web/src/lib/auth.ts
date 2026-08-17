import { writable } from 'svelte/store';
import { getMe, logout as apiLogout } from '$lib/api';
import type { Subscription } from '$lib/types';

export interface Me {
	email: string;
	subscriptions: Subscription[];
	is_administrator: boolean;
	has_list_role: boolean;
}

// undefined = still loading, null = signed out, Me = signed in.
export const me = writable<Me | null | undefined>(undefined);

export async function refreshMe(): Promise<Me | null> {
	try {
		const data = await getMe();
		me.set(data);
		return data;
	} catch {
		me.set(null);
		return null;
	}
}

export async function signOut(): Promise<void> {
	try {
		await apiLogout();
	} finally {
		me.set(null);
	}
}

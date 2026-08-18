import { writable } from 'svelte/store';
import { getWebStatus } from '$lib/api';
import type { WebStatus } from '$lib/types';

// undefined = still loading.
export const webStatus = writable<WebStatus | undefined>(undefined);

// refreshWebStatus loads the instance-wide web access control switches (ADR
// 0020). On failure it falls back to "everything enabled" so a broken status
// read never locks a user out of the UI.
export async function refreshWebStatus(): Promise<WebStatus | undefined> {
	try {
		const s = await getWebStatus();
		webStatus.set(s);
		return s;
	} catch {
		webStatus.set({ login_enabled: true, management_enabled: true });
		return undefined;
	}
}

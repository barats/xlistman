import type {
	ArchiveEntry,
	ArchiveMessage,
	ListInfo,
	ListSummary,
	Subscription
} from '$lib/types';

const jsonHeaders = { 'Content-Type': 'application/json' };

export class ApiError extends Error {
	status: number;
	constructor(status: number, message: string) {
		super(message);
		this.name = 'ApiError';
		this.status = status;
	}
}

async function throwOnError(res: Response): Promise<void> {
	if (res.ok) return;
	let message = `Request failed (${res.status})`;
	try {
		const body = await res.json();
		if (typeof body?.error === 'string') message = body.error;
	} catch {
		// keep the generic message
	}
	throw new ApiError(res.status, message);
}

export async function getLists(): Promise<ListSummary[]> {
	const res = await fetch('/api/lists');
	await throwOnError(res);
	return res.json();
}

export async function getList(domain: string, listName: string): Promise<ListInfo> {
	const res = await fetch(`/api/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}`);
	await throwOnError(res);
	return res.json();
}

export async function subscribe(domain: string, listName: string, email: string): Promise<void> {
	const res = await fetch(
		`/api/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/subscribe`,
		{ method: 'POST', headers: jsonHeaders, body: JSON.stringify({ email }) }
	);
	await throwOnError(res);
}

export async function requestMagicLink(email: string): Promise<void> {
	const res = await fetch('/api/auth/magic-link', {
		method: 'POST',
		headers: jsonHeaders,
		body: JSON.stringify({ email })
	});
	await throwOnError(res);
}

export async function logout(): Promise<void> {
	await fetch('/api/auth/logout', { method: 'POST' });
}

export async function getMe(): Promise<{ email: string; subscriptions: Subscription[] }> {
	const res = await fetch('/api/me');
	await throwOnError(res);
	return res.json();
}

export async function setDelivery(subscriptionId: number, mode: string): Promise<void> {
	const res = await fetch(`/api/me/subscriptions/${subscriptionId}/delivery`, {
		method: 'POST',
		headers: jsonHeaders,
		body: JSON.stringify({ mode })
	});
	await throwOnError(res);
}

export async function reEnable(subscriptionId: number): Promise<void> {
	const res = await fetch(`/api/me/subscriptions/${subscriptionId}/re-enable`, { method: 'POST' });
	await throwOnError(res);
}

export async function unsubscribeMe(subscriptionId: number): Promise<void> {
	const res = await fetch(`/api/me/subscriptions/${subscriptionId}/unsubscribe`, { method: 'POST' });
	await throwOnError(res);
}

export async function getArchives(
	domain: string,
	listName: string,
	opts: { q?: string; limit?: number; offset?: number } = {}
): Promise<ArchiveEntry[]> {
	const params = new URLSearchParams();
	if (opts.q) params.set('q', opts.q);
	if (opts.limit) params.set('limit', String(opts.limit));
	if (opts.offset) params.set('offset', String(opts.offset));
	const qs = params.toString();
	const res = await fetch(
		`/api/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/archives${qs ? `?${qs}` : ''}`
	);
	await throwOnError(res);
	return res.json();
}

export async function getArchiveEntry(
	domain: string,
	listName: string,
	id: number
): Promise<ArchiveMessage> {
	const res = await fetch(
		`/api/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/archives/${id}`
	);
	await throwOnError(res);
	return res.json();
}

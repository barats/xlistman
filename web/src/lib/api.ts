import type {
	AdminAdministrator,
	AdminDomain,
	AdminInfo,
	AdminList,
	ArchiveEntry,
	ArchiveMessage,
	AuditEvent,
	BounceMember,
	ConsoleList,
	ConsoleListInfo,
	ConsoleMember,
	ConsoleSettings,
	DesignatedSender,
	HeldMessage,
	HeldMessageDetail,
	HeldPost,
	ListInfo,
	ListSummary,
	Subscription,
	WebStatus
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

// getWebStatus reports the instance-wide web access control switches (ADR
// 0020) so the UI can show disabled notices and hide the consoles.
export async function getWebStatus(): Promise<WebStatus> {
	const res = await fetch('/api/web-status');
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

export async function getMe(): Promise<{
	email: string;
	subscriptions: Subscription[];
	is_administrator: boolean;
	has_list_role: boolean;
}> {
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

export async function getMyHeldPosts(): Promise<HeldPost[]> {
	const res = await fetch('/api/me/held-posts');
	await throwOnError(res);
	return res.json();
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

// --- Role console (ADR 0015) ---

export async function getConsoleLists(): Promise<ConsoleList[]> {
	const res = await fetch('/api/console/lists');
	await throwOnError(res);
	return res.json();
}

export async function getConsoleListInfo(
	domain: string,
	listName: string
): Promise<ConsoleListInfo> {
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}`
	);
	await throwOnError(res);
	return res.json();
}

export async function getHeldMessages(
	domain: string,
	listName: string
): Promise<HeldMessage[]> {
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/held`
	);
	await throwOnError(res);
	return res.json();
}

export async function getHeldMessage(
	domain: string,
	listName: string,
	id: number
): Promise<HeldMessageDetail> {
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/held/${id}`
	);
	await throwOnError(res);
	return res.json();
}

export async function moderate(
	domain: string,
	listName: string,
	id: number,
	action: 'approve' | 'reject' | 'discard'
): Promise<void> {
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/held/${id}/${action}`,
		{ method: 'POST' }
	);
	await throwOnError(res);
}

export async function getSenders(
	domain: string,
	listName: string
): Promise<DesignatedSender[]> {
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/senders`
	);
	await throwOnError(res);
	return res.json();
}

export async function addSender(
	domain: string,
	listName: string,
	email: string
): Promise<void> {
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/senders`,
		{ method: 'POST', headers: jsonHeaders, body: JSON.stringify({ email }) }
	);
	await throwOnError(res);
}

export async function removeSender(
	domain: string,
	listName: string,
	subscriberId: number
): Promise<void> {
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/senders/${subscriberId}`,
		{ method: 'DELETE' }
	);
	await throwOnError(res);
}

// --- Admin console (ADR 0016) ---

export async function getConsoleSettings(
	domain: string,
	listName: string
): Promise<ConsoleSettings> {
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/settings`
	);
	await throwOnError(res);
	return res.json();
}

export async function updateConsoleSettings(
	domain: string,
	listName: string,
	body: ConsoleSettings
): Promise<void> {
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/settings`,
		{ method: 'PUT', headers: jsonHeaders, body: JSON.stringify(body) }
	);
	await throwOnError(res);
}

export async function getConsoleMembers(
	domain: string,
	listName: string
): Promise<ConsoleMember[]> {
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/members`
	);
	await throwOnError(res);
	return res.json();
}

export async function addMember(
	domain: string,
	listName: string,
	email: string
): Promise<void> {
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/members`,
		{ method: 'POST', headers: jsonHeaders, body: JSON.stringify({ email }) }
	);
	await throwOnError(res);
}

// --- Member import/export (Phase 14) ---

export interface MemberImportResult {
	status: string;
	added: number;
	skipped: number;
	already: number;
	disabled: number;
	invalid: number;
}

// exportMembers downloads the list's members as a CSV file.
export async function exportMembers(domain: string, listName: string): Promise<Blob> {
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/members/export`
	);
	await throwOnError(res);
	return res.blob();
}

// importMembers uploads a CSV file of member emails and adds them as Active
// members (authoritative add).
export async function importMembers(
	domain: string,
	listName: string,
	file: File
): Promise<MemberImportResult> {
	const form = new FormData();
	form.append('file', file);
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/members/import`,
		{ method: 'POST', body: form }
	);
	await throwOnError(res);
	return res.json();
}

export async function removeMember(
	domain: string,
	listName: string,
	subscriberId: number
): Promise<void> {
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/members/${subscriberId}`,
		{ method: 'DELETE' }
	);
	await throwOnError(res);
}

export async function approveSubscription(
	domain: string,
	listName: string,
	subscriberId: number
): Promise<void> {
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/members/${subscriberId}/approve`,
		{ method: 'POST' }
	);
	await throwOnError(res);
}

export async function rejectSubscription(
	domain: string,
	listName: string,
	subscriberId: number
): Promise<void> {
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/members/${subscriberId}/reject`,
		{ method: 'POST' }
	);
	await throwOnError(res);
}

export async function grantRole(
	domain: string,
	listName: string,
	subscriberId: number,
	role: string
): Promise<void> {
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/roles/${subscriberId}/${role}`,
		{ method: 'POST' }
	);
	await throwOnError(res);
}

export async function revokeRole(
	domain: string,
	listName: string,
	subscriberId: number,
	role: string
): Promise<void> {
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/roles/${subscriberId}/${role}`,
		{ method: 'DELETE' }
	);
	await throwOnError(res);
}

// --- Bounce management (ADR 0019) ---

export async function getBounces(
	domain: string,
	listName: string
): Promise<BounceMember[]> {
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/bounces`
	);
	await throwOnError(res);
	return res.json();
}

export async function reenableBounceMember(
	domain: string,
	listName: string,
	subscriberId: number
): Promise<void> {
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/bounces/${subscriberId}/re-enable`,
		{ method: 'POST' }
	);
	await throwOnError(res);
}

export async function resetBounceCount(
	domain: string,
	listName: string,
	subscriberId: number
): Promise<void> {
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/bounces/${subscriberId}/reset`,
		{ method: 'POST' }
	);
	await throwOnError(res);
}

// --- Server administration (ADR 0017) ---

export async function getAdminInfo(): Promise<AdminInfo> {
	const res = await fetch('/api/console/admin/info');
	await throwOnError(res);
	return res.json();
}

export async function getAdminDomains(): Promise<AdminDomain[]> {
	const res = await fetch('/api/console/admin/domains');
	await throwOnError(res);
	return res.json();
}

export async function createAdminDomain(name: string, description: string): Promise<void> {
	const res = await fetch('/api/console/admin/domains', {
		method: 'POST',
		headers: jsonHeaders,
		body: JSON.stringify({ name, description })
	});
	await throwOnError(res);
}

export async function getAdminLists(): Promise<AdminList[]> {
	const res = await fetch('/api/console/admin/lists');
	await throwOnError(res);
	return res.json();
}

export interface CreateListBody {
	list_name: string;
	domain: string;
	list_type: string;
	description: string;
	first_owner_email: string;
	moderation: boolean;
}

export async function createAdminList(body: CreateListBody): Promise<{ address: string }> {
	const res = await fetch('/api/console/admin/lists', {
		method: 'POST',
		headers: jsonHeaders,
		body: JSON.stringify(body)
	});
	await throwOnError(res);
	return res.json();
}

export async function deleteAdminList(domain: string, listName: string): Promise<void> {
	const res = await fetch(
		`/api/console/admin/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}`,
		{ method: 'DELETE' }
	);
	await throwOnError(res);
}

export async function changeAdminListType(domain: string, listName: string, listType: string): Promise<void> {
	const res = await fetch(
		`/api/console/admin/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/type`,
		{ method: 'POST', headers: jsonHeaders, body: JSON.stringify({ list_type: listType }) }
	);
	await throwOnError(res);
}

export async function getAdminAdministrators(): Promise<AdminAdministrator[]> {
	const res = await fetch('/api/console/admin/administrators');
	await throwOnError(res);
	return res.json();
}

export async function addAdminAdministrator(email: string): Promise<void> {
	const res = await fetch('/api/console/admin/administrators', {
		method: 'POST',
		headers: jsonHeaders,
		body: JSON.stringify({ email })
	});
	await throwOnError(res);
}

export async function removeAdminAdministrator(subscriberId: number): Promise<void> {
	const res = await fetch(`/api/console/admin/administrators/${subscriberId}`, { method: 'DELETE' });
	await throwOnError(res);
}

// --- Audit trail (ADR 0018) ---

export async function getAuditEvents(
	domain: string,
	listName: string,
	action?: string
): Promise<AuditEvent[]> {
	const qs = action ? `?action=${encodeURIComponent(action)}` : '';
	const res = await fetch(
		`/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/audit${qs}`
	);
	await throwOnError(res);
	return res.json();
}

export async function getAdminAuditEvents(action?: string): Promise<AuditEvent[]> {
	const qs = action ? `?action=${encodeURIComponent(action)}` : '';
	const res = await fetch(`/api/console/admin/audit${qs}`);
	await throwOnError(res);
	return res.json();
}

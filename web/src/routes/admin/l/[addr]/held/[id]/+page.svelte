<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { ApiError, getHeldMessage, moderate } from '$lib/api';
	import type { HeldMessageDetail } from '$lib/types';
	import { fmtDate } from '$lib/dates';
	import { getSiteName, setSeo } from '$lib/seo';
	import { Button, buttonVariants } from '$lib/components/ui/button';
	import { Card } from '$lib/components/ui/card';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import MessageBody from '$lib/components/message-body.svelte';

	const site = getSiteName();
	const addr = page.params.addr ?? '';
	const id = Number(page.params.id ?? '');
	const at = addr.indexOf('@');
	const listName = addr.slice(0, at);
	const domain = addr.slice(at + 1);

	setSeo({
		title: `${addr} — ${site}`,
		description: `Review a held message on ${addr}.`,
		noindex: true
	});

	let msg = $state<HeldMessageDetail | null>(null);
	let phase: 'loading' | 'loaded' | 'denied' | 'error' = $state('loading');
	let error = $state('');
	let working = $state(false);
	let done = $state('');

	onMount(async () => {
		try {
			msg = await getHeldMessage(domain, listName, id);
			phase = 'loaded';
			setSeo({
				title: `${msg?.subject || '(no subject)'} — ${addr} — ${site}`,
				description: `Review a held message on ${addr}.`,
				noindex: true
			});
		} catch (e) {
			if (e instanceof ApiError && (e.status === 401 || e.status === 403)) {
				phase = 'denied';
			} else {
				phase = 'error';
				error = e instanceof Error ? e.message : 'Could not load the message.';
			}
		}
	});

	async function act(action: 'approve' | 'reject' | 'discard') {
		working = true;
		error = '';
		try {
			await moderate(domain, listName, id, action);
			done = action === 'approve' ? 'approved' : action === 'reject' ? 'rejected' : 'discarded';
		} catch (e) {
			error = e instanceof Error ? e.message : 'Action failed.';
		} finally {
			working = false;
		}
	}

	const attachmentPrefix = `/api/console/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/held/${id}/attachments`;
</script>

{#if phase === 'loading'}
	<div class="space-y-3">
		<Skeleton class="h-6 w-2/3" />
		<Skeleton class="h-4 w-1/3" />
		<Skeleton class="h-40 w-full" />
	</div>
{:else if phase === 'denied'}
	<Card class="p-6">
		<h2 class="text-lg font-semibold">Not authorized</h2>
		<p class="mt-1 text-sm text-muted-foreground">
			You need to be an owner or moderator of {addr} to act on this message.
		</p>
	</Card>
{:else if phase === 'error'}
	<p class="text-sm text-destructive">{error}</p>
{:else if done}
	<div class="mx-auto max-w-md text-center">
		<h1 class="text-2xl font-bold tracking-tight">Message {done}</h1>
		<p class="mt-2 text-sm text-muted-foreground">
			{msg?.subject || '(no subject)'} from {msg?.sender}.
		</p>
		<div class="mt-4 flex justify-center gap-2">
			<a href={`/admin/l/${addr}`} class={buttonVariants()}>Back to held messages</a>
		</div>
	</div>
{:else}
	<a href={`/admin/l/${addr}`} class="text-sm text-muted-foreground hover:text-foreground">
		&larr; Back to held messages
	</a>
	<div class="mt-3 border-b pb-4">
		<h1 class="text-2xl font-bold tracking-tight">{msg?.subject || '(no subject)'}</h1>
		<p class="mt-2 text-sm text-muted-foreground">
			<span class="font-medium text-foreground">{msg?.sender}</span> · received {msg ? fmtDate(msg.received_at) : ''}
		</p>
	</div>
	{#if msg}
		<MessageBody msg={msg.body} downloadPrefix={attachmentPrefix} />
	{/if}

	{#if error}
		<p class="mt-4 text-sm text-destructive">{error}</p>
	{/if}
	<div class="mt-6 flex flex-wrap gap-2">
		<Button disabled={working} onclick={() => act('approve')}>Approve</Button>
		<Button variant="outline" disabled={working} onclick={() => act('reject')}>Reject</Button>
		<Button variant="ghost" disabled={working} onclick={() => act('discard')}>Discard</Button>
	</div>
{/if}

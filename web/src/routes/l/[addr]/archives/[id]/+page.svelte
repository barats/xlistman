<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { ApiError, getArchiveEntry } from '$lib/api';
	import type { ArchiveMessage } from '$lib/types';
	import { fmtDate } from '$lib/dates';
	import { getSiteName, setSeo } from '$lib/seo';
	import { buttonVariants } from '$lib/components/ui/button';
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
		title: `Archives — ${addr} — ${site}`,
		description: `A post in the ${addr} archive.`,
		noindex: true
	});

	let msg = $state<ArchiveMessage | null>(null);
	let phase: 'loading' | 'loaded' | 'denied' | 'error' = $state('loading');
	let deniedStatus = $state(0);
	let error = $state('');

	onMount(async () => {
		try {
			msg = await getArchiveEntry(domain, listName, id);
			phase = 'loaded';
			setSeo({
				title: `${msg?.subject || '(no subject)'} — ${addr} — ${site}`,
				description: `A post in the ${addr} archive.`,
				noindex: true
			});
		} catch (e) {
			if (e instanceof ApiError && (e.status === 401 || e.status === 403)) {
				phase = 'denied';
				deniedStatus = e.status;
			} else {
				phase = 'error';
				error = e instanceof Error ? e.message : 'Could not load the message.';
			}
		}
	});

	const attachmentPrefix = `/api/lists/${encodeURIComponent(domain)}/${encodeURIComponent(listName)}/archives/${id}/attachments`;
</script>

{#if phase === 'loading'}
	<div class="space-y-3">
		<Skeleton class="h-6 w-2/3" />
		<Skeleton class="h-4 w-1/3" />
		<Skeleton class="h-40 w-full" />
	</div>
{:else if phase === 'denied'}
	<Card class="p-6">
		<h2 class="text-lg font-semibold">Members only</h2>
		{#if deniedStatus === 403}
			<p class="mt-1 text-sm text-muted-foreground">
				Only subscribers of {addr} can browse its archives.
			</p>
			<div class="mt-4">
				<a href={`/l/${addr}`} class={buttonVariants()}>Subscribe</a>
			</div>
		{:else}
			<p class="mt-1 text-sm text-muted-foreground">
				Archives are available to subscribers of {addr}. Sign in to continue.
			</p>
			<div class="mt-4">
				<a href="/auth" class="text-sm font-medium text-primary underline-offset-4 hover:underline"
					>Sign in</a
				>
			</div>
		{/if}
	</Card>
{:else if phase === 'error'}
	<p class="text-sm text-destructive">{error}</p>
{:else}
	<a href={`/l/${addr}/archives`} class="text-sm text-muted-foreground hover:text-foreground">
		&larr; Back to archives
	</a>
	<div class="mt-3 border-b pb-4">
		<h1 class="text-2xl font-bold tracking-tight">{msg?.subject || '(no subject)'}</h1>
		<p class="mt-2 text-sm text-muted-foreground">
			<span class="font-medium text-foreground">{msg?.from}</span> · {msg ? fmtDate(msg.received_at) : ''}
		</p>
	</div>
	{#if msg}
		<MessageBody msg={msg.body} downloadPrefix={attachmentPrefix} />
	{/if}
{/if}

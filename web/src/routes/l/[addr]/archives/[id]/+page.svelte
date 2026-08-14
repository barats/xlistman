<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { ApiError, getArchiveEntry } from '$lib/api';
	import type { ArchiveMessage } from '$lib/types';
	import { Card } from '$lib/components/ui/card';
	import { Skeleton } from '$lib/components/ui/skeleton';

	const addr = page.params.addr ?? '';
	const id = Number(page.params.id ?? '');
	const at = addr.indexOf('@');
	const listName = addr.slice(0, at);
	const domain = addr.slice(at + 1);

	let msg = $state<ArchiveMessage | null>(null);
	let phase: 'loading' | 'loaded' | 'denied' | 'error' = $state('loading');
	let error = $state('');

	onMount(async () => {
		try {
			msg = await getArchiveEntry(domain, listName, id);
			phase = 'loaded';
		} catch (e) {
			if (e instanceof ApiError && (e.status === 401 || e.status === 403)) {
				phase = 'denied';
			} else {
				phase = 'error';
				error = e instanceof Error ? e.message : 'Could not load the message.';
			}
		}
	});

	function fmtDate(iso: string): string {
		return new Date(iso).toLocaleString();
	}

	function bodyText(): string {
		const b = msg?.body ?? '';
		const sep = b.indexOf('\r\n\r\n');
		if (sep >= 0) return b.slice(sep + 4);
		const sep2 = b.indexOf('\n\n');
		if (sep2 >= 0) return b.slice(sep2 + 2);
		return b;
	}
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
		<p class="mt-1 text-sm text-muted-foreground">
			Archives are available to subscribers of {addr}. Sign in to continue.
		</p>
		<div class="mt-4">
			<a href="/auth" class="text-sm font-medium text-primary underline-offset-4 hover:underline"
				>Sign in</a
			>
		</div>
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
	<pre class="mt-4 whitespace-pre-wrap break-words font-sans text-sm leading-relaxed">{bodyText()}</pre>
{/if}

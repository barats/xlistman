<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { getHeldMessages, moderate } from '$lib/api';
	import type { HeldMessage } from '$lib/types';
	import { fmtRelative } from '$lib/dates';
	import { Button } from '$lib/components/ui/button';
	import { Skeleton } from '$lib/components/ui/skeleton';

	const addr = page.params.addr ?? '';
	const at = addr.indexOf('@');
	const listName = addr.slice(0, at);
	const domain = addr.slice(at + 1);

	let held = $state<HeldMessage[] | null>(null);
	let error = $state('');

	async function load() {
		try {
			held = await getHeldMessages(domain, listName);
			error = '';
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not load held messages.';
		}
	}

	onMount(load);

	let busyHeldId = $state<number | null>(null);
	let heldError = $state('');

	async function act(id: number, action: 'approve' | 'reject' | 'discard') {
		busyHeldId = id;
		heldError = '';
		try {
			await moderate(domain, listName, id, action);
			await load();
		} catch (e) {
			heldError = e instanceof Error ? e.message : 'Action failed.';
		} finally {
			busyHeldId = null;
		}
	}
</script>

{#if heldError}
	<p class="text-sm text-destructive">{heldError}</p>
{/if}

<h2 class="text-lg font-semibold">Held messages</h2>
{#if held === null}
	<div class="mt-3 space-y-3">
		<Skeleton class="h-16 w-full" />
	</div>
{:else if held.length === 0}
	<p class="mt-2 text-sm text-muted-foreground">No messages awaiting moderation.</p>
{:else}
	<div class="mt-3 divide-y rounded-lg border">
		{#each held as m (m.id)}
			<div class="flex items-center justify-between gap-3 px-4 py-3">
				<a href={`/admin/l/${addr}/held/${m.id}`} class="block min-w-0">
					<p class="truncate font-medium">{m.subject || '(no subject)'}</p>
					<p class="mt-0.5 truncate text-sm text-muted-foreground">
						{m.sender} &middot; {fmtRelative(m.received_at)}
					</p>
				</a>
				<div class="flex shrink-0 gap-2">
					<Button size="sm" disabled={busyHeldId === m.id} onclick={() => act(m.id, 'approve')}>
						Approve
					</Button>
					<Button
						variant="outline"
						size="sm"
						disabled={busyHeldId === m.id}
						onclick={() => act(m.id, 'reject')}
					>
						Reject
					</Button>
					<Button
						variant="ghost"
						size="sm"
						disabled={busyHeldId === m.id}
						onclick={() => act(m.id, 'discard')}
					>
						Discard
					</Button>
				</div>
			</div>
		{/each}
	</div>
{/if}

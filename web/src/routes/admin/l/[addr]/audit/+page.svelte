<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { getAuditEvents } from '$lib/api';
	import type { AuditEvent } from '$lib/types';
	import { auditActions } from '$lib/audit';
	import AuditEvents from '$lib/components/audit-events.svelte';
	import { Skeleton } from '$lib/components/ui/skeleton';

	const addr = page.params.addr ?? '';
	const at = addr.indexOf('@');
	const listName = addr.slice(0, at);
	const domain = addr.slice(at + 1);

	let events = $state<AuditEvent[] | null>(null);
	let error = $state('');
	let filter = $state('');

	async function load() {
		try {
			events = await getAuditEvents(domain, listName, filter || undefined);
			error = '';
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not load audit events.';
		}
	}

	onMount(load);
</script>

<h2 class="text-lg font-semibold">Audit trail</h2>
<p class="mt-1 text-sm text-muted-foreground">
	Every privileged action on this list, newest first. Events are never deleted.
</p>

<div class="mt-3 max-w-xs">
	<select
		bind:value={filter}
		onchange={load}
		class="w-full rounded-md border bg-background px-3 py-2 text-sm"
	>
		<option value="">All actions</option>
		{#each auditActions as a (a)}
			<option value={a}>{a}</option>
		{/each}
	</select>
</div>

{#if error}
	<p class="mt-3 text-sm text-destructive">{error}</p>
{/if}
{#if events === null}
	<div class="mt-3 space-y-3">
		<Skeleton class="h-10 w-full" />
	</div>
{:else}
	<div class="mt-3">
		<AuditEvents events={events} />
	</div>
{/if}

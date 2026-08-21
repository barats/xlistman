<script lang="ts">
	import { onMount } from 'svelte';
	import { getAdminAuditEvents } from '$lib/api';
	import type { AuditEventPage } from '$lib/types';
	import { auditActions } from '$lib/audit';
	import { getSiteName, setSeo } from '$lib/seo';
	import AuditEvents from '$lib/components/audit-events.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Select } from '$lib/components/ui/select';
	import { Skeleton } from '$lib/components/ui/skeleton';

	const site = getSiteName();
	setSeo({
		title: `Server audit — ${site}`,
		description: 'Review the instance-wide audit trail.',
		noindex: true
	});

	const pageSize = 500;

	let data = $state<AuditEventPage | null>(null);
	let error = $state('');
	let filter = $state('');
	let offset = $state(0);

	async function load() {
		try {
			data = await getAdminAuditEvents({ action: filter || undefined, limit: pageSize, offset });
			error = '';
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not load audit events.';
		}
	}

	onMount(load);

	function changeFilter() {
		offset = 0;
		load();
	}

	const canPrev = $derived(offset > 0);
	const canNext = $derived(data !== null && offset + pageSize < (data?.total ?? 0));
	const showingFrom = $derived(data ? offset + 1 : 0);
	const showingTo = $derived(data ? Math.min(offset + pageSize, data.total) : 0);

	function prevPage() {
		if (!canPrev) return;
		offset = Math.max(0, offset - pageSize);
		load();
	}

	function nextPage() {
		if (!canNext) return;
		offset += pageSize;
		load();
	}
</script>

<h2 class="text-lg font-semibold">Audit trail</h2>
<p class="mt-1 text-sm text-muted-foreground">
	Every privileged action across the instance, newest first, including on
	deleted lists. Events are never deleted.
</p>

<div class="mt-3 max-w-xs">
	<Select bind:value={filter} onchange={changeFilter} aria-label="Filter by action">
		<option value="">All actions</option>
		{#each auditActions as a (a)}
			<option value={a}>{a}</option>
		{/each}
	</Select>
</div>
<p class="mt-2 text-xs text-muted-foreground">
	This view shows the most recent 500 events. Older history is available via the CLI:
	<code class="rounded bg-muted px-1 py-0.5 font-mono">xlistman audit server [action]</code>
</p>

{#if error}
	<p class="mt-3 text-sm text-destructive">{error}</p>
{/if}
{#if data === null}
	<div class="mt-3 space-y-3">
		<Skeleton class="h-10 w-full" />
	</div>
{:else}
	<div class="mt-3">
		<AuditEvents events={data.events} showList />
	</div>
	<div class="mt-3 flex flex-wrap items-center justify-between gap-2 text-sm text-muted-foreground">
		<p>
			Showing {showingFrom}–{showingTo} of {data.total}
		</p>
		<div class="flex gap-2">
			<Button variant="outline" size="sm" disabled={!canPrev} onclick={prevPage}>
				&larr; Prev
			</Button>
			<Button variant="outline" size="sm" disabled={!canNext} onclick={nextPage}>
				Next &rarr;
			</Button>
		</div>
	</div>
{/if}

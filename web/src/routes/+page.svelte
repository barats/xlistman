<script lang="ts">
	import { onMount } from 'svelte';
	import { getLists } from '$lib/api';
	import type { ListSummary } from '$lib/types';
	import { getSiteName, setSeo } from '$lib/seo';
	import { Badge } from '$lib/components/ui/badge';
	import { Card } from '$lib/components/ui/card';
	import { Skeleton } from '$lib/components/ui/skeleton';

	const site = getSiteName();

	let lists = $state<ListSummary[] | null>(null);
	let error = $state('');

	onMount(async () => {
		setSeo({
			title: `Mailing lists — ${site}`,
			description: `Browse the mailing lists hosted on this ${site} instance and subscribe with one email address.`
		});
		try {
			lists = await getLists();
		} catch {
			error = 'Could not load the list of mailing lists.';
		}
	});
</script>

<h1 class="text-3xl font-bold tracking-tight">Mailing lists</h1>
<p class="mt-1 text-muted-foreground">
	A self-hosted, one-binary mailing list manager.
</p>

{#if error}
	<p class="mt-4 text-sm text-destructive">{error}</p>
{/if}

<div class="mt-6 grid gap-3">
	{#if lists === null}
		{#each Array(3) as _}
			<Skeleton class="h-20 w-full" />
		{/each}
	{:else if lists.length === 0}
		<Card class="p-6 text-sm text-muted-foreground">No lists are hosted here yet.</Card>
	{:else}
		{#each lists as l (l.address)}
			<a href={`/l/${l.address}`} class="group block rounded-lg">
				<Card class="p-4 transition-colors hover:bg-accent/40">
					<div class="flex items-center justify-between gap-4">
						<div class="min-w-0">
							<p class="truncate font-mono font-semibold">{l.address}</p>
							<p class="mt-0.5 truncate text-sm text-muted-foreground">
								{l.description || 'No description'}
							</p>
						</div>
						<div class="flex shrink-0 items-center gap-3">
							<Badge variant="secondary" class="shrink-0">{l.list_type}</Badge>
							<span
								class="text-muted-foreground transition-colors group-hover:text-foreground"
								aria-hidden="true"
								>&rarr;</span
							>
						</div>
					</div>
				</Card>
			</a>
		{/each}
	{/if}
</div>

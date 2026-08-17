<script lang="ts">
	import { onMount } from 'svelte';
	import { createAdminDomain, getAdminDomains } from '$lib/api';
	import type { AdminDomain } from '$lib/types';
	import { Button } from '$lib/components/ui/button';
	import { Card } from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Skeleton } from '$lib/components/ui/skeleton';

	let domains = $state<AdminDomain[] | null>(null);
	let error = $state('');
	let busy = $state(false);

	let name = $state('');
	let description = $state('');

	onMount(async () => {
		try {
			domains = await getAdminDomains();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not load domains.';
		}
	});

	async function addDomain() {
		busy = true;
		error = '';
		try {
			await createAdminDomain(name, description);
			name = '';
			description = '';
			domains = await getAdminDomains();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not create domain.';
		} finally {
			busy = false;
		}
	}
</script>

{#if !domains && !error}
	<div class="space-y-3">
		<Skeleton class="h-6 w-1/2" />
		<Skeleton class="h-24 w-full" />
	</div>
{:else}
	<Card class="p-6">
		<h2 class="text-lg font-semibold">Add a domain</h2>
		<p class="mt-1 text-sm text-muted-foreground">
			A virtual email domain hosted by this instance (e.g. <code>lists.example.org</code>). Point
			your MTA at this instance for addresses on the domain.
		</p>
		<form
			class="mt-4 flex flex-col gap-3 sm:flex-row sm:items-end"
			onsubmit={(e) => { e.preventDefault(); addDomain(); }}
		>
			<div class="flex-1 space-y-1.5">
				<Label for="domain-name">Domain</Label>
				<Input id="domain-name" bind:value={name} placeholder="lists.example.org" />
			</div>
			<div class="flex-1 space-y-1.5">
				<Label for="domain-desc">Description</Label>
				<Input id="domain-desc" bind:value={description} placeholder="What is this domain for?" />
			</div>
			<Button type="submit" disabled={busy}>Add domain</Button>
		</form>
		{#if error}
			<p class="mt-3 text-sm text-destructive">{error}</p>
		{/if}
	</Card>

	<div class="mt-6 space-y-3">
		{#if domains && domains.length === 0}
			<Card class="p-6 text-sm text-muted-foreground">
				No domains yet. Add one above to start hosting lists.
			</Card>
		{:else}
			{#each domains ?? [] as d (d.id)}
				<Card class="p-4">
					<div class="flex items-center justify-between gap-4">
						<div class="min-w-0">
							<p class="truncate font-semibold">{d.name}</p>
							<p class="truncate text-sm text-muted-foreground">
								{d.description || 'No description'}
							</p>
						</div>
						<p class="shrink-0 text-sm text-muted-foreground">
							{d.list_count === 1 ? '1 list' : `${d.list_count} lists`}
						</p>
					</div>
				</Card>
			{/each}
		{/if}
	</div>
{/if}

<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { getConsoleListInfo, getConsoleMembers, getHeldMessages } from '$lib/api';
	import type { ConsoleListInfo, HeldMessage, MemberPage } from '$lib/types';
	import { getSiteName, setSeo } from '$lib/seo';
	import { Card } from '$lib/components/ui/card';
	import { Skeleton } from '$lib/components/ui/skeleton';

	const site = getSiteName();
	const addr = page.params.addr ?? '';
	const at = addr.indexOf('@');
	const listName = addr.slice(0, at);
	const domain = addr.slice(at + 1);

	setSeo({
		title: `${addr} — ${site}`,
		description: `List console for ${addr}.`,
		noindex: true
	});

	let info = $state<ConsoleListInfo | null>(null);
	let memberPage = $state<MemberPage | null>(null);
	let held = $state<HeldMessage[] | null>(null);
	let error = $state('');

	onMount(async () => {
		try {
			info = await getConsoleListInfo(domain, listName);
			held = await getHeldMessages(domain, listName);
			// Member statistics are owner-only; moderators see a placeholder.
			if (info.roles.includes('owner')) {
				memberPage = await getConsoleMembers(domain, listName, { limit: 1 });
			}
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not load the overview.';
		}
	});

	const memberCount = $derived(memberPage ? memberPage.total + memberPage.held.length : null);
	const pendingCount = $derived(memberPage ? memberPage.held.length : null);
</script>

{#if error}
	<p class="text-sm text-destructive">{error}</p>
{:else if !info || !held}
	<div class="space-y-3">
		<Skeleton class="h-6 w-1/2" />
		<Skeleton class="h-24 w-full" />
	</div>
{:else}
	<div class="grid gap-3 sm:grid-cols-3">
		<Card class="p-4">
			<p class="text-sm text-muted-foreground">Members</p>
			<p class="mt-1 text-2xl font-semibold">{memberCount ?? '—'}</p>
		</Card>
		<Card class="p-4">
			<p class="text-sm text-muted-foreground">Held messages</p>
			<p class="mt-1 text-2xl font-semibold">{held.length}</p>
		</Card>
		<Card class="p-4">
			<p class="text-sm text-muted-foreground">Awaiting approval</p>
			<p class="mt-1 text-2xl font-semibold">{pendingCount ?? '—'}</p>
		</Card>
	</div>

	<Card class="mt-6 p-6">
		<h2 class="text-lg font-semibold">About this list</h2>
		<p class="mt-1 text-sm text-muted-foreground">
			{info.description || 'No description yet. Owners can edit it in Settings.'}
		</p>
		{#if info.instructions}
			<p class="mt-3 whitespace-pre-wrap text-sm leading-relaxed">{info.instructions}</p>
		{/if}
	</Card>
{/if}

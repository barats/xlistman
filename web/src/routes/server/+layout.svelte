<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { ApiError, getAdminInfo } from '$lib/api';
	import type { AdminInfo } from '$lib/types';
	import { Card } from '$lib/components/ui/card';
	import { Skeleton } from '$lib/components/ui/skeleton';

	let { children } = $props();

	let info = $state<AdminInfo | null>(null);
	let phase: 'loading' | 'loaded' | 'denied' | 'error' = $state('loading');
	let error = $state('');

	onMount(async () => {
		try {
			info = await getAdminInfo();
			if (!info.is_administrator) {
				phase = 'denied';
			} else {
				phase = 'loaded';
			}
		} catch (e) {
			if (e instanceof ApiError && (e.status === 401 || e.status === 403)) {
				phase = 'denied';
			} else {
				phase = 'error';
				error = e instanceof Error ? e.message : 'Could not load the console.';
			}
		}
	});

	const tabs = [
		{ href: '/server', label: 'Overview' },
		{ href: '/server/domains', label: 'Domains' },
		{ href: '/server/lists', label: 'Lists' },
		{ href: '/server/administrators', label: 'Administrators' },
		{ href: '/server/audit', label: 'Audit' }
	];

	function isActive(href: string): boolean {
		if (href === '/server') return page.url.pathname === href;
		return page.url.pathname.startsWith(href);
	}
</script>

<div>
	<div>
		<h1 class="text-2xl font-bold tracking-tight">Server administration</h1>
		<p class="mt-1 text-muted-foreground">
			Create domains and lists, manage other Administrators, delete lists, and change list types
			instance-wide.
		</p>
	</div>

	{#if phase === 'denied'}
		<Card class="mt-8 p-6">
			<h2 class="text-lg font-semibold">Administrator required</h2>
			<p class="mt-1 text-sm text-muted-foreground">
				Only an Administrator can manage the server. Ask an Administrator to designate you, or
				use <code class="rounded bg-muted px-1 py-0.5">xlistman admin add</code> on the server.
			</p>
		</Card>
	{:else if phase === 'loading'}
		<div class="mt-6 space-y-3">
			<Skeleton class="h-6 w-2/3" />
			<Skeleton class="h-24 w-full" />
		</div>
	{:else if phase === 'error'}
		<p class="mt-6 text-sm text-destructive">{error}</p>
	{:else}
		<nav class="mt-6 flex flex-wrap gap-1 overflow-x-auto border-b">
			{#each tabs as tab (tab.href)}
				<a
					href={tab.href}
					class={isActive(tab.href)
						? 'whitespace-nowrap border-b-2 border-primary px-3 py-2 text-sm font-medium text-foreground'
						: 'whitespace-nowrap border-b-2 border-transparent px-3 py-2 text-sm text-muted-foreground transition-colors hover:text-foreground'}
				>
					{tab.label}
				</a>
			{/each}
		</nav>
		<div class="mt-6">
			{@render children()}
		</div>
	{/if}
</div>

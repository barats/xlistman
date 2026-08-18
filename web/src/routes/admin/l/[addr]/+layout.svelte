<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { ApiError, getConsoleListInfo } from '$lib/api';
	import type { ConsoleListInfo } from '$lib/types';
	import { webStatus } from '$lib/access';
	import { Badge } from '$lib/components/ui/badge';
	import { Card } from '$lib/components/ui/card';
	import { Skeleton } from '$lib/components/ui/skeleton';

	let { children } = $props();

	const addr = page.params.addr ?? '';
	const at = addr.indexOf('@');
	const listName = addr.slice(0, at);
	const domain = addr.slice(at + 1);

	let info = $state<ConsoleListInfo | null>(null);
	let phase: 'loading' | 'loaded' | 'denied' | 'error' = $state('loading');
	let error = $state('');

	async function load() {
		phase = 'loading';
		error = '';
		try {
			info = await getConsoleListInfo(domain, listName);
			phase = 'loaded';
		} catch (e) {
			if (e instanceof ApiError && (e.status === 401 || e.status === 403)) {
				phase = 'denied';
			} else {
				phase = 'error';
				error = e instanceof Error ? e.message : 'Could not load this list.';
			}
		}
	}

	onMount(load);

	const isOwner = $derived(info?.roles.includes('owner') ?? false);

	const tabs = $derived.by(() => {
		const base = `/admin/l/${addr}`;
		const list: { href: string; label: string }[] = [{ href: base, label: 'Overview' }];
		if (isOwner) {
			list.push({ href: `${base}/settings`, label: 'Settings' });
			list.push({ href: `${base}/members`, label: 'Members' });
			list.push({ href: `${base}/bounces`, label: 'Bounces' });
			list.push({ href: `${base}/audit`, label: 'Audit' });
		}
		if (info?.list_type === 'newsletter' && isOwner) {
			list.push({ href: `${base}/allowlist`, label: 'Allowlist' });
		}
		list.push({ href: `${base}/moderation`, label: 'Moderation' });
		return list;
	});

	function isActive(href: string): boolean {
		if (href === `/admin/l/${addr}`) return page.url.pathname === href;
		return page.url.pathname.startsWith(href);
	}
</script>

<div>
	<div class="flex flex-wrap items-center justify-between gap-3">
		<div>
			<a href="/admin" class="text-sm text-muted-foreground hover:text-foreground">
				&larr; My lists
			</a>
			<h1 class="mt-1 text-2xl font-bold tracking-tight">{addr}</h1>
			{#if info}
				<div class="mt-1 flex items-center gap-2">
					<Badge variant="secondary" class="capitalize">{info.list_type}</Badge>
					{#each info.roles as role (role)}
						<Badge class="capitalize">{role}</Badge>
					{/each}
				</div>
			{/if}
		</div>
	</div>

	{#if $webStatus?.management_enabled === false}
		<Card class="mt-8 p-6">
			<h2 class="text-lg font-semibold">Web management is disabled</h2>
			<p class="mt-1 text-sm text-muted-foreground">
				The server operator has switched off web management. List consoles are unavailable.
			</p>
		</Card>
	{:else if phase === 'denied'}
		<Card class="mt-8 p-6">
			<h2 class="text-lg font-semibold">Not authorized</h2>
			<p class="mt-1 text-sm text-muted-foreground">
				You need to be an owner or moderator of {addr} to use the console for it.
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

<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { ChevronDown } from '@lucide/svelte';
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

	// Tab order (Phase 15 follow-up): Overview, Members, Settings, Bounces,
	// [Allowlist on newsletters], Moderation, Audit.
	const tabs = $derived.by(() => {
		const base = `/admin/l/${addr}`;
		const list: { href: string; label: string }[] = [{ href: base, label: 'Overview' }];
		if (isOwner) {
			list.push({ href: `${base}/members`, label: 'Members' });
			list.push({ href: `${base}/settings`, label: 'Settings' });
			list.push({ href: `${base}/bounces`, label: 'Bounces' });
		}
		if (info?.list_type === 'newsletter' && isOwner) {
			list.push({ href: `${base}/allowlist`, label: 'Allowlist' });
		}
		list.push({ href: `${base}/moderation`, label: 'Moderation' });
		if (isOwner) {
			list.push({ href: `${base}/audit`, label: 'Audit' });
		}
		return list;
	});

	function isActive(href: string): boolean {
		if (href === `/admin/l/${addr}`) return page.url.pathname === href;
		return page.url.pathname.startsWith(href);
	}

	// On small screens the tab bar collapses to the active tab plus a More
	// overflow menu; the full row shows from md up.
	let moreOpen = $state(false);
	const activeTab = $derived(tabs.find((t) => isActive(t.href)) ?? tabs[0]);
	function closeMore() {
		moreOpen = false;
	}
</script>

<div>
	<div class="flex flex-wrap items-center justify-between gap-3">
		<div>
			<a href="/admin" class="text-sm text-muted-foreground hover:text-foreground">
				&larr; My lists
			</a>
			<h1 class="mt-1 font-mono text-2xl font-bold tracking-tight">{addr}</h1>
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
		<div class="relative mt-6 md:hidden">
			<nav class="flex items-center justify-between gap-1 border-b">
				<a
					href={activeTab.href}
					aria-current="page"
					class="whitespace-nowrap border-b-2 border-primary px-3 py-2 text-sm font-medium text-foreground"
				>
					{activeTab.label}
				</a>
				<button
					type="button"
					aria-expanded={moreOpen}
					class="inline-flex items-center gap-1 px-3 py-2 text-sm text-muted-foreground transition-colors hover:text-foreground"
					onclick={() => (moreOpen = !moreOpen)}
				>
					More <ChevronDown class="size-4" />
				</button>
			</nav>
			{#if moreOpen}
				<button
					type="button"
					class="fixed inset-0 z-10 cursor-default"
					aria-hidden="true"
					tabindex="-1"
					onclick={closeMore}
				></button>
				<div class="absolute right-0 z-20 mt-1 w-56 rounded-md border bg-popover p-1 text-popover-foreground shadow-md">
					{#each tabs as tab (tab.href)}
						<a
							href={tab.href}
							onclick={closeMore}
							class={isActive(tab.href)
								? 'block rounded-sm bg-muted px-3 py-2 text-sm font-medium'
								: 'block rounded-sm px-3 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground'}
						>
							{tab.label}
						</a>
					{/each}
				</div>
			{/if}
		</div>
		<nav class="mt-6 hidden gap-1 overflow-x-auto border-b md:flex">
			{#each tabs as tab (tab.href)}
				<a
					href={tab.href}
					aria-current={isActive(tab.href) ? 'page' : undefined}
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

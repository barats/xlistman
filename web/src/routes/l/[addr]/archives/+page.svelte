<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { ApiError, getArchives } from '$lib/api';
	import type { ArchiveEntry } from '$lib/types';
	import { fmtRelative } from '$lib/dates';
	import { getSiteName, setSeo } from '$lib/seo';
	import { Button, buttonVariants } from '$lib/components/ui/button';
	import { Card } from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input';
	import { Skeleton } from '$lib/components/ui/skeleton';

	const site = getSiteName();
	const addr = page.params.addr ?? '';
	const at = addr.indexOf('@');
	const listName = addr.slice(0, at);
	const domain = addr.slice(at + 1);

	setSeo({
		title: `Archives — ${addr} — ${site}`,
		description: `Browse the members-only archive of ${addr}.`,
		noindex: true
	});

	let entries = $state<ArchiveEntry[] | null>(null);
	let phase: 'loading' | 'loaded' | 'denied' | 'error' = $state('loading');
	let deniedStatus = $state(0);
	let error = $state('');
	let q = $state('');

	async function load() {
		phase = 'loading';
		entries = null;
		try {
			entries = await getArchives(domain, listName, { q: q.trim() || undefined, limit: 100 });
			phase = 'loaded';
		} catch (e) {
			if (e instanceof ApiError && (e.status === 401 || e.status === 403)) {
				phase = 'denied';
				deniedStatus = e.status;
			} else {
				phase = 'error';
				error = e instanceof Error ? e.message : 'Could not load the archive.';
			}
		}
	}

	onMount(load);
</script>

<div class="flex flex-wrap items-center justify-between gap-3">
	<div>
		<a href={`/l/${addr}`} class="text-sm text-muted-foreground hover:text-foreground">
			&larr; {addr}
		</a>
		<h1 class="mt-1 text-2xl font-bold tracking-tight">Archives</h1>
	</div>
	<form
		class="flex gap-2"
		onsubmit={(e) => {
			e.preventDefault();
			load();
		}}
	>
		<Input
			placeholder="Search…"
			aria-label="Search archives"
			bind:value={q}
			class="w-48 sm:w-64"
		/>
		<Button type="submit" variant="outline">Search</Button>
	</form>
</div>

{#if phase === 'denied'}
	<Card class="mt-6 p-6">
		<h2 class="text-lg font-semibold">Members only</h2>
		{#if deniedStatus === 403}
			<p class="mt-1 text-sm text-muted-foreground">
				Only subscribers of {addr} can browse its archives.
			</p>
			<div class="mt-4">
				<a href={`/l/${addr}`} class={buttonVariants()}>Subscribe</a>
			</div>
		{:else}
			<p class="mt-1 text-sm text-muted-foreground">
				Archives are available to subscribers of {addr}. Sign in to confirm you're a member.
			</p>
			<div class="mt-4">
				<a href="/auth" class={buttonVariants()}>Sign in</a>
			</div>
		{/if}
	</Card>
{:else if phase === 'loading'}
	<div class="mt-6 space-y-3">
		{#each Array(4) as _}
			<Skeleton class="h-16 w-full" />
		{/each}
	</div>
{:else if phase === 'error'}
	<p class="mt-6 text-sm text-destructive">{error}</p>
{:else}
	<div class="mt-6 divide-y rounded-lg border">
		{#if entries && entries.length === 0}
			<p class="p-6 text-sm text-muted-foreground">
				{q.trim() ? `No messages match “${q.trim()}”.` : 'No messages in the archive yet.'}
			</p>
		{:else}
			{#each entries ?? [] as e (e.id)}
				<a
					href={`/l/${addr}/archives/${e.id}`}
					class="block px-4 py-3 transition-colors hover:bg-accent/50"
				>
					<p class="truncate font-medium">{e.subject || '(no subject)'}</p>
					<p class="mt-0.5 truncate text-sm text-muted-foreground">
						{e.from} · {fmtRelative(e.received_at)}
					</p>
				</a>
			{/each}
		{/if}
	</div>
{/if}

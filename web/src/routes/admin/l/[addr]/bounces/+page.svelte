<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { getBounces, reenableBounceMember, resetBounceCount } from '$lib/api';
	import type { BounceMember } from '$lib/types';
	import { statusVariant } from '$lib/status';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import { Skeleton } from '$lib/components/ui/skeleton';

	const addr = page.params.addr ?? '';
	const at = addr.indexOf('@');
	const listName = addr.slice(0, at);
	const domain = addr.slice(at + 1);

	let members = $state<BounceMember[] | null>(null);
	let error = $state('');
	let busyId = $state<number | null>(null);
	let actionOk = $state('');

	async function load() {
		try {
			members = await getBounces(domain, listName);
			error = '';
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not load bouncing members.';
		}
	}

	onMount(load);

	async function run(m: BounceMember, label: string, fn: () => Promise<void>) {
		busyId = m.subscriber_id;
		error = '';
		actionOk = '';
		try {
			await fn();
			actionOk = `${label} ${m.email}.`;
			await load();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Action failed.';
		} finally {
			busyId = null;
		}
	}
</script>

<h2 class="text-lg font-semibold">Bounces</h2>
<p class="mt-1 text-sm text-muted-foreground">
	Members whose delivery is failing. A member is disabled after reaching the
	list's bounce threshold and can be re-enabled here; reset the count to give
	a struggling address a fresh start.
</p>

{#if actionOk}
	<p class="mt-3 rounded-md border border-success/40 bg-success/10 px-3 py-2 text-sm">{actionOk}</p>
{/if}
{#if error}
	<p class="mt-3 text-sm text-destructive">{error}</p>
{/if}

{#if members === null}
	<div class="mt-3 space-y-3">
		<Skeleton class="h-10 w-full" />
	</div>
{:else if members.length === 0}
	<p class="mt-3 text-sm text-muted-foreground">No members with bounce activity.</p>
{:else}
	<ul class="mt-3 divide-y rounded-lg border">
		{#each members as m (m.subscriber_id)}
			<li class="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
				<div class="min-w-0">
					<div class="flex flex-wrap items-center gap-x-2 gap-y-1">
						<span class="truncate font-mono text-sm font-medium">{m.email}</span>
						<Badge variant={statusVariant(m.status)} class="capitalize">{m.status}</Badge>
					</div>
					<p class="mt-0.5 text-xs text-muted-foreground">
						{m.bounce_count} / {m.bounce_threshold} bounces &middot; {m.delivery_mode}
					</p>
				</div>
				<div class="flex gap-2">
					{#if m.status === 'disabled'}
						<Button
							size="sm"
							disabled={busyId === m.subscriber_id}
							onclick={() => run(m, 'Re-enabled', () => reenableBounceMember(domain, listName, m.subscriber_id))}
						>
							Re-enable
						</Button>
					{/if}
					{#if m.bounce_count > 0}
						<Button
							variant="outline"
							size="sm"
							disabled={busyId === m.subscriber_id}
							onclick={() => run(m, 'Reset bounce count for', () => resetBounceCount(domain, listName, m.subscriber_id))}
						>
							Reset count
						</Button>
					{/if}
				</div>
			</li>
		{/each}
	</ul>
{/if}

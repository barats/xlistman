<script lang="ts">
	import { onMount } from 'svelte';
	import { ApiError, getConsoleLists } from '$lib/api';
	import type { ConsoleList } from '$lib/types';
	import { webStatus } from '$lib/access';
	import { Badge } from '$lib/components/ui/badge';
	import { Card } from '$lib/components/ui/card';
	import { Skeleton } from '$lib/components/ui/skeleton';

	let lists = $state<ConsoleList[] | null>(null);
	let phase: 'loading' | 'loaded' | 'denied' | 'error' = $state('loading');
	let error = $state('');

	onMount(async () => {
		try {
			lists = await getConsoleLists();
			phase = 'loaded';
		} catch (e) {
			if (e instanceof ApiError && (e.status === 401 || e.status === 403)) {
				phase = 'denied';
			} else {
				phase = 'error';
				error = e instanceof Error ? e.message : 'Could not load the console.';
			}
		}
	});

	function heldLabel(heldCount: number): string {
		return heldCount === 1 ? '1 held' : `${heldCount} held`;
	}
</script>

<h1 class="text-2xl font-bold tracking-tight">My lists</h1>
<p class="mt-1 text-muted-foreground">
	Review held messages and manage lists where you hold a role.
</p>

{#if $webStatus?.management_enabled === false}
	<Card class="mx-auto mt-8 max-w-md p-6 text-center">
		<h2 class="text-lg font-semibold">Web management is disabled</h2>
		<p class="mt-1 text-sm text-muted-foreground">
			The server operator has switched off web management. List consoles are unavailable.
		</p>
	</Card>
{:else if phase === 'denied'}
	<Card class="mx-auto mt-8 max-w-md p-6 text-center">
		<h2 class="text-lg font-semibold">Sign in required</h2>
		<p class="mt-1 text-sm text-muted-foreground">
			The console shows lists where you're an owner or moderator. Sign in to continue.
		</p>
		<div class="mt-4">
			<a href="/auth" class="text-sm font-medium text-primary underline-offset-4 hover:underline"
				>Sign in</a
			>
		</div>
	</Card>
{:else if phase === 'loading'}
	<div class="mt-6 space-y-3">
		{#each Array(2) as _}
			<Skeleton class="h-20 w-full" />
		{/each}
	</div>
{:else if phase === 'error'}
	<p class="mt-6 text-sm text-destructive">{error}</p>
{:else if lists && lists.length === 0}
	<Card class="mt-6 p-6 text-sm text-muted-foreground">
		You don't hold a role on any list yet. Lists you own or moderate will appear here.
	</Card>
{:else}
	<div class="mt-6 grid gap-3">
		{#each lists ?? [] as l (l.address)}
			<a href={`/admin/l/${l.address}`} class="block transition-opacity hover:opacity-80">
				<Card class="p-4">
					<div class="flex items-center justify-between gap-4">
						<div class="min-w-0">
							<p class="truncate font-semibold">{l.address}</p>
							<div class="mt-1 flex items-center gap-2">
								{#each l.roles as role (role)}
									<Badge variant="secondary" class="capitalize">{role}</Badge>
								{/each}
							</div>
						</div>
						<Badge variant={l.held_count > 0 ? 'default' : 'outline'}>{heldLabel(l.held_count)}</Badge>
					</div>
				</Card>
			</a>
		{/each}
	</div>
{/if}

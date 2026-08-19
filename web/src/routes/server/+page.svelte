<script lang="ts">
	import { onMount } from 'svelte';
	import {
		getAdminAdministrators,
		getAdminDomains,
		getAdminLists
	} from '$lib/api';
	import type { AdminAdministrator, AdminDomain, AdminList } from '$lib/types';
	import { Card } from '$lib/components/ui/card';
	import { Skeleton } from '$lib/components/ui/skeleton';

	let domains = $state<AdminDomain[] | null>(null);
	let lists = $state<AdminList[] | null>(null);
	let admins = $state<AdminAdministrator[] | null>(null);
	let error = $state('');

	onMount(async () => {
		try {
			[domains, lists, admins] = await Promise.all([
				getAdminDomains(),
				getAdminLists(),
				getAdminAdministrators()
			]);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not load server overview.';
		}
	});
</script>

{#if error}
	<p class="text-sm text-destructive">{error}</p>
{:else if !domains || !lists || !admins}
	<div class="space-y-3">
		<Skeleton class="h-6 w-1/2" />
		<Skeleton class="h-24 w-full" />
	</div>
{:else}
	<div class="grid gap-3 sm:grid-cols-3">
		<a href="/server/domains" class="group block rounded-lg">
			<Card class="p-4 transition-colors hover:bg-accent/40">
				<p class="text-sm text-muted-foreground">Domains</p>
				<p class="mt-1 text-2xl font-semibold">{domains.length}</p>
			</Card>
		</a>
		<a href="/server/lists" class="group block rounded-lg">
			<Card class="p-4 transition-colors hover:bg-accent/40">
				<p class="text-sm text-muted-foreground">Lists</p>
				<p class="mt-1 text-2xl font-semibold">{lists.length}</p>
			</Card>
		</a>
		<a href="/server/administrators" class="group block rounded-lg">
			<Card class="p-4 transition-colors hover:bg-accent/40">
				<p class="text-sm text-muted-foreground">Administrators</p>
				<p class="mt-1 text-2xl font-semibold">{admins.length}</p>
			</Card>
		</a>
	</div>

	<Card class="mt-6 p-6">
		<h2 class="text-lg font-semibold">How server administration works</h2>
		<ul class="mt-2 list-disc space-y-1 pl-5 text-sm text-muted-foreground">
			<li>
				An <strong class="font-medium text-foreground">Administrator</strong> is a Subscriber
				with instance-wide privileges. The first is designated on the server with
				<code class="rounded bg-muted px-1 py-0.5">xlistman admin add</code>; later ones can be
				added here.
			</li>
			<li>Creating a list makes you its first Owner by default (overrideable to any Subscriber).</li>
			<li>
				Deleting a list removes it and all its data — archive, members, held messages, and
				outbound queue — permanently.
			</li>
		</ul>
	</Card>
{/if}

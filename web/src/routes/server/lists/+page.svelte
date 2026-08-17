<script lang="ts">
	import { onMount } from 'svelte';
	import {
		changeAdminListType,
		createAdminList,
		deleteAdminList,
		getAdminDomains,
		getAdminLists
	} from '$lib/api';
	import type { AdminDomain, AdminList } from '$lib/types';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import { Card } from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Skeleton } from '$lib/components/ui/skeleton';

	let lists = $state<AdminList[] | null>(null);
	let domains = $state<AdminDomain[] | null>(null);
	let error = $state('');
	let busy = $state(false);

	// Create form state.
	let cName = $state('');
	let cDomain = $state('');
	let cType = $state('discussion');
	let cDesc = $state('');
	let cOwner = $state('');
	let cModeration = $state(false);

	// Per-row destructive state: address being deleted, type being changed.
	let deleting = $state<string | null>(null);
	let confirmText = $state('');
	let changingType = $state<string | null>(null);

	onMount(async () => {
		try {
			[lists, domains] = await Promise.all([getAdminLists(), getAdminDomains()]);
			if (domains && domains.length > 0 && !cDomain) cDomain = domains[0].name;
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not load lists.';
		}
	});

	async function reload() {
		lists = await getAdminLists();
	}

	async function createList() {
		busy = true;
		error = '';
		try {
			await createAdminList({
				list_name: cName,
				domain: cDomain,
				list_type: cType,
				description: cDesc,
				first_owner_email: cOwner,
				moderation: cModeration
			});
			cName = '';
			cDesc = '';
			cOwner = '';
			cModeration = false;
			await reload();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not create list.';
		} finally {
			busy = false;
		}
	}

	async function confirmDelete(address: string) {
		busy = true;
		error = '';
		try {
			const [domain, listName] = splitAddress(address);
			await deleteAdminList(domain, listName);
			deleting = null;
			confirmText = '';
			await reload();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not delete list.';
		} finally {
			busy = false;
		}
	}

	async function confirmTypeChange(list: AdminList) {
		busy = true;
		error = '';
		try {
			await changeAdminListType(list.domain, list.list_name, list.list_type === 'discussion' ? 'newsletter' : 'discussion');
			changingType = null;
			await reload();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not change list type.';
		} finally {
			busy = false;
		}
	}

	function splitAddress(address: string): [string, string] {
		const at = address.indexOf('@');
		return [address.slice(at + 1), address.slice(0, at)];
	}

	const selectClass =
		'flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring';
</script>

{#if !lists || !domains}
	<div class="space-y-3">
		<Skeleton class="h-6 w-1/2" />
		<Skeleton class="h-40 w-full" />
	</div>
{:else}
	<Card class="p-6">
		<h2 class="text-lg font-semibold">Create a list</h2>
		<form
			class="mt-4 grid gap-3 sm:grid-cols-2"
			onsubmit={(e) => { e.preventDefault(); createList(); }}
		>
			<div class="space-y-1.5">
				<Label for="list-name">Name</Label>
				<Input id="list-name" bind:value={cName} placeholder="announce" />
			</div>
			<div class="space-y-1.5">
				<Label for="list-domain">Domain</Label>
				<select id="list-domain" class={selectClass} bind:value={cDomain}>
					{#each domains ?? [] as d (d.id)}
						<option value={d.name}>{d.name}</option>
					{/each}
				</select>
			</div>
			<div class="space-y-1.5">
				<Label for="list-type">Type</Label>
				<select id="list-type" class={selectClass} bind:value={cType}>
					<option value="discussion">Discussion — members can post</option>
					<option value="newsletter">Newsletter — owners/designated senders post</option>
				</select>
			</div>
			<div class="space-y-1.5">
				<Label for="list-desc">Description</Label>
				<Input id="list-desc" bind:value={cDesc} placeholder="What is this list for?" />
			</div>
			<div class="space-y-1.5">
				<Label for="list-owner">First owner (optional)</Label>
				<Input
					id="list-owner"
					bind:value={cOwner}
					placeholder="Defaults to you"
				/>
			</div>
			{#if cType === 'discussion'}
				<label class="flex items-end gap-3 pb-1">
					<input type="checkbox" class="size-4" bind:checked={cModeration} />
					<span class="text-sm text-muted-foreground">
						Hold all posts for approval (moderation)
					</span>
				</label>
			{/if}
			<div class="sm:col-span-2">
				<Button type="submit" disabled={busy}>Create list</Button>
			</div>
		</form>
		{#if error}
			<p class="mt-3 text-sm text-destructive">{error}</p>
		{/if}
	</Card>

	<div class="mt-6 space-y-3">
		{#if lists.length === 0}
			<Card class="p-6 text-sm text-muted-foreground">
				No lists on this instance yet. Create the first one above.
			</Card>
		{:else}
			{#each lists as l (l.address)}
				<Card class="p-4">
					<div class="flex flex-wrap items-center justify-between gap-3">
						<div class="min-w-0">
							<div class="flex items-center gap-2">
								<p class="truncate font-semibold">{l.address}</p>
								<Badge variant="secondary" class="capitalize">{l.list_type}</Badge>
							</div>
							<p class="truncate text-sm text-muted-foreground">
								{l.description || 'No description'} · {l.member_count === 1
									? '1 member'
									: `${l.member_count} members`}
							</p>
						</div>
						<div class="flex shrink-0 items-center gap-2">
							<Button variant="outline" onclick={() => (changingType = l.address)}>
								{l.list_type === 'discussion' ? 'Make newsletter' : 'Make discussion'}
							</Button>
							<Button
								variant="destructive"
								onclick={() => {
									deleting = l.address;
									confirmText = '';
								}}
							>
								Delete
							</Button>
						</div>
					</div>

					{#if changingType === l.address}
						<div class="mt-3 rounded-md border border-amber-600/40 bg-amber-600/10 p-4">
							<p class="text-sm font-medium text-amber-300">Change {l.address} to {l.list_type === 'discussion' ? 'Newsletter' : 'Discussion'}?</p>
							{#if l.list_type === 'discussion'}
								<p class="mt-1 text-sm text-muted-foreground">
									Subscribers will no longer be able to post. Only owners and designated
									senders can post to a newsletter; all other posts are rejected.
								</p>
							{:else}
								<p class="mt-1 text-sm text-muted-foreground">
									All subscribers will now be able to post. Posts from non-subscribers are
									rejected unless moderation is on.
								</p>
							{/if}
							<div class="mt-3 flex gap-2">
								<Button
									size="sm"
									onclick={() => confirmTypeChange(l)}
									disabled={busy}
								>
									Change type
								</Button>
								<Button size="sm" variant="ghost" onclick={() => (changingType = null)}>
									Cancel
								</Button>
							</div>
						</div>
					{/if}

					{#if deleting === l.address}
						<div class="mt-3 rounded-md border border-destructive/40 bg-destructive/10 p-4">
							<p class="text-sm font-medium text-destructive">
								Delete {l.address} permanently? This removes the list, its archive, members,
								and held messages. This cannot be undone.
							</p>
							<div class="mt-3 flex flex-col gap-2 sm:flex-row sm:items-center">
								<Input
									bind:value={confirmText}
									placeholder={`Type ${l.address} to confirm`}
									class="sm:max-w-xs"
								/>
								<div class="flex gap-2">
									<Button
										size="sm"
										variant="destructive"
										disabled={busy || confirmText !== l.address}
										onclick={() => confirmDelete(l.address)}
									>
										Delete permanently
									</Button>
									<Button size="sm" variant="ghost" onclick={() => (deleting = null)}>
										Cancel
									</Button>
								</div>
							</div>
						</div>
					{/if}
				</Card>
			{/each}
		{/if}
	</div>
{/if}

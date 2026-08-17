<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import {
		ApiError,
		addMember,
		approveSubscription,
		getConsoleMembers,
		grantRole,
		rejectSubscription,
		removeMember,
		revokeRole
	} from '$lib/api';
	import type { ConsoleMember } from '$lib/types';
	import { Badge } from '$lib/components/ui/badge';
	import { Button } from '$lib/components/ui/button';
	import { Card } from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Skeleton } from '$lib/components/ui/skeleton';

	const addr = page.params.addr ?? '';
	const at = addr.indexOf('@');
	const listName = addr.slice(0, at);
	const domain = addr.slice(at + 1);

	let members = $state<ConsoleMember[] | null>(null);
	let phase: 'loading' | 'loaded' | 'denied' | 'error' = $state('loading');
	let error = $state('');

	async function load() {
		error = '';
		try {
			members = await getConsoleMembers(domain, listName);
			phase = 'loaded';
		} catch (e) {
			if (e instanceof ApiError && (e.status === 401 || e.status === 403)) {
				phase = 'denied';
			} else {
				phase = 'error';
				error = e instanceof Error ? e.message : 'Could not load members.';
			}
		}
	}

	onMount(load);

	// --- add member ---
	let newEmail = $state('');
	let addError = $state('');
	let addOk = $state('');
	let addBusy = $state(false);

	async function doAdd() {
		addBusy = true;
		addError = '';
		addOk = '';
		try {
			await addMember(domain, listName, newEmail.trim());
			newEmail = '';
			addOk = 'Member added.';
			await load();
		} catch (e) {
			addError = e instanceof Error ? e.message : 'Could not add member.';
		} finally {
			addBusy = false;
		}
	}

	// --- row actions ---
	let busySub = $state<number | null>(null);
	let actionError = $state('');

	async function act(subscriberId: number, fn: () => Promise<void>) {
		busySub = subscriberId;
		actionError = '';
		try {
			await fn();
			await load();
		} catch (e) {
			actionError = e instanceof Error ? e.message : 'Action failed.';
		} finally {
			busySub = null;
		}
	}

	const held = $derived((members ?? []).filter((m) => m.status === 'held'));
	const rest = $derived((members ?? []).filter((m) => m.status !== 'held'));

	function hasRole(m: ConsoleMember, role: string): boolean {
		return (m.roles ?? []).includes(role);
	}

	function statusLabel(status?: string): string {
		return status ? status.charAt(0).toUpperCase() + status.slice(1) : '';
	}
</script>

{#if phase === 'denied'}
	<Card class="p-6">
		<h2 class="text-lg font-semibold">Owner only</h2>
		<p class="mt-1 text-sm text-muted-foreground">
			Only owners can manage a list's members and roles.
		</p>
	</Card>
{:else if phase === 'loading' || !members}
	<div class="space-y-3">
		<Skeleton class="h-6 w-1/2" />
		<Skeleton class="h-40 w-full" />
	</div>
{:else if phase === 'error'}
	<p class="text-sm text-destructive">{error}</p>
{:else}
	{#if actionError}
		<p class="text-sm text-destructive">{actionError}</p>
	{/if}

	<Card class="p-6">
		<h2 class="text-lg font-semibold">Add a member</h2>
		<p class="mt-1 text-sm text-muted-foreground">
			Adds the address immediately (no confirmation email) — the owner's action is the
			authorization.
		</p>
		{#if addOk}
			<p class="mt-3 text-sm text-primary">{addOk}</p>
		{/if}
		{#if addError}
			<p class="mt-3 text-sm text-destructive">{addError}</p>
		{/if}
		<form
			class="mt-3 flex max-w-md gap-2"
			onsubmit={(e) => {
				e.preventDefault();
				doAdd();
			}}
		>
			<div class="flex-1 space-y-1.5">
				<Label for="member-email">Email</Label>
				<Input
					id="member-email"
					type="email"
					required
					autocomplete="email"
					placeholder="person@example.com"
					bind:value={newEmail}
				/>
			</div>
			<div class="flex items-end">
				<Button type="submit" disabled={addBusy}>Add member</Button>
			</div>
		</form>
	</Card>

	{#if held.length > 0}
		<div class="mt-8">
			<h2 class="text-lg font-semibold">Awaiting approval</h2>
			<p class="mt-1 text-sm text-muted-foreground">
				Confirmed subscription requests on a moderated list. Approve to activate, reject to
				remove.
			</p>
			<div class="mt-3 divide-y rounded-lg border">
				{#each held as m (m.subscriber_id)}
					<div class="flex items-center justify-between gap-3 px-4 py-3">
						<div class="min-w-0">
							<p class="truncate font-medium">{m.email}</p>
							<p class="text-sm text-muted-foreground">Requested membership</p>
						</div>
						<div class="flex shrink-0 gap-2">
							<Button
								size="sm"
								disabled={busySub === m.subscriber_id}
								onclick={() => act(m.subscriber_id, () => approveSubscription(domain, listName, m.subscriber_id))}
							>
								Approve
							</Button>
							<Button
								variant="outline"
								size="sm"
								disabled={busySub === m.subscriber_id}
								onclick={() => act(m.subscriber_id, () => rejectSubscription(domain, listName, m.subscriber_id))}
							>
								Reject
							</Button>
						</div>
					</div>
				{/each}
			</div>
		</div>
	{/if}

	<div class="mt-8">
		<h2 class="text-lg font-semibold">Members</h2>
		{#if rest.length === 0}
			<p class="mt-2 text-sm text-muted-foreground">No members yet.</p>
		{:else}
			<div class="mt-3 divide-y rounded-lg border">
				{#each rest as m (m.subscriber_id)}
					<div class="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
						<div class="min-w-0">
							<p class="truncate font-medium">{m.email}</p>
							<div class="mt-0.5 flex flex-wrap items-center gap-2">
								{#if m.status}
									<Badge variant="outline" class="capitalize">{statusLabel(m.status)}</Badge>
									<span class="text-sm text-muted-foreground capitalize">
										{m.delivery_mode}
									</span>
								{:else}
									<span class="text-sm text-muted-foreground">not subscribed</span>
								{/if}
								{#each m.roles as role (role)}
									<Badge variant="secondary" class="capitalize">{role.replace('_', ' ')}</Badge>
								{/each}
							</div>
						</div>
						<div class="flex shrink-0 flex-wrap gap-2">
							<Button
								variant={hasRole(m, 'owner') ? 'default' : 'outline'}
								size="sm"
								disabled={busySub === m.subscriber_id}
								onclick={() =>
									act(m.subscriber_id, () =>
										hasRole(m, 'owner')
											? revokeRole(domain, listName, m.subscriber_id, 'owner')
											: grantRole(domain, listName, m.subscriber_id, 'owner')
									)}
							>
								Owner
							</Button>
							<Button
								variant={hasRole(m, 'moderator') ? 'default' : 'outline'}
								size="sm"
								disabled={busySub === m.subscriber_id}
								onclick={() =>
									act(m.subscriber_id, () =>
										hasRole(m, 'moderator')
											? revokeRole(domain, listName, m.subscriber_id, 'moderator')
											: grantRole(domain, listName, m.subscriber_id, 'moderator')
									)}
							>
								Moderator
							</Button>
							{#if m.status}
								<Button
									variant="ghost"
									size="sm"
									disabled={busySub === m.subscriber_id}
									onclick={() => act(m.subscriber_id, () => removeMember(domain, listName, m.subscriber_id))}
								>
									Remove
								</Button>
							{/if}
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</div>
{/if}

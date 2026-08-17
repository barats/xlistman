<script lang="ts">
	import { onMount } from 'svelte';
	import { addAdminAdministrator, getAdminAdministrators, removeAdminAdministrator } from '$lib/api';
	import type { AdminAdministrator } from '$lib/types';
	import { Button } from '$lib/components/ui/button';
	import { Card } from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Skeleton } from '$lib/components/ui/skeleton';

	let admins = $state<AdminAdministrator[] | null>(null);
	let email = $state('');
	let error = $state('');
	let busy = $state(false);

	onMount(async () => {
		try {
			admins = await getAdminAdministrators();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not load administrators.';
		}
	});

	async function addAdmin() {
		busy = true;
		error = '';
		try {
			await addAdminAdministrator(email);
			email = '';
			admins = await getAdminAdministrators();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not add administrator.';
		} finally {
			busy = false;
		}
	}

	async function removeAdmin(id: number) {
		busy = true;
		error = '';
		try {
			await removeAdminAdministrator(id);
			admins = await getAdminAdministrators();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not remove administrator.';
		} finally {
			busy = false;
		}
	}
</script>

{#if !admins}
	<div class="space-y-3">
		<Skeleton class="h-6 w-1/2" />
		<Skeleton class="h-24 w-full" />
	</div>
{:else}
	<Card class="p-6">
		<h2 class="text-lg font-semibold">Designate an Administrator</h2>
		<p class="mt-1 text-sm text-muted-foreground">
			Administrators can create domains and lists, manage other Administrators, delete lists, and
			change list types. Only known Subscribers can be designated — add them to a list first, or
			use <code class="rounded bg-muted px-1 py-0.5">xlistman admin add</code>.
		</p>
		<form
			class="mt-4 flex flex-col gap-3 sm:flex-row sm:items-end"
			onsubmit={(e) => { e.preventDefault(); addAdmin(); }}
		>
			<div class="flex-1 space-y-1.5">
				<Label for="admin-email">Subscriber email</Label>
				<Input id="admin-email" type="email" bind:value={email} placeholder="admin@example.com" />
			</div>
			<Button type="submit" disabled={busy}>Add Administrator</Button>
		</form>
		{#if error}
			<p class="mt-3 text-sm text-destructive">{error}</p>
		{/if}
	</Card>

	<div class="mt-6 space-y-3">
		{#if admins.length === 0}
			<Card class="p-6 text-sm text-muted-foreground">
				No Administrators. Designate the first one above or on the server with
				<code class="rounded bg-muted px-1 py-0.5">xlistman admin add</code>.
			</Card>
		{:else}
			{#each admins as a (a.id)}
				<Card class="p-4">
					<div class="flex items-center justify-between gap-4">
						<p class="min-w-0 truncate font-semibold">{a.email}</p>
						<Button
							variant="outline"
							size="sm"
							disabled={busy}
							onclick={() => removeAdmin(a.id)}
						>
							Revoke
						</Button>
					</div>
				</Card>
			{/each}
		{/if}
	</div>
{/if}

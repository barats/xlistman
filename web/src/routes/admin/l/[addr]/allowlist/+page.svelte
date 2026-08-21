<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { addSender, getSenders, removeSender } from '$lib/api';
	import type { DesignatedSender } from '$lib/types';
	import { getSiteName, setSeo } from '$lib/seo';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Skeleton } from '$lib/components/ui/skeleton';

	const site = getSiteName();
	const addr = page.params.addr ?? '';
	const at = addr.indexOf('@');
	const listName = addr.slice(0, at);
	const domain = addr.slice(at + 1);

	setSeo({
		title: `Designated senders — ${addr} — ${site}`,
		description: `Manage designated senders on ${addr}.`,
		noindex: true
	});

	let senders = $state<DesignatedSender[] | null>(null);
	let error = $state('');

	async function load() {
		try {
			senders = await getSenders(domain, listName);
			error = '';
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not load designated senders.';
		}
	}

	onMount(load);

	let senderEmail = $state('');
	let senderError = $state('');
	let senderOk = $state('');
	let senderBusy = $state(false);

	async function doAddSender() {
		senderBusy = true;
		senderError = '';
		senderOk = '';
		try {
			await addSender(domain, listName, senderEmail.trim());
			senderEmail = '';
			senders = await getSenders(domain, listName);
			senderOk = 'Sender added.';
		} catch (e) {
			senderError = e instanceof Error ? e.message : 'Could not add sender.';
		} finally {
			senderBusy = false;
		}
	}

	async function doRemoveSender(id: number) {
		senderBusy = true;
		senderError = '';
		senderOk = '';
		try {
			await removeSender(domain, listName, id);
			senders = await getSenders(domain, listName);
			senderOk = 'Sender removed.';
		} catch (e) {
			senderError = e instanceof Error ? e.message : 'Could not remove sender.';
		} finally {
			senderBusy = false;
		}
	}
</script>

{#if error}
	<p class="text-sm text-destructive">{error}</p>
{/if}

<h2 class="text-lg font-semibold">Designated senders</h2>
<p class="mt-1 text-sm text-muted-foreground">
	Only these subscribers (and owners) can post to this newsletter.
</p>

{#if senderOk}
	<p class="mt-3 rounded-md border bg-muted/50 px-3 py-2 text-sm">{senderOk}</p>
{/if}
{#if senderError}
	<p class="mt-3 text-sm text-destructive">{senderError}</p>
{/if}

<form
	class="mt-3 flex max-w-md gap-2"
	onsubmit={(e) => {
		e.preventDefault();
		doAddSender();
	}}
>
	<div class="flex-1 space-y-1.5">
		<Label for="sender-email">Subscriber email</Label>
		<Input
			id="sender-email"
			type="email"
			required
			autocomplete="email"
			placeholder="author@example.com"
			bind:value={senderEmail}
		/>
	</div>
	<div class="flex items-end">
		<Button type="submit" disabled={senderBusy}>Add sender</Button>
	</div>
</form>

{#if senders === null}
	<div class="mt-3 space-y-3">
		<Skeleton class="h-10 w-full" />
	</div>
{:else if senders.length === 0}
	<p class="mt-3 text-sm text-muted-foreground">No designated senders yet.</p>
{:else}
	<ul class="mt-3 divide-y rounded-lg border">
		{#each senders as s (s.id)}
			<li class="flex items-center justify-between gap-3 px-4 py-2.5">
				<span class="truncate text-sm">{s.email}</span>
				<Button
					variant="ghost"
					size="sm"
					disabled={senderBusy}
					onclick={() => doRemoveSender(s.id)}
				>
					Remove
				</Button>
			</li>
		{/each}
	</ul>
{/if}

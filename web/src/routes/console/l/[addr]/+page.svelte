<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import {
		ApiError,
		addSender,
		getConsoleListInfo,
		getHeldMessages,
		getSenders,
		moderate,
		removeSender
	} from '$lib/api';
	import type { ConsoleListInfo, DesignatedSender, HeldMessage } from '$lib/types';
	import { Badge } from '$lib/components/ui/badge';
	import { Button, buttonVariants } from '$lib/components/ui/button';
	import { Card } from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Skeleton } from '$lib/components/ui/skeleton';

	const addr = page.params.addr ?? '';
	const at = addr.indexOf('@');
	const listName = addr.slice(0, at);
	const domain = addr.slice(at + 1);

	let info = $state<ConsoleListInfo | null>(null);
	let held = $state<HeldMessage[] | null>(null);
	let senders = $state<DesignatedSender[] | null>(null);
	let phase: 'loading' | 'loaded' | 'denied' | 'error' = $state('loading');
	let error = $state('');

	async function load() {
		phase = 'loading';
		error = '';
		try {
			info = await getConsoleListInfo(domain, listName);
			held = await getHeldMessages(domain, listName);
			if (info.list_type === 'newsletter' && info.roles.includes('owner')) {
				senders = await getSenders(domain, listName);
			}
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

	// --- moderation actions ---
	let busyHeldId = $state<number | null>(null);
	let heldError = $state('');

	async function act(id: number, action: 'approve' | 'reject' | 'discard') {
		busyHeldId = id;
		heldError = '';
		try {
			await moderate(domain, listName, id, action);
			await load();
		} catch (e) {
			heldError = e instanceof Error ? e.message : 'Action failed.';
		} finally {
			busyHeldId = null;
		}
	}

	// --- allowlist ---
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

	function fmtDate(iso: string): string {
		return new Date(iso).toLocaleString();
	}
</script>

<div class="flex flex-wrap items-center justify-between gap-3">
	<div>
		<a href="/console" class="text-sm text-muted-foreground hover:text-foreground">
			&larr; Console
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

{#if phase === 'denied'}
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
	{#if heldError}
		<p class="mt-4 text-sm text-destructive">{heldError}</p>
	{/if}

	<div class="mt-6">
		<h2 class="text-lg font-semibold">Held messages</h2>
		{#if held && held.length === 0}
			<p class="mt-2 text-sm text-muted-foreground">No messages awaiting moderation.</p>
		{:else}
			<div class="mt-3 divide-y rounded-lg border">
				{#each held ?? [] as m (m.id)}
					<div class="flex items-center justify-between gap-3 px-4 py-3">
						<a
							href={`/console/l/${addr}/held/${m.id}`}
							class="block min-w-0 transition-colors hover:bg-accent/50"
						>
							<p class="truncate font-medium">{m.subject || '(no subject)'}</p>
							<p class="mt-0.5 truncate text-sm text-muted-foreground">
								{m.sender} · {fmtDate(m.received_at)}
							</p>
						</a>
						<div class="flex shrink-0 gap-2">
							<Button
								size="sm"
								disabled={busyHeldId === m.id}
								onclick={() => act(m.id, 'approve')}
							>
								Approve
							</Button>
							<Button
								variant="outline"
								size="sm"
								disabled={busyHeldId === m.id}
								onclick={() => act(m.id, 'reject')}
							>
								Reject
							</Button>
							<Button
								variant="ghost"
								size="sm"
								disabled={busyHeldId === m.id}
								onclick={() => act(m.id, 'discard')}
							>
								Discard
							</Button>
						</div>
					</div>
				{/each}
			</div>
		{/if}
	</div>

	{#if info && info.list_type === 'newsletter' && info.roles.includes('owner')}
		<div class="mt-8">
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

			{#if senders && senders.length === 0}
				<p class="mt-3 text-sm text-muted-foreground">No designated senders yet.</p>
			{:else}
				<ul class="mt-3 divide-y rounded-lg border">
					{#each senders ?? [] as s (s.id)}
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
		</div>
	{/if}
{/if}

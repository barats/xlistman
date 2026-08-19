<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { getList, subscribe } from '$lib/api';
	import type { ListInfo } from '$lib/types';
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

	let info = $state<ListInfo | null>(null);
	let error = $state('');

	onMount(async () => {
		try {
			info = await getList(domain, listName);
		} catch {
			error = 'List not found.';
		}
	});

	let email = $state('');
	let phase: 'idle' | 'sending' | 'sent' | 'error' = $state('idle');
	let formError = $state('');

	async function doSubscribe() {
		phase = 'sending';
		formError = '';
		try {
			await subscribe(domain, listName, email.trim());
			phase = 'sent';
		} catch (e) {
			phase = 'error';
			formError = e instanceof Error ? e.message : 'Subscription failed.';
		}
	}
</script>

{#if error}
	<h1 class="text-2xl font-bold tracking-tight">List not found</h1>
	<p class="mt-2 text-muted-foreground">{error}</p>
{:else if info === null}
	<div class="space-y-3">
		<Skeleton class="h-8 w-64" />
		<Skeleton class="h-4 w-96 max-w-full" />
		<Skeleton class="h-24 w-full" />
	</div>
{:else}
	<div class="flex flex-wrap items-center justify-between gap-3">
		<div class="min-w-0">
			<h1 class="truncate font-mono text-3xl font-bold tracking-tight">{info.address}</h1>
			<p class="mt-1 text-muted-foreground">{info.description || 'No description'}</p>
		</div>
		<Badge variant="secondary" class="shrink-0">{info.list_type}</Badge>
	</div>

	<dl class="mt-5 grid gap-2 text-sm sm:grid-cols-3">
		<div>
			<dt class="text-muted-foreground">Subscription</dt>
			<dd class="font-medium capitalize">{info.subscription_policy}</dd>
		</div>
		<div>
			<dt class="text-muted-foreground">Moderation</dt>
			<dd class="font-medium">{info.moderation_enabled ? 'On' : 'Off'}</dd>
		</div>
		<div>
			<dt class="text-muted-foreground">Digest</dt>
			<dd class="font-medium capitalize">{info.digest_frequency}</dd>
		</div>
	</dl>

	<div class="mt-6">
		<a href={`/l/${addr}/archives`} class={buttonVariants({ variant: 'outline' })}>
			Browse archives
		</a>
	</div>

	<Card class="mt-6 p-6">
		<h2 class="text-lg font-semibold">Subscribe</h2>
		<p class="mt-1 text-sm text-muted-foreground">
			We'll email you a confirmation message to double opt-in.
		</p>

		{#if phase === 'sent'}
			<div class="mt-4 rounded-md border bg-muted/50 p-4 text-sm">
				A confirmation email is on its way to <span class="font-medium">{email.trim()}</span>.
				Reply to it to confirm your subscription.
			</div>
		{:else}
			<form
				class="mt-4 space-y-3"
				onsubmit={(e) => {
					e.preventDefault();
					doSubscribe();
				}}
			>
				<div class="space-y-1.5">
					<Label for="subscribe-email">Email address</Label>
					<Input
						id="subscribe-email"
						type="email"
						required
						autocomplete="email"
						placeholder="you@example.com"
						bind:value={email}
					/>
				</div>
				{#if phase === 'error'}
					<p class="text-sm text-destructive">{formError}</p>
				{/if}
				<Button type="submit" disabled={phase === 'sending'}>
					{phase === 'sending' ? 'Sending…' : 'Subscribe'}
				</Button>
			</form>
		{/if}
	</Card>
{/if}

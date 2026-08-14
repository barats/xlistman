<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { me, refreshMe } from '$lib/auth';
	import { unsubscribeMe } from '$lib/api';
	import { Button, buttonVariants } from '$lib/components/ui/button';
	import { Card } from '$lib/components/ui/card';

	const listAddr = page.url.searchParams.get('list') ?? '';
	const email = page.url.searchParams.get('email') ?? '';
	const authHref = `/auth?email=${encodeURIComponent(email)}`;

	let done = $state(false);
	let error = $state('');
	let working = $state(false);

	onMount(() => {
		refreshMe();
	});

	const sub = $derived(
		$me?.subscriptions.find(
			(s) => s.address === listAddr && ($me?.email ?? '').toLowerCase() === email.toLowerCase()
		)
	);

	async function doUnsubscribe() {
		if (!sub) return;
		working = true;
		error = '';
		try {
			await unsubscribeMe(sub.id);
			done = true;
			await refreshMe();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Unsubscribe failed.';
		} finally {
			working = false;
		}
	}
</script>

<Card class="mx-auto max-w-md p-6">
	{#if done}
		<h1 class="text-2xl font-bold tracking-tight">Unsubscribed</h1>
		<p class="mt-2 text-sm text-muted-foreground">
			{email} has been removed from <span class="font-medium text-foreground">{listAddr}</span>.
		</p>
		<div class="mt-4">
			<a href="/" class="text-sm font-medium text-primary underline-offset-4 hover:underline"
				>&larr; Back to lists</a
			>
		</div>
	{:else if $me === null}
		<h1 class="text-2xl font-bold tracking-tight">Confirm to unsubscribe</h1>
		<p class="mt-2 text-sm text-muted-foreground">
			To unsubscribe <span class="font-medium text-foreground">{email}</span> from
			<span class="font-medium text-foreground">{listAddr}</span>, sign in to confirm you own
			this address.
		</p>
		<div class="mt-4">
			<a href={authHref} class={buttonVariants()}>Sign in</a>
		</div>
	{:else if sub}
		<h1 class="text-2xl font-bold tracking-tight">Unsubscribe</h1>
		<p class="mt-2 text-sm text-muted-foreground">
			Remove <span class="font-medium text-foreground">{email}</span> from
			<span class="font-medium text-foreground">{listAddr}</span>?
		</p>
		{#if error}
			<p class="mt-3 text-sm text-destructive">{error}</p>
		{/if}
		<div class="mt-4 flex gap-2">
			<Button variant="destructive" disabled={working} onclick={doUnsubscribe}>
				{working ? 'Removing…' : 'Unsubscribe'}
			</Button>
			<a href={`/l/${listAddr}`} class={buttonVariants({ variant: 'ghost' })}>Cancel</a>
		</div>
	{:else}
		<h1 class="text-2xl font-bold tracking-tight">Not subscribed</h1>
		<p class="mt-2 text-sm text-muted-foreground">
			<span class="font-medium text-foreground">{email}</span> is not subscribed to
			<span class="font-medium text-foreground">{listAddr}</span>.
		</p>
		<div class="mt-4">
			<a href="/" class="text-sm font-medium text-primary underline-offset-4 hover:underline"
				>&larr; Back to lists</a
			>
		</div>
	{/if}
</Card>

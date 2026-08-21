<script lang="ts">
	import { onMount } from 'svelte';
	import { me, refreshMe } from '$lib/auth';
	import { getMyHeldPosts, reEnable, setDelivery, unsubscribeMe } from '$lib/api';
	import type { HeldPost, Subscription } from '$lib/types';
	import { getSiteName, setSeo } from '$lib/seo';
	import { cn } from '$lib/utils';
	import { fmtRelative } from '$lib/dates';
	import { statusVariant } from '$lib/status';
	import { Badge } from '$lib/components/ui/badge';
	import { Button, buttonVariants } from '$lib/components/ui/button';
	import { Card } from '$lib/components/ui/card';
	import { Skeleton } from '$lib/components/ui/skeleton';

	const site = getSiteName();
	setSeo({
		title: `My subscriptions — ${site}`,
		description: `Manage your subscriptions and delivery preferences on ${site}.`,
		noindex: true
	});

	const modes = [
		{ value: 'regular', label: 'Regular' },
		{ value: 'digest', label: 'Digest' },
		{ value: 'nomail', label: 'Nomail' }
	];

	let busyId = $state<number | null>(null);
	let actionError = $state('');
	let actionOk = $state('');
	let heldPosts = $state<HeldPost[] | null>(null);

	onMount(async () => {
		refreshMe();
		try {
			heldPosts = await getMyHeldPosts();
		} catch {
			heldPosts = [];
		}
	});

	async function run(sub: Subscription, label: string, fn: () => Promise<void>) {
		busyId = sub.id;
		actionError = '';
		actionOk = '';
		try {
			await fn();
			actionOk = `${label} on ${sub.address}.`;
			await refreshMe();
		} catch (e) {
			actionError = e instanceof Error ? e.message : 'Action failed.';
		} finally {
			busyId = null;
		}
	}
</script>

{#if $me === undefined}
	<div class="space-y-3">
		<Skeleton class="h-8 w-64" />
		{#each Array(2) as _}
			<Skeleton class="h-28 w-full" />
		{/each}
	</div>
{:else if $me === null}
	<Card class="mx-auto max-w-md p-6 text-center">
		<h1 class="text-2xl font-bold tracking-tight">Sign in required</h1>
		<p class="mt-1 text-sm text-muted-foreground">
			To manage your subscriptions, sign in with your email address.
		</p>
		<div class="mt-4">
			<a href="/auth" class={buttonVariants()}>Sign in</a>
		</div>
	</Card>
{:else}
	<h1 class="text-2xl font-bold tracking-tight">My subscriptions</h1>
	<p class="mt-1 text-muted-foreground">Signed in as {$me.email}</p>

	{#if actionOk}
		<p class="mt-4 rounded-md border border-success/40 bg-success/10 px-3 py-2 text-sm">{actionOk}</p>
	{/if}
	{#if actionError}
		<p class="mt-4 text-sm text-destructive">{actionError}</p>
	{/if}

	{#if $me.subscriptions.length === 0}
		<p class="mt-6 text-muted-foreground">You're not subscribed to any lists.</p>
	{:else}
		<div class="mt-6 space-y-4">
			{#each $me.subscriptions as sub (sub.id)}
				<Card class="p-4">
					<div class="flex flex-wrap items-center justify-between gap-2">
						<a href={`/l/${sub.address}`} class="font-mono font-semibold hover:underline">
							{sub.address}
						</a>
						<Badge variant={statusVariant(sub.status)} class="capitalize">
							{sub.status}
						</Badge>
					</div>

					{#if sub.status === 'disabled'}
						<div
							class="mt-3 rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive"
						>
							Bounces ({sub.bounce_count}) disabled delivery. Re-enable to start receiving
							posts again.
						</div>
						<div class="mt-3">
							<Button
								variant="outline"
								disabled={busyId === sub.id}
								onclick={() => run(sub, 'Re-enabled', () => reEnable(sub.id))}
							>
								Re-enable
							</Button>
						</div>
					{:else}
						<div class="mt-3">
							<p class="text-sm text-muted-foreground">Delivery</p>
							<div class="mt-1.5 inline-flex rounded-md border">
								{#each modes as m (m.value)}
									<button
										type="button"
										class={cn(
											'px-3 py-1.5 text-sm font-medium transition-colors',
											sub.delivery_mode === m.value
												? 'bg-primary text-primary-foreground'
												: 'text-muted-foreground hover:bg-accent'
										)}
										disabled={busyId === sub.id}
										onclick={() =>
											run(
												sub,
												`Delivery set to ${m.label.toLowerCase()}`,
												() => setDelivery(sub.id, m.value)
											)}
									>
										{m.label}
									</button>
								{/each}
							</div>
						</div>
					{/if}

					<div class="mt-4 flex flex-wrap gap-2">
						<a
							href={`/l/${sub.address}/archives`}
							class={buttonVariants({ variant: 'outline', size: 'sm' })}
						>
							Archives
						</a>
						<Button
							variant="ghost"
							size="sm"
							disabled={busyId === sub.id}
							onclick={() => run(sub, 'Unsubscribed', () => unsubscribeMe(sub.id))}
						>
							Unsubscribe
						</Button>
					</div>
				</Card>
			{/each}
		</div>
	{/if}

	{#if heldPosts !== null}
		<section class="mt-10">
			<h2 class="text-lg font-semibold">Posts awaiting approval</h2>
			<p class="mt-1 text-sm text-muted-foreground">
				Posts you've sent that are waiting for a moderator's decision. You're notified by
				email when a post is approved or rejected; discarded posts are removed silently.
			</p>
			{#if heldPosts.length === 0}
				<p class="mt-3 text-sm text-muted-foreground">No posts awaiting approval.</p>
			{:else}
				<ul class="mt-3 divide-y rounded-lg border">
					{#each heldPosts as p (p.id)}
						<li class="px-4 py-3">
							<div class="flex flex-wrap items-center gap-x-2 gap-y-1">
								<span class="text-sm font-medium">{p.subject || '(no subject)'}</span>
								<span class="rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
									{p.list_addr}
								</span>
							</div>
							<p class="mt-1 text-sm text-muted-foreground">
								Sent {fmtRelative(p.received_at)} &middot; expires {fmtRelative(p.expires_at)}
							</p>
						</li>
					{/each}
				</ul>
			{/if}
		</section>
	{/if}
{/if}

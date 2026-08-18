<script lang="ts">
	import { page } from '$app/state';
	import { requestMagicLink } from '$lib/api';
	import { me } from '$lib/auth';
	import { webStatus } from '$lib/access';
	import { Button } from '$lib/components/ui/button';
	import { Card } from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';

	const errorParam = page.url.searchParams.get('error');
	const prefill = page.url.searchParams.get('email') ?? '';

	let email = $state(prefill);
	let phase: 'idle' | 'sending' | 'sent' | 'error' = $state('idle');
	let error = $state('');

	const loginDisabled = $derived(
		$webStatus?.login_enabled === false || errorParam === 'disabled'
	);

	async function send() {
		phase = 'sending';
		error = '';
		try {
			await requestMagicLink(email.trim());
			phase = 'sent';
		} catch (e) {
			phase = 'error';
			error = e instanceof Error ? e.message : 'Could not send the login link.';
		}
	}
</script>

{#if loginDisabled}
	<Card class="mx-auto max-w-md p-6 text-center">
		<h1 class="text-2xl font-bold tracking-tight">Sign in is disabled</h1>
		<p class="mt-2 text-sm text-muted-foreground">
			Web login has been switched off by the server operator. Existing subscriptions,
			unsubscribe links, and public list pages still work.
		</p>
	</Card>
{:else if $me}
	<div class="mx-auto max-w-md text-center">
		<h1 class="text-2xl font-bold tracking-tight">You're signed in</h1>
		<p class="mt-2 text-muted-foreground">
			Signed in as <span class="font-medium text-foreground">{$me.email}</span>.
		</p>
		<div class="mt-4">
			<a href="/me" class="text-sm font-medium text-primary underline-offset-4 hover:underline"
				>Go to your subscriptions</a
			>
		</div>
	</div>
{:else}
	<Card class="mx-auto max-w-md p-6">
		<h1 class="text-2xl font-bold tracking-tight">Sign in</h1>
		<p class="mt-1 text-sm text-muted-foreground">
			Passwordless — we'll email you a one-time login link.
		</p>

		{#if errorParam === 'invalid'}
			<div
				class="mt-4 rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive"
			>
				That login link is invalid or has expired. Request a new one below.
			</div>
		{/if}

		{#if phase === 'sent'}
			<div class="mt-4 rounded-md border bg-muted/50 p-4 text-sm">
				If <span class="font-medium">{email.trim()}</span> is a known address, a login link is
				on its way. Check your inbox.
			</div>
		{:else}
			<form
				class="mt-4 space-y-3"
				onsubmit={(e) => {
					e.preventDefault();
					send();
				}}
			>
				<div class="space-y-1.5">
					<Label for="auth-email">Email address</Label>
					<Input
						id="auth-email"
						type="email"
						required
						autocomplete="email"
						placeholder="you@example.com"
						bind:value={email}
					/>
				</div>
				{#if phase === 'error'}
					<p class="text-sm text-destructive">{error}</p>
				{/if}
				<Button type="submit" disabled={phase === 'sending'} class="w-full">
					{phase === 'sending' ? 'Sending…' : 'Send login link'}
				</Button>
			</form>
		{/if}
	</Card>
{/if}

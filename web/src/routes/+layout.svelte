<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { LogOut, Mail } from '@lucide/svelte';
	import { goto } from '$app/navigation';
	import { me, refreshMe, signOut } from '$lib/auth';
	import { webStatus, refreshWebStatus } from '$lib/access';

	let { children } = $props();

	onMount(() => {
		refreshMe();
		refreshWebStatus();
	});
</script>

<div class="flex min-h-dvh flex-col bg-background">
	<header class="border-b">
		<div class="mx-auto flex h-14 w-full max-w-3xl items-center justify-between px-4">
			<a href="/" class="flex items-center gap-2 font-semibold tracking-tight">
				<Mail class="size-5" />
				xListman
			</a>
			<nav class="flex items-center gap-4 text-sm">
				<a href="/" class="text-muted-foreground transition-colors hover:text-foreground">Lists</a>
				{#if $me}
					<a href="/me" class="text-muted-foreground transition-colors hover:text-foreground"
						>My subscriptions</a
					>
					{#if $me.has_list_role && $webStatus?.management_enabled !== false}
						<a href="/admin" class="text-muted-foreground transition-colors hover:text-foreground"
							>My lists</a
						>
					{/if}
					{#if $me.is_administrator && $webStatus?.management_enabled !== false}
						<a href="/server" class="text-muted-foreground transition-colors hover:text-foreground"
							>Server</a
						>
					{/if}
					<button
						class="inline-flex items-center gap-1.5 text-muted-foreground transition-colors hover:text-foreground"
						onclick={async () => {
							await signOut();
							goto('/');
						}}
					>
						<LogOut class="size-4" /> Sign out
					</button>
				{:else if $me === null}
					<a href="/auth" class="text-muted-foreground transition-colors hover:text-foreground"
						>Sign in</a
					>
				{/if}
			</nav>
		</div>
	</header>
	<main class="mx-auto w-full max-w-3xl flex-1 px-4 py-8">
		{@render children()}
	</main>
	<footer class="border-t">
		<div class="mx-auto w-full max-w-3xl px-4 py-4 text-xs text-muted-foreground">
			xListman — self-hosted mailing lists
		</div>
	</footer>
</div>

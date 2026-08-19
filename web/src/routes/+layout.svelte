<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { LogOut, Mail } from '@lucide/svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { me, refreshMe, signOut } from '$lib/auth';
	import { webStatus, refreshWebStatus } from '$lib/access';

	let { children } = $props();

	onMount(() => {
		refreshMe();
		refreshWebStatus();
	});

	const isListIndex = $derived(page.url.pathname === '/');
	const isMe = $derived(page.url.pathname.startsWith('/me'));
	const isAdmin = $derived(page.url.pathname.startsWith('/admin'));
	const isServer = $derived(page.url.pathname.startsWith('/server'));
</script>

<div class="flex min-h-dvh flex-col bg-background">
	<header class="border-b">
		<div
			class="mx-auto flex min-h-14 w-full max-w-5xl flex-wrap items-center justify-between gap-x-4 gap-y-2 px-4 py-2"
		>
			<a href="/" class="flex items-center gap-2 font-semibold tracking-tight">
				<Mail class="size-5" />
				xListman
			</a>
			<nav class="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
				<a
					href="/"
					class="text-muted-foreground transition-colors hover:text-foreground"
					aria-current={isListIndex ? 'page' : undefined}
				>
					Lists
				</a>
				{#if $me}
					<a
						href="/me"
						class="text-muted-foreground transition-colors hover:text-foreground"
						aria-current={isMe ? 'page' : undefined}
					>
						My subscriptions
					</a>
					{#if $me.has_list_role && $webStatus?.management_enabled !== false}
						<a
							href="/admin"
							class="text-muted-foreground transition-colors hover:text-foreground"
							aria-current={isAdmin ? 'page' : undefined}
						>
							My lists
						</a>
					{/if}
					{#if $me.is_administrator && $webStatus?.management_enabled !== false}
						<a
							href="/server"
							class="text-muted-foreground transition-colors hover:text-foreground"
							aria-current={isServer ? 'page' : undefined}
						>
							Server
						</a>
					{/if}
					<button
						class="inline-flex items-center gap-1.5 text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring focus-visible:ring-offset-1"
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
	<main class="mx-auto w-full max-w-5xl flex-1 px-4 py-8">
		{@render children()}
	</main>
	<footer class="border-t">
		<div class="mx-auto w-full max-w-5xl px-4 py-4 text-xs text-muted-foreground">
			xListman · self-hosted mailing lists
		</div>
	</footer>
</div>

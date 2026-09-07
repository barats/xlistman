<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { LogOut, Menu, X } from '@lucide/svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { me, refreshMe, signOut } from '$lib/auth';
	import { webStatus, refreshWebStatus } from '$lib/access';
	import { getSiteName, getVersion } from '$lib/seo';

	let { children } = $props();

	const site = getSiteName();
	const version = getVersion();

	onMount(() => {
		refreshMe();
		refreshWebStatus();
	});

	const isListIndex = $derived(page.url.pathname === '/');
	const isMe = $derived(page.url.pathname.startsWith('/me'));
	const isAdmin = $derived(page.url.pathname.startsWith('/admin'));
	const isServer = $derived(page.url.pathname.startsWith('/server'));

	// Mobile navigation: links collapse behind a hamburger that expands a
	// vertical dropdown under the header.
	let menuOpen = $state(false);

	function closeMenu() {
		menuOpen = false;
	}
</script>

{#snippet navLinks(close: () => void)}
	<a
		href="/"
		onclick={close}
		class="text-muted-foreground transition-colors hover:text-foreground"
		aria-current={isListIndex ? 'page' : undefined}
	>
		Lists
	</a>
	{#if $me}
		<a
			href="/me"
			onclick={close}
			class="text-muted-foreground transition-colors hover:text-foreground"
			aria-current={isMe ? 'page' : undefined}
		>
			My subscriptions
		</a>
		{#if $me.has_list_role && $webStatus?.management_enabled !== false}
			<a
				href="/admin"
				onclick={close}
				class="text-muted-foreground transition-colors hover:text-foreground"
				aria-current={isAdmin ? 'page' : undefined}
			>
				Admin
			</a>
		{/if}
		{#if $me.is_administrator && $webStatus?.management_enabled !== false}
			<a
				href="/server"
				onclick={close}
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
		<a href="/auth" onclick={close} class="text-muted-foreground transition-colors hover:text-foreground"
			>Sign in</a
		>
	{/if}
{/snippet}

<div class="flex min-h-dvh flex-col bg-background">
	<header class="border-b">
		<div
			class="mx-auto flex min-h-14 w-full max-w-5xl flex-wrap items-center justify-between gap-x-4 gap-y-2 px-4 py-2"
		>
			<a href="/" class="flex items-center gap-2 font-semibold tracking-tight">
				<svg
					class="size-5 shrink-0"
					viewBox="0 0 64 64"
					role="img"
					aria-label={site}
					xmlns="http://www.w3.org/2000/svg"
				>
					<circle cx="32" cy="20" r="17" fill="#039ba3" />
					<path
						d="M 44 30 L 44 35 L 38 35"
						fill="none"
						stroke="#039ba3"
						stroke-width="2.6"
						stroke-linecap="round"
						stroke-linejoin="round"
					/>
					<polygon points="38,31.5 34,35 38,38.5" fill="#039ba3" />
					<g
						fill="none"
						stroke="#ffffff"
						stroke-width="2.2"
						stroke-linecap="round"
						stroke-linejoin="round"
					>
						<polyline points="22,11 24.5,13.5 28,10" />
						<polyline points="22,18 24.5,20.5 28,17" />
						<polyline points="22,25 24.5,27.5 28,24" />
					</g>
					<g stroke="#ffffff" stroke-width="2.2" stroke-linecap="round">
						<line x1="30" y1="10" x2="42" y2="10" />
						<line x1="30" y1="17" x2="42" y2="17" />
						<line x1="30" y1="24" x2="42" y2="24" />
						<line x1="30" y1="31" x2="42" y2="31" />
					</g>
					<path
						d="M 6 34 L 58 34 L 58 56 Q 58 58 56 58 L 8 58 Q 6 58 6 56 Z"
						fill="#053776"
					/>
					<path d="M 6 34 L 32 49 L 58 34 Z" fill="#ffffff" />
					<path
						d="M 6 34 L 32 49 L 58 34"
						fill="none"
						stroke="#053776"
						stroke-width="2.2"
						stroke-linejoin="round"
						stroke-linecap="round"
					/>
				</svg>
				{site}
			</a>
			<div class="flex items-center gap-1">
				<nav class="hidden items-center gap-x-4 text-sm md:flex">
					{@render navLinks(() => {})}
				</nav>
				<button
					type="button"
					class="inline-flex size-9 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring md:hidden"
					aria-label="Toggle menu"
					aria-expanded={menuOpen}
					onclick={() => (menuOpen = !menuOpen)}
				>
					{#if menuOpen}
						<X class="size-5" />
					{:else}
						<Menu class="size-5" />
					{/if}
				</button>
			</div>
		</div>
		{#if menuOpen}
			<div class="border-t md:hidden">
				<nav class="mx-auto flex max-w-5xl flex-col gap-0.5 px-4 py-2 text-sm">
					{@render navLinks(closeMenu)}
				</nav>
			</div>
		{/if}
	</header>
	<main class="mx-auto w-full max-w-5xl flex-1 px-4 py-8">
		{@render children()}
	</main>
	<footer class="border-t">
		<div class="mx-auto w-full max-w-5xl px-4 py-4 text-center text-xs text-muted-foreground">
			Powered by
			<a
				href="https://www.xlistman.com"
				class="font-medium underline underline-offset-2 transition-colors hover:text-foreground"
			>
				xListman{version ? ' ' + version : ''}
			</a>
		</div>
	</footer>
</div>

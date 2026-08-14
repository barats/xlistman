<script lang="ts">
	import type { HTMLButtonAttributes } from 'svelte/elements';
	import type { Snippet } from 'svelte';
	import { cn } from '$lib/utils';
	import { buttonVariants, type ButtonVariants } from './button-variants';

	let {
		children,
		class: className,
		variant = 'default',
		size = 'default',
		...restProps
	}: {
		children?: Snippet;
		class?: string;
	} & ButtonVariants & HTMLButtonAttributes = $props();
</script>

<button class={cn(buttonVariants({ variant, size }), className)} {...restProps}>
	{@render children?.()}
</button>

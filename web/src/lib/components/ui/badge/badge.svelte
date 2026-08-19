<script lang="ts">
	import { cva, type VariantProps } from 'class-variance-authority';
	import type { HTMLAttributes } from 'svelte/elements';
	import type { Snippet } from 'svelte';
	import { cn } from '$lib/utils';

	const badgeVariants = cva(
		'inline-flex items-center rounded-md border px-2.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2',
		{
			variants: {
				variant: {
					default: 'border-transparent bg-primary text-primary-foreground shadow',
					secondary: 'border-transparent bg-secondary text-secondary-foreground',
					destructive: 'border-transparent bg-destructive text-destructive-foreground shadow',
					success: 'border-transparent bg-success text-success-foreground shadow',
					warning: 'border-transparent bg-warning text-warning-foreground',
					outline: 'text-foreground'
				}
			},
			defaultVariants: { variant: 'default' }
		}
	);

	type Variants = VariantProps<typeof badgeVariants>;

	let {
		children,
		class: className,
		variant = 'default',
		...restProps
	}: { children?: Snippet; class?: string } & Variants & HTMLAttributes<HTMLSpanElement> = $props();
</script>

<span class={cn(badgeVariants({ variant }), className)} {...restProps}>
	{@render children?.()}
</span>

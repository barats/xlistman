<script lang="ts">
	import { cn } from '$lib/utils';

	let {
		orientation = 'horizontal',
		class: className
	}: { orientation?: 'horizontal' | 'vertical'; class?: string } = $props();
</script>

{#if orientation === 'horizontal'}
	<div role="separator" aria-orientation="horizontal" class={cn('h-px w-full bg-border', className)}></div>
{:else}
	<div role="separator" aria-orientation="vertical" class={cn('h-full w-px bg-border', className)}></div>
{/if}

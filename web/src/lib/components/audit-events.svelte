<script lang="ts">
	import type { AuditEvent } from '$lib/types';
	import { auditActionLabels } from '$lib/audit';

	// showList renders the list address on each event (instance-wide view).
	let { events, showList = false }: { events: AuditEvent[]; showList?: boolean } = $props();

	function fmtTime(iso: string): string {
		const d = new Date(iso);
		if (Number.isNaN(d.getTime())) return iso;
		return d.toLocaleString();
	}

	function actorLabel(e: AuditEvent): string {
		if (e.actor_kind === 'cli') {
			if (e.actor_detail) return `CLI operator (${e.actor_detail})`;
			return 'CLI operator';
		}
		return e.actor_email ?? 'unknown';
	}
</script>

{#if events.length === 0}
	<p class="text-sm text-muted-foreground">No audit events.</p>
{:else}
	<ul class="divide-y rounded-lg border">
		{#each events as e (e.id)}
			<li class="px-4 py-3">
				<div class="flex flex-wrap items-center gap-x-2 gap-y-1">
					<span class="text-sm font-medium">{auditActionLabels[e.action] ?? e.action}</span>
					{#if showList && e.list_addr}
						<span class="rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
							{e.list_addr}
						</span>
					{/if}
				</div>
				<div class="mt-1 text-sm text-muted-foreground">
					{actorLabel(e)} &middot; {fmtTime(e.at)}
				</div>
				{#if e.target}
					<div class="mt-0.5 text-xs text-muted-foreground">On: {e.target}</div>
				{/if}
				{#if e.detail}
					<div class="mt-0.5 text-xs text-muted-foreground">
						{e.action === 'settings.update' ? 'Changed:' : 'Details:'} {e.detail}
					</div>
				{/if}
			</li>
		{/each}
	</ul>
{/if}

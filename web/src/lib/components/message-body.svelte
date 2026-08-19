<script lang="ts">
	import MessageBody from './message-body.svelte';
	import type { ParsedMessage } from '$lib/types';

	let {
		msg,
		downloadPrefix,
		depth = 0
	}: {
		msg: ParsedMessage;
		downloadPrefix: string;
		depth?: number;
	} = $props();

	// Plain text by default when a text body exists, HTML otherwise (ADR
	// 0026). A toggle appears when both parts are present.
	let mode = $state(msg.text ? 'plain' : msg.html ? 'html' : 'plain');

	const MAX_DEPTH = 3;

	function cidMap(): Map<string, number> {
		const m = new Map<string, number>();
		for (const a of msg.attachments ?? []) {
			if (a.content_id) m.set(a.content_id, a.ordinal);
		}
		return m;
	}

	// cid: image references in the HTML body point at the matching attachment
	// part, served by the download endpoint (ADR 0026).
	function renderedHtml(): string {
		let html = msg.html ?? '';
		const map = cidMap();
		html = html.replace(/(src|href)=["']cid:([^"']+)["']/gi, (_attr, attr: string, cid: string) => {
			const ordinal = map.get(cid);
			if (ordinal === undefined) return `${attr}=""`;
			return `${attr}="${downloadPrefix}/${ordinal}"`;
		});
		return html;
	}

	function fmtSize(n: number): string {
		if (n >= 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
		if (n >= 1024) return `${Math.round(n / 1024)} KB`;
		return `${n} B`;
	}

	const hasBody = !!(msg.text || msg.html);
	const hasAttachments = (msg.attachments?.length ?? 0) > 0;
</script>

{#if hasBody && msg.text && msg.html}
	<div class="mb-2 flex justify-end">
		<div class="inline-flex rounded-md bg-muted p-0.5" role="group" aria-label="View mode">
			<button
				type="button"
				class="rounded px-2.5 py-1 text-xs font-medium transition-colors"
				class:bg-background={mode === 'plain'}
				class:text-foreground={mode === 'plain'}
				class:text-muted-foreground={mode !== 'plain'}
				class:shadow-sm={mode === 'plain'}
				onclick={() => (mode = 'plain')}
			>
				Plain text
			</button>
			<button
				type="button"
				class="rounded px-2.5 py-1 text-xs font-medium transition-colors"
				class:bg-background={mode === 'html'}
				class:text-foreground={mode === 'html'}
				class:text-muted-foreground={mode !== 'html'}
				class:shadow-sm={mode === 'html'}
				onclick={() => (mode = 'html')}
			>
				HTML
			</button>
		</div>
	</div>
{/if}

{#if mode === 'html' && msg.html}
	<!-- The HTML is sanitized server-side (ADR 0026). -->
	<div class="html-body text-sm leading-relaxed">{@html renderedHtml()}</div>
{:else if msg.text}
	<pre class="whitespace-pre-wrap break-words font-sans text-sm leading-relaxed">{msg.text}</pre>
{:else if hasBody && msg.html}
	<!-- html-only message renders by default (no text part to fall back to) -->
	<div class="html-body text-sm leading-relaxed">{@html renderedHtml()}</div>
{:else}
	<p class="text-sm text-muted-foreground">(no text body)</p>
{/if}

{#if hasAttachments}
	<div class="mt-4 border-t pt-3">
		<p class="text-xs font-medium uppercase tracking-wide text-muted-foreground">
			Attachments ({msg.attachments?.length})
		</p>
		<ul class="mt-2 space-y-1">
			{#each msg.attachments ?? [] as a (a.ordinal)}
				<li class="flex flex-wrap items-baseline gap-x-2 text-sm">
					<span class="text-muted-foreground">📎</span>
					{#if a.inline}
						<span class="break-all">{a.name}</span>
					{:else}
						<a
							href={`${downloadPrefix}/${a.ordinal}`}
							class="break-all font-medium text-primary underline-offset-4 hover:underline"
							>{a.name}</a
						>
					{/if}
					<span class="text-xs text-muted-foreground">{a.content_type} · {fmtSize(a.size)}</span>
				</li>
			{/each}
		</ul>
	</div>
{/if}

{#if (msg.nested?.length ?? 0) > 0}
	<div class="mt-4 space-y-3">
		{#each msg.nested ?? [] as n, i (i)}
			{#if depth >= MAX_DEPTH}
				<div class="rounded-md border bg-muted/30 px-3 py-2 text-sm text-muted-foreground">
					↳ Forwarded message{n.subject ? ` "${n.subject}"` : ''} — collapsed
				</div>
			{:else}
				<div class="rounded-md border border-muted bg-muted/10 p-3">
					<p class="text-xs text-muted-foreground">
						<span class="font-medium text-foreground">{n.from || '(unknown sender)'}</span>
						{#if n.subject} · <span class="italic">{n.subject}</span>{/if}
						{#if n.date} · {n.date}{/if}
					</p>
					<div class="mt-2">
						<MessageBody msg={n} downloadPrefix={downloadPrefix} depth={depth + 1} />
					</div>
				</div>
			{/if}
		{/each}
	</div>
{/if}

<style>
	/* Minimal typography for sanitized HTML bodies (Tailwind preflight resets
	   element margins, so restored here without a typography plugin). */
	.html-body :global(p),
	.html-body :global(ul),
	.html-body :global(ol),
	.html-body :global(blockquote),
	.html-body :global(pre),
	.html-body :global(table) {
		margin: 0.5em 0;
	}
	.html-body :global(h1),
	.html-body :global(h2),
	.html-body :global(h3),
	.html-body :global(h4) {
		font-weight: 600;
		margin: 0.7em 0 0.3em;
	}
	.html-body :global(ul) {
		list-style: disc;
		padding-left: 1.25rem;
	}
	.html-body :global(ol) {
		list-style: decimal;
		padding-left: 1.25rem;
	}
	.html-body :global(blockquote) {
		border-left: 3px solid hsl(var(--border));
		padding-left: 0.75rem;
		color: hsl(var(--muted-foreground));
	}
	.html-body :global(a) {
		color: hsl(var(--primary));
		text-decoration: underline;
	}
	.html-body :global(img) {
		max-width: 100%;
		height: auto;
	}
	.html-body :global(pre),
	.html-body :global(code) {
		background: hsl(var(--muted));
		border-radius: 0.25rem;
	}
	.html-body :global(pre) {
		padding: 0.75rem;
		overflow-x: auto;
	}
	.html-body :global(code) {
		padding: 0.1em 0.3em;
		font-size: 0.9em;
	}
</style>

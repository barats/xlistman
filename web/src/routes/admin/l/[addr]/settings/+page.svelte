<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { ApiError, getConsoleSettings, updateConsoleSettings } from '$lib/api';
	import type { ConsoleSettings } from '$lib/types';
	import { Button } from '$lib/components/ui/button';
	import { Card } from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import { Select } from '$lib/components/ui/select';
	import { Skeleton } from '$lib/components/ui/skeleton';

	const addr = page.params.addr ?? '';
	const at = addr.indexOf('@');
	const listName = addr.slice(0, at);
	const domain = addr.slice(at + 1);

	let data = $state<ConsoleSettings | null>(null);
	let phase: 'loading' | 'loaded' | 'denied' | 'error' = $state('loading');
	let error = $state('');
	let saved = $state('');
	let busy = $state(false);

	onMount(async () => {
		try {
			data = await getConsoleSettings(domain, listName);
			phase = 'loaded';
		} catch (e) {
			if (e instanceof ApiError && (e.status === 401 || e.status === 403)) {
				phase = 'denied';
			} else {
				phase = 'error';
				error = e instanceof Error ? e.message : 'Could not load settings.';
			}
		}
	});

	async function save() {
		if (!data) return;
		busy = true;
		error = '';
		saved = '';
		try {
			await updateConsoleSettings(domain, listName, data);
			saved = 'Settings saved.';
		} catch (e) {
			error = e instanceof Error ? e.message : 'Could not save settings.';
		} finally {
			busy = false;
		}
	}
</script>

{#if phase === 'denied'}
	<Card class="p-6">
		<h2 class="text-lg font-semibold">Owner only</h2>
		<p class="mt-1 text-sm text-muted-foreground">
			Only owners can change a list's settings.
		</p>
	</Card>
{:else if phase === 'loading' || !data}
	<div class="space-y-3">
		<Skeleton class="h-6 w-1/2" />
		<Skeleton class="h-40 w-full" />
	</div>
{:else if phase === 'error'}
	<p class="text-sm text-destructive">{error}</p>
{:else}
	<form class="space-y-6" onsubmit={(e) => { e.preventDefault(); save(); }}>
		<Card class="p-6">
			<h2 class="text-lg font-semibold">Identity</h2>
			<div class="mt-4 space-y-4">
				<div class="space-y-1.5">
					<Label for="description">Description</Label>
					<Input id="description" bind:value={data.description} placeholder="What is this list for?" />
				</div>
				<div class="space-y-1.5">
					<Label for="instructions">Instructions</Label>
					<textarea
						id="instructions"
						class="flex min-h-28 w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-sm transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
						placeholder="Multi-line guidance shown on the public list page, e.g. how to post or what the list is for."
						bind:value={data.instructions}
					></textarea>
				</div>
				<div class="space-y-1.5">
					<Label for="subject-prefix">Subject prefix</Label>
					<Input
						id="subject-prefix"
						bind:value={data.settings.subject_prefix}
						placeholder="e.g. [dev]"
					/>
				</div>
			</div>
		</Card>

		<Card class="p-6">
			<h2 class="text-lg font-semibold">Posting & delivery</h2>
			<div class="mt-2 divide-y">
				{#if data.list_type === 'discussion'}
					<label class="flex items-center justify-between gap-4 py-3">
						<div>
							<p class="text-sm font-medium">Moderation</p>
							<p class="text-sm text-muted-foreground">
								Hold all posts for approval instead of delivering subscriber posts directly.
							</p>
						</div>
						<input
							type="checkbox"
							class="size-4"
							bind:checked={data.settings.moderation_enabled}
						/>
					</label>
				{/if}
				<label class="flex items-center justify-between gap-4 py-3">
					<div>
						<p class="text-sm font-medium">Footer</p>
						<p class="text-sm text-muted-foreground">
							Append an unsubscribe footer to delivered posts.
						</p>
					</div>
					<input type="checkbox" class="size-4" bind:checked={data.settings.footer_enabled} />
				</label>
				<label class="flex items-center justify-between gap-4 py-3">
					<div>
						<p class="text-sm font-medium">Attachments</p>
						<p class="text-sm text-muted-foreground">
							Allow posts to carry attachments (files and inline images). When off, any
							post with an attachment is rejected.
						</p>
					</div>
					<input type="checkbox" class="size-4" bind:checked={data.settings.allow_attachments} />
				</label>
				<div class="grid gap-4 py-3 sm:grid-cols-2">
					<div class="space-y-1.5">
						<Label for="max-attachment-size">Max attachment size (bytes, 0 = unlimited)</Label>
						<Input
							id="max-attachment-size"
							type="number"
							min="0"
							disabled={!data.settings.allow_attachments}
							bind:value={data.settings.max_attachment_size}
						/>
					</div>
				</div>
				<div class="grid gap-4 py-3 sm:grid-cols-2">
					<div class="space-y-1.5">
						<Label for="max-message-size">Max message size (bytes)</Label>
						<Input
							id="max-message-size"
							type="number"
							min="0"
							bind:value={data.settings.max_message_size}
						/>
					</div>
					<div class="space-y-1.5">
						<Label for="reply-to-mode">Reply-To</Label>
						<Select id="reply-to-mode" bind:value={data.settings.reply_to_mode}>
							<option value="list">The list</option>
							<option value="sender">The sender</option>
							<option value="specified">A specified address</option>
						</Select>
					</div>
				</div>
				{#if data.settings.reply_to_mode === 'specified'}
					<div class="space-y-1.5 pb-3">
						<Label for="reply-to-address">Reply-To address</Label>
						<Input id="reply-to-address" bind:value={data.settings.reply_to_address} />
					</div>
				{/if}
			</div>
		</Card>

		<Card class="p-6">
			<h2 class="text-lg font-semibold">Subscription & status</h2>
			<div class="mt-2 divide-y">
				<div class="grid gap-4 py-3 sm:grid-cols-2">
					<div class="space-y-1.5">
						<Label for="subscription-policy">Subscription policy</Label>
						<Select
							id="subscription-policy"
							bind:value={data.settings.subscription_policy}
						>
							<option value="open">Open · new subscribers join immediately</option>
							<option value="moderated">Moderated · owner approves each join</option>
							<option value="closed">Closed · owners add members manually</option>
						</Select>
					</div>
					<div class="space-y-1.5">
						<Label for="digest-frequency">Digest frequency</Label>
						<Select
							id="digest-frequency"
							bind:value={data.settings.digest_frequency}
						>
							<option value="daily">Daily</option>
							<option value="weekly">Weekly</option>
						</Select>
					</div>
				</div>
				<div class="grid gap-4 py-3 sm:grid-cols-2">
					<div class="space-y-1.5">
						<Label for="held-expiry-days">Held request/message expiry (days)</Label>
						<Input
							id="held-expiry-days"
							type="number"
							min="0"
							bind:value={data.settings.held_expiry_days}
						/>
					</div>
					<div class="space-y-1.5">
						<Label for="bounce-threshold">Bounces before auto-disable</Label>
						<Input
							id="bounce-threshold"
							type="number"
							min="0"
							bind:value={data.settings.bounce_threshold}
						/>
					</div>
				</div>
				<div class="space-y-1.5 py-3">
					<Label for="archive-max-age">Archive retention (days, 0 = unlimited)</Label>
					<Input
						id="archive-max-age"
						type="number"
						min="0"
						bind:value={data.settings.archive_max_age_days}
					/>
				</div>
				<label class="flex items-center justify-between gap-4 py-3">
					<div>
						<p class="text-sm font-medium">Owner auto-disable notice</p>
						<p class="text-sm text-muted-foreground">
							Notify owners when a member is auto-disabled by bounces.
						</p>
					</div>
					<input
						type="checkbox"
						class="size-4"
						bind:checked={data.settings.owner_auto_disable_notice}
					/>
				</label>
			</div>
		</Card>

		<Card class="p-6">
			<h2 class="text-lg font-semibold">Email notices</h2>
			<p class="mt-1 text-sm text-muted-foreground">
				Customize each notice's subject and body. Leave a field empty to use the default text.
				Placeholders: <code class="rounded bg-muted px-1">{'{list}'}</code> (list address),{' '}
				<code class="rounded bg-muted px-1">{'{email}'}</code> (recipient),{' '}
				<code class="rounded bg-muted px-1">{'{url}'}</code> (web UI), and for the held notice{' '}
				<code class="rounded bg-muted px-1">{'{subject}'}</code> (the post subject).
			</p>
			<div class="mt-2 divide-y">
				<div class="py-3">
					<label class="flex items-center justify-between gap-4">
						<div>
							<p class="text-sm font-medium">Welcome email</p>
							<p class="text-sm text-muted-foreground">Sent when a subscription activates.</p>
						</div>
						<input type="checkbox" class="size-4" bind:checked={data.settings.welcome_email} />
					</label>
					<div class="mt-3 grid gap-3 sm:grid-cols-2">
						<div class="space-y-1.5">
							<Label for="notice-subject-welcome">Subject</Label>
							<Input id="notice-subject-welcome" bind:value={data.settings.welcome_subject} />
						</div>
					</div>
					<div class="mt-3 space-y-1.5">
						<Label for="notice-body-welcome">Body</Label>
						<textarea
							id="notice-body-welcome"
							class="flex min-h-24 w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-sm transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
							bind:value={data.settings.welcome_body}
						></textarea>
					</div>
				</div>
				<div class="py-3">
					<label class="flex items-center justify-between gap-4">
						<div>
							<p class="text-sm font-medium">Goodbye email</p>
							<p class="text-sm text-muted-foreground">Sent when a member is removed.</p>
						</div>
						<input type="checkbox" class="size-4" bind:checked={data.settings.goodbye_email} />
					</label>
					<div class="mt-3 grid gap-3 sm:grid-cols-2">
						<div class="space-y-1.5">
							<Label for="notice-subject-goodbye">Subject</Label>
							<Input id="notice-subject-goodbye" bind:value={data.settings.goodbye_subject} />
						</div>
					</div>
					<div class="mt-3 space-y-1.5">
						<Label for="notice-body-goodbye">Body</Label>
						<textarea
							id="notice-body-goodbye"
							class="flex min-h-24 w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-sm transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
							bind:value={data.settings.goodbye_body}
						></textarea>
					</div>
				</div>
				<div class="py-3">
					<label class="flex items-center justify-between gap-4">
						<div>
							<p class="text-sm font-medium">Sender held notice</p>
							<p class="text-sm text-muted-foreground">Sent when a post awaits approval.</p>
						</div>
						<input type="checkbox" class="size-4" bind:checked={data.settings.sender_held_notice} />
					</label>
					<div class="mt-3 grid gap-3 sm:grid-cols-2">
						<div class="space-y-1.5">
							<Label for="notice-subject-sender-held">Subject</Label>
							<Input id="notice-subject-sender-held" bind:value={data.settings.sender_held_subject} />
						</div>
					</div>
					<div class="mt-3 space-y-1.5">
						<Label for="notice-body-sender-held">Body</Label>
						<textarea
							id="notice-body-sender-held"
							class="flex min-h-24 w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-sm transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
							bind:value={data.settings.sender_held_body}
						></textarea>
					</div>
				</div>
			</div>
		</Card>

		{#if error}
			<p class="text-sm text-destructive">{error}</p>
		{/if}
		{#if saved}
			<p class="rounded-md border border-success/40 bg-success/10 px-3 py-2 text-sm">{saved}</p>
		{/if}
		<Button type="submit" disabled={busy}>Save settings</Button>
	</form>
{/if}

<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, formatCents, statusLabel } from '$lib/api';
	import { session } from '$lib/session.svelte';
	import type { SwagRequest } from '$lib/types';

	let requests = $state<SwagRequest[] | null>(null);
	let error = $state('');

	async function load() {
		try {
			requests = (await api.get<{ requests: SwagRequest[] }>('/api/requests')).requests;
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Could not load your requests.';
		}
	}
	onMount(load);

	async function cancel(id: number) {
		if (!confirm('Cancel this request?')) return;
		try {
			await api.post(`/api/requests/${id}/cancel`);
			await load();
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Could not cancel.';
		}
	}
</script>

<h1>My requests</h1>
{#if !session.me?.user}
	<p><a class="button" href="/auth/login" data-sveltekit-reload>Sign in with Hack Club</a></p>
{:else}
	{#if error}<p class="error">{error}</p>{/if}
	{#each requests ?? [] as r (r.id)}
		<div class="card" style="margin-bottom: 1rem">
			<div class="row">
				<strong>#{r.id}</strong>
				<span class="badge {r.status}">{statusLabel[r.status]}</span>
				<span class="muted">{new Date(r.created_at).toLocaleDateString()}</span>
			</div>
			<ul>
				{#each r.lines as l}<li>{l.quantity} × {l.name}</li>{/each}
			</ul>
			{#if r.status === 'awaiting_payment'}
				<p>
					Shipping to {r.address.country} costs {formatCents(r.shipping_fee_cents)}.
					{#if r.payment_url}<a class="button" href={r.payment_url}>Pay shipping on HCB</a>{/if}
				</p>
			{/if}
			{#if r.mailed_at}<p>Mailed {new Date(r.mailed_at).toLocaleDateString()}</p>{/if}
			{#if r.tracking_number}<p>Tracking: {r.carrier} {r.tracking_number}</p>{/if}
			{#if r.admin_note}<p class="muted">Note from Hack Club: {r.admin_note}</p>{/if}
			{#if r.status === 'pending' || r.status === 'awaiting_payment'}
				<button class="link" onclick={() => cancel(r.id)}>Cancel request</button>
			{/if}
		</div>
	{:else}
		{#if requests}<p class="muted">No requests yet. <a href="/">Pick some swag!</a></p>{/if}
	{/each}
{/if}

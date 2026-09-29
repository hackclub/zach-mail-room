<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError, formatCents, isInternational } from '$lib/api';
	import { cartErrors, cartLines, emptyCart, setQuantity, totalQuantity, type Cart } from '$lib/cart';
	import { session } from '$lib/session.svelte';
	import type { Address, Catalog, SwagRequest } from '$lib/types';

	let catalog = $state<Catalog | null>(null);
	let cart = $state<Cart>(emptyCart());
	let address = $state<Address>({
		first_name: '', last_name: '', line_1: '', line_2: '', city: '', state: '', postal_code: '', country: 'US', phone_number: ''
	});
	let note = $state('');
	let error = $state('');
	let submitting = $state(false);

	onMount(async () => {
		try {
			catalog = await api.get<Catalog>('/api/catalog');
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Could not load swag.';
		}
	});

	const problems = $derived(catalog ? cartErrors(cart, catalog.settings.max_items_per_request) : []);
	const intl = $derived(isInternational(address.country));

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		if (!catalog) return;
		error = '';
		submitting = true;
		try {
			const r = await api.post<SwagRequest>('/api/requests', {
				address: { ...address, country: address.country.trim().toUpperCase() },
				lines: cartLines(cart, catalog.items),
				note
			});
			if (r.payment_url) window.location.href = r.payment_url;
			else goto('/requests');
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Something went wrong.';
		} finally {
			submitting = false;
		}
	}
</script>

<h1>Hack Club swag</h1>
<p class="muted">
	Pick what you'd like and we'll mail it to you. Shipping inside the US is on us; outside the US we'll ask
	you to cover shipping through HCB.
</p>

{#if error}<p class="error">{error}</p>{/if}

{#if catalog}
	{#if !catalog.settings.requests_open}
		<p class="error">Requests are closed right now. Check back soon!</p>
	{/if}
	<section class="grid">
		{#each catalog.items as item (item.id)}
			<div class="card">
				{#if item.image_url}<img src={item.image_url} alt={item.name} loading="lazy" />{/if}
				<h3>{item.name}</h3>
				{#if item.description}<p class="muted">{item.description}</p>{/if}
				{#if session.me?.user && catalog.settings.requests_open}
					<label>
						Quantity (max {item.max_per_request})
						<input class="qty" type="number" min="0" max={item.max_per_request} value={cart[item.id] ?? 0}
							oninput={(e) => (cart = setQuantity(cart, item, +e.currentTarget.value))} />
					</label>
				{/if}
			</div>
		{:else}
			<p class="muted">Nothing listed yet.</p>
		{/each}
	</section>

	{#if !session.me?.user}
		<p><a class="button" href="/auth/login" data-sveltekit-reload>Sign in with Hack Club to request swag</a></p>
	{:else if catalog.settings.requests_open && totalQuantity(cart) > 0}
		<form class="card" style="margin-top: 1.5rem" onsubmit={submit}>
			<h2>Where should we send it?</h2>
			<div class="form-grid">
				<label>First name <input required bind:value={address.first_name} autocomplete="given-name" /></label>
				<label>Last name <input required bind:value={address.last_name} autocomplete="family-name" /></label>
				<label>Address line 1 <input required bind:value={address.line_1} autocomplete="address-line1" /></label>
				<label>Address line 2 <input bind:value={address.line_2} autocomplete="address-line2" /></label>
				<label>City <input required bind:value={address.city} autocomplete="address-level2" /></label>
				<label>State / province <input bind:value={address.state} autocomplete="address-level1" /></label>
				<label>Postal code <input required bind:value={address.postal_code} autocomplete="postal-code" /></label>
				<label>Country (2-letter code) <input required maxlength="2" bind:value={address.country} autocomplete="country" /></label>
				<label>Phone {intl ? '(required for customs)' : '(optional)'}
					<input required={intl} bind:value={address.phone_number} autocomplete="tel" /></label>
			</div>
			<label style="margin-top: 0.75rem">Anything we should know? <textarea bind:value={note} rows="2"></textarea></label>
			{#if intl && catalog.settings.international_shipping_fee_cents > 0}
				<p>
					International shipping is <strong>{formatCents(catalog.settings.international_shipping_fee_cents)}</strong>.
					After you submit we'll send you to HCB to pay it; we ship once it's received.
				</p>
			{/if}
			{#each problems as p}<p class="error">{p}</p>{/each}
			<button disabled={submitting || problems.length > 0}>
				{submitting ? 'Sending…' : `Request ${totalQuantity(cart)} item${totalQuantity(cart) === 1 ? '' : 's'}`}
			</button>
		</form>
	{/if}
{:else if !error}
	<p class="muted">Loading swag…</p>
{/if}

<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, formatCents, statusLabel } from '$lib/api';
	import { session } from '$lib/session.svelte';
	import type { Item, Settings, Submission, SwagRequest, WarehouseSKU } from '$lib/types';

	type Tab = 'requests' | 'items' | 'settings' | 'submissions';
	let tab = $state<Tab>('requests');
	let error = $state('');
	let flash = $state('');

	let statusFilter = $state('pending');
	let requests = $state<SwagRequest[]>([]);
	let items = $state<Item[]>([]);
	let skus = $state<WarehouseSKU[]>([]);
	let settings = $state<Settings | null>(null);
	let submissions = $state<Submission[]>([]);

	async function guard(fn: () => Promise<unknown>, ok = '') {
		error = flash = '';
		try {
			await fn();
			flash = ok;
		} catch (e) {
			error = e instanceof ApiError ? e.message : String(e);
		}
	}

	const loadRequests = () =>
		guard(async () => {
			const q = statusFilter ? `?status=${statusFilter}` : '';
			requests = (await api.get<{ requests: SwagRequest[] }>(`/api/admin/requests${q}`)).requests;
		});
	const loadItems = () =>
		guard(async () => {
			items = (await api.get<{ items: Item[] }>('/api/admin/items')).items;
		});
	const loadSKUs = () =>
		guard(async () => {
			skus = (await api.get<{ skus: WarehouseSKU[] }>('/api/admin/warehouse/skus')).skus;
		});
	const loadSettings = () =>
		guard(async () => {
			settings = await api.get<Settings>('/api/admin/settings');
		});
	const loadSubmissions = () =>
		guard(async () => {
			submissions = (await api.get<{ submissions: Submission[] }>('/api/admin/submissions')).submissions;
		});

	onMount(() => {
		if (!session.me?.roles.admin) return;
		loadRequests();
		loadItems();
		loadSettings();
		loadSubmissions();
	});

	const act = (id: number, action: string, body: object = {}) =>
		guard(async () => {
			await api.post(`/api/admin/requests/${id}/${action}`, body);
			await loadRequests();
		}, `Request #${id}: ${action} done`);

	function reject(id: number) {
		const note = prompt('Reason (shown to the requester):') ?? '';
		act(id, 'reject', { note });
	}

	// ---- items ----
	const listedSKUs = $derived(new Set(items.map((i) => i.sku)));
	const saveItem = (it: Item) =>
		guard(async () => {
			await api.put(`/api/admin/items/${it.id}`, it);
			await loadItems();
		}, `Saved ${it.name}`);
	const listSKU = (s: WarehouseSKU) =>
		guard(async () => {
			await api.post('/api/admin/items', { sku: s.sku, name: s.name, max_per_request: 1, visible: false });
			await loadItems();
		}, `Listed ${s.name} (hidden until you make it visible)`);

	const saveSettings = () =>
		guard(async () => {
			settings = await api.put<Settings>('/api/admin/settings', settings);
		}, 'Settings saved');

	const saveSubmission = (s: Submission) =>
		guard(async () => {
			await api.put(`/api/admin/submissions/${s.id}`, { status: s.status, admin_note: s.admin_note, sku: s.sku });
			await loadSubmissions();
		}, `Saved submission #${s.id}`);
</script>

<h1>Admin</h1>
{#if !session.me?.roles.admin}
	<p class="error">Admins only.</p>
{:else}
	<div class="row" style="margin-bottom: 1rem">
		{#each ['requests', 'items', 'settings', 'submissions'] as t}
			<button class:secondary={tab !== t} onclick={() => (tab = t as Tab)}>{t}</button>
		{/each}
	</div>
	{#if error}<p class="error">{error}</p>{/if}
	{#if flash}<p class="ok">{flash}</p>{/if}

	{#if tab === 'requests'}
		<div class="row">
			<select bind:value={statusFilter} onchange={loadRequests} style="width: auto">
				<option value="">All</option>
				{#each ['pending', 'awaiting_payment', 'dispatched', 'rejected', 'cancelled'] as s}
					<option value={s}>{statusLabel[s]}</option>
				{/each}
			</select>
		</div>
		<table>
			<thead><tr><th>#</th><th>Who</th><th>Items</th><th>Ship to</th><th>Status</th><th></th></tr></thead>
			<tbody>
				{#each requests as r (r.id)}
					<tr>
						<td>{r.id}<div class="muted">{new Date(r.created_at).toLocaleDateString()}</div>
							{#if r.source !== 'app'}<span class="badge" title={r.airtable_record_id}>imported · {r.source}</span>{/if}</td>
						<td>{r.user_email}{#if r.note}<div class="muted">“{r.note}”</div>{/if}
							{#if r.internal_note}<details><summary class="muted">internal note</summary><pre style="white-space: pre-wrap">{r.internal_note}</pre></details>{/if}</td>
						<td>{#each r.lines as l}<div>{l.quantity} × {l.name} <span class="muted">{l.sku}</span></div>{/each}</td>
						<td>{r.address.first_name} {r.address.last_name}<br />{r.address.city}, {r.address.state} {r.address.country}</td>
						<td>
							<span class="badge {r.status}">{statusLabel[r.status]}</span>
							{#if r.shipping_fee_cents}<div class="muted">fee {formatCents(r.shipping_fee_cents)}{r.paid_at ? ' (paid)' : ''}</div>{/if}
							{#if r.mailed_at}<div class="muted">mailed {new Date(r.mailed_at).toLocaleDateString()}</div>{/if}
							{#if r.tracking_number}<div class="muted">{r.carrier} {r.tracking_number}</div>{/if}
						</td>
						<td class="row">
							{#if r.status === 'awaiting_payment'}<button onclick={() => act(r.id, 'mark-paid')}>Mark paid</button>{/if}
							{#if r.status === 'pending' && r.source === 'app'}<button onclick={() => act(r.id, 'dispatch')}>Ship</button>{/if}
							{#if r.status === 'pending' || r.status === 'awaiting_payment'}<button class="secondary" onclick={() => reject(r.id)}>Reject</button>{/if}
							{#if r.status === 'dispatched' && r.theseus_order_id}<button class="secondary" onclick={() => act(r.id, 'refresh')}>Refresh tracking</button>{/if}
						</td>
					</tr>
				{:else}
					<tr><td colspan="6" class="muted">No requests.</td></tr>
				{/each}
			</tbody>
		</table>
	{:else if tab === 'items'}
		<h2>Listed items</h2>
		<table>
			<thead><tr><th>Item</th><th>Limits</th><th>Display</th><th></th></tr></thead>
			<tbody>
				{#each items as it (it.id)}
					<tr>
						<td>
							<input bind:value={it.name} /><div class="muted">{it.sku}</div>
							<input bind:value={it.description} placeholder="description" />
							<input bind:value={it.image_url} placeholder="image URL" />
						</td>
						<td>
							<label>Per request <input type="number" min="1" bind:value={it.max_per_request} /></label>
							<label>Per person, ever (blank = no cap)
								<input type="number" min="0" value={it.max_per_user ?? ''}
									oninput={(e) => (it.max_per_user = e.currentTarget.value === '' ? null : +e.currentTarget.value)} /></label>
						</td>
						<td>
							<label class="row"><input type="checkbox" style="width: auto" bind:checked={it.visible} /> Visible</label>
							<label>Sort order <input type="number" bind:value={it.sort_order} /></label>
						</td>
						<td><button onclick={() => saveItem(it)}>Save</button></td>
					</tr>
				{:else}
					<tr><td colspan="4" class="muted">No items listed. Add some from the warehouse below.</td></tr>
				{/each}
			</tbody>
		</table>
		<h2>Warehouse inventory (mail.hackclub.com)</h2>
		{#if skus.length === 0}
			<button class="secondary" onclick={loadSKUs}>Load warehouse SKUs</button>
		{:else}
			<table>
				<thead><tr><th>SKU</th><th>Name</th><th>In stock</th><th>Unit cost</th><th></th></tr></thead>
				<tbody>
					{#each skus as s (s.sku)}
						<tr>
							<td>{s.sku}</td><td>{s.name}</td><td>{s.in_stock}</td><td>${s.unit_cost}</td>
							<td>{#if listedSKUs.has(s.sku)}<span class="muted">listed</span>{:else}<button onclick={() => listSKU(s)}>List</button>{/if}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{/if}
	{:else if tab === 'settings' && settings}
		<form class="card" onsubmit={(e) => { e.preventDefault(); saveSettings(); }}>
			<div class="form-grid">
				<label class="row"><input type="checkbox" style="width: auto" bind:checked={settings.requests_open} /> Requests open</label>
				<label>Max items per request (0 = no cap) <input type="number" min="0" bind:value={settings.max_items_per_request} /></label>
				<label>Days between requests per person <input type="number" min="0" bind:value={settings.request_cooldown_days} /></label>
				<label>International shipping fee (cents) <input type="number" min="0" bind:value={settings.international_shipping_fee_cents} /></label>
			</div>
			<button style="margin-top: 0.75rem">Save settings</button>
		</form>
	{:else if tab === 'submissions'}
		<table>
			<thead><tr><th>Author</th><th>Material</th><th>Qty</th><th>Status / SKU / note</th><th></th></tr></thead>
			<tbody>
				{#each submissions as s (s.id)}
					<tr>
						<td>{s.user_email}<div class="muted">{s.program}</div></td>
						<td><a href={s.file_url} target="_blank" rel="noreferrer">{s.title}</a> <span class="muted">({s.kind})</span>
							{#if s.description}<div class="muted">{s.description}</div>{/if}</td>
						<td>{s.quantity}</td>
						<td>
							<select bind:value={s.status}>
								{#each ['submitted', 'approved', 'printing', 'stocked', 'rejected'] as st}<option value={st}>{statusLabel[st]}</option>{/each}
							</select>
							<input bind:value={s.sku} placeholder="warehouse SKU once stocked" />
							<input bind:value={s.admin_note} placeholder="note to author" />
						</td>
						<td><button onclick={() => saveSubmission(s)}>Save</button></td>
					</tr>
				{:else}
					<tr><td colspan="5" class="muted">No submissions yet.</td></tr>
				{/each}
			</tbody>
		</table>
	{/if}
{/if}

<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, statusLabel } from '$lib/api';
	import { session } from '$lib/session.svelte';
	import type { Submission } from '$lib/types';

	let subs = $state<Submission[]>([]);
	let error = $state('');
	let saved = $state('');
	let form = $state({ program: '', title: '', description: '', kind: 'sticker', file_url: '', quantity: 100 });

	async function load() {
		subs = (await api.get<{ submissions: Submission[] }>('/api/author/submissions')).submissions;
	}
	onMount(() => {
		if (session.me?.roles.author) load().catch((e) => (error = e.message));
	});

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		error = saved = '';
		try {
			await api.post('/api/author/submissions', form);
			saved = 'Submitted! We will follow up about printing.';
			form = { ...form, title: '', description: '', file_url: '' };
			await load();
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Could not submit.';
		}
	}
</script>

<h1>Stock the warehouse</h1>
{#if !session.me?.roles.author}
	<p class="muted">
		This page is for YSWS authors. We recognize you by your email in the YSWS Authors table of the Unified
		YSWS DB. If you run a YSWS and can't see this, ask to have your Hack Club Auth email added there.
	</p>
{:else}
	<p class="muted">
		Submit print-ready material (stickers, posters, cards…) for your YSWS. Once approved we'll print it,
		stock it in the warehouse, and it can be shipped with your program's orders.
	</p>
	<form class="card" onsubmit={submit}>
		<div class="form-grid">
			<label>YSWS program <input required bind:value={form.program} placeholder="e.g. Sprig" /></label>
			<label>Title <input required bind:value={form.title} placeholder="e.g. Sprig holographic sticker" /></label>
			<label>Kind
				<select bind:value={form.kind}>
					<option value="sticker">Sticker</option>
					<option value="poster">Poster</option>
					<option value="card">Card / flyer</option>
					<option value="other">Other</option>
				</select>
			</label>
			<label>Quantity <input type="number" min="1" required bind:value={form.quantity} /></label>
		</div>
		<label style="margin-top: 0.75rem">Link to print-ready file
			<input type="url" required bind:value={form.file_url} placeholder="https://cdn.hackclub.com/…" /></label>
		<label style="margin-top: 0.75rem">Notes (size, finish, deadline…)
			<textarea rows="3" bind:value={form.description}></textarea></label>
		{#if error}<p class="error">{error}</p>{/if}
		{#if saved}<p class="ok">{saved}</p>{/if}
		<button style="margin-top: 0.75rem">Submit</button>
	</form>

	<h2>Your submissions</h2>
	<table>
		<thead><tr><th>Program</th><th>Title</th><th>Qty</th><th>Status</th><th>SKU</th></tr></thead>
		<tbody>
			{#each subs as s (s.id)}
				<tr>
					<td>{s.program}</td>
					<td><a href={s.file_url} target="_blank" rel="noreferrer">{s.title}</a>
						{#if s.admin_note}<div class="muted">{s.admin_note}</div>{/if}</td>
					<td>{s.quantity}</td>
					<td><span class="badge {s.status}">{statusLabel[s.status]}</span></td>
					<td>{s.sku}</td>
				</tr>
			{:else}
				<tr><td colspan="5" class="muted">Nothing submitted yet.</td></tr>
			{/each}
		</tbody>
	</table>
{/if}

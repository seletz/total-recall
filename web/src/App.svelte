<script lang="ts">
	import { onMount } from 'svelte';
	import { api, token, type Status } from './lib/api.ts';
	import { subscribe } from './lib/sse.ts';
	import Search from './views/Search.svelte';
	import Stats from './views/Stats.svelte';

	// Routing by path: the Go server answers every unknown path with
	// index.html, so /stats loads this app as well.
	type View = 'search' | 'stats';
	const views: { id: View; path: string; label: string }[] = [
		{ id: 'search', path: '/', label: 'Search' },
		{ id: 'stats', path: '/stats', label: 'Stats' }
	];
	function viewOf(path: string): View {
		return path.startsWith('/stats') ? 'stats' : 'search';
	}

	let view = $state<View>(viewOf(location.pathname));
	let hasToken = $state(token() !== null);
	let status = $state<Status | null>(null);
	let live = $state(false);
	// Bumped on every change of the archive; the views reload on it.
	let generation = $state(0);
	let error = $state('');

	function go(v: View, e?: MouseEvent) {
		e?.preventDefault();
		if (v === view) return;
		history.pushState(null, '', views.find((x) => x.id === v)!.path);
		view = v;
	}

	async function loadStatus() {
		try {
			status = await api<Status>('status');
			error = '';
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
		}
	}

	onMount(() => {
		const pop = () => (view = viewOf(location.pathname));
		addEventListener('popstate', pop);
		if (!hasToken) return () => removeEventListener('popstate', pop);
		void loadStatus();
		const stop = subscribe(
			() => {
				generation += 1;
				void loadStatus();
			},
			(l) => (live = l)
		);
		return () => {
			removeEventListener('popstate', pop);
			stop();
		};
	});

	function lastIngest(s: Status): string {
		const ts = Object.values(s.last_ingest).sort().at(-1);
		return ts ? new Date(ts).toLocaleString() : 'never';
	}
</script>

<header class="top">
	<h1>total-recall</h1>
	<nav>
		{#each views as v (v.id)}
			<a href={v.path} class:active={view === v.id} onclick={(e) => go(v.id, e)}>{v.label}</a>
		{/each}
	</nav>
	<div class="status">
		{#if status}
			<span>{status.sessions.toLocaleString()} sessions · {status.events.toLocaleString()} events</span>
			<span>last ingest {lastIngest(status)}</span>
			<span class="mono">{status.version}</span>
		{/if}
		<span class="dot" class:live title={live ? 'live updates connected' : 'live updates off'}></span>
	</div>
</header>

<main>
	{#if !hasToken}
		<p class="error">
			No token. Open the URL that <code>total-recall serve</code> printed; it carries the token in
			its fragment.
		</p>
	{:else}
		{#if error}
			<p class="error">{error}</p>
		{/if}
		{#if view === 'search'}
			<Search {generation} />
		{:else}
			<Stats {generation} timezone={status?.timezone ?? ''} />
		{/if}
	{/if}
</main>

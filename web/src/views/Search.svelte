<script lang="ts">
	import { api, type Hit } from '../lib/api.ts';
	import { splitSnippet } from '../lib/snippet.ts';

	let { generation }: { generation: number } = $props();

	const actors = ['', 'human', 'agent', 'peer', 'automation', 'tool'];
	let query = $state('');
	let actor = $state('');
	let hits = $state<Hit[]>([]);
	let took = $state(0);
	let error = $state('');
	let searching = $state(false);
	let seq = 0;
	let timer: ReturnType<typeof setTimeout> | undefined;
	let ctl: AbortController | undefined;

	async function run() {
		const q = query.trim();
		ctl?.abort();
		if (!q) {
			hits = [];
			error = '';
			return;
		}
		const mine = ++seq;
		ctl = new AbortController();
		searching = true;
		const t0 = performance.now();
		try {
			const out = await api<Hit[]>('search', { q, actor, limit: '100' }, ctl.signal);
			if (mine !== seq) return;
			hits = out;
			took = Math.round(performance.now() - t0);
			error = '';
		} catch (e) {
			if (mine !== seq || (e instanceof DOMException && e.name === 'AbortError')) return;
			error = e instanceof Error ? e.message : String(e);
		} finally {
			if (mine === seq) searching = false;
		}
	}

	// 150 ms after the last keystroke, like the TUI.
	function debounce() {
		clearTimeout(timer);
		timer = setTimeout(run, 150);
	}

	$effect(() => {
		void generation; // rerun on an archive change
		void actor;
		void run();
	});

	function time(ts: string): string {
		return new Date(ts).toLocaleString(undefined, {
			year: 'numeric',
			month: '2-digit',
			day: '2-digit',
			hour: '2-digit',
			minute: '2-digit'
		});
	}

	// Focus the input once; an autofocus attribute would trip the a11y check.
	function focusOnMount(el: HTMLInputElement) {
		el.focus();
	}

	function shortRepo(r: string | null): string {
		if (!r) return '';
		return r.replace(/^https?:\/\/[^/]+\//, '').replace(/\.git$/, '');
	}
</script>

<div class="toolbar">
	<input
		type="search"
		placeholder="Search the archive…"
		bind:value={query}
		oninput={debounce}
		spellcheck="false"
		{@attach focusOnMount}
	/>
	<select bind:value={actor} aria-label="actor">
		{#each actors as a (a)}
			<option value={a}>{a || 'all actors'}</option>
		{/each}
	</select>
	<span class="hint">
		{#if searching}
			searching…
		{:else if query.trim()}
			{hits.length}{hits.length === 100 ? '+' : ''} hits ({took} ms)
		{:else}
			every word must occur; results update while you type
		{/if}
	</span>
</div>

{#if error}
	<p class="error">{error}</p>
{/if}

<ul class="hits">
	{#each hits as h (h.event_id)}
		<li>
			<div class="meta">
				<span>{time(h.ts)}</span>
				<span class="name">{h.session_name ?? h.session_id}</span>
				<span>{shortRepo(h.repo)}</span>
				<span class="badge {h.actor}">{h.actor}</span>
				<span class="mono">{h.session_id}</span>
			</div>
			<div class="snippet">
				{#each splitSnippet(h.snippet) as p, i (i)}
					{#if p.hit}<mark>{p.text}</mark>{:else}{p.text}{/if}
				{/each}
			</div>
		</li>
	{/each}
</ul>

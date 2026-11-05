<script lang="ts">
	import { api, type Series } from '../lib/api.ts';
	import { colorOf, layout, metricOf, shortLabel, type Metric } from '../lib/chart.ts';

	let { generation, timezone }: { generation: number; timezone: string } = $props();

	const periods = ['day', 'week', 'month'] as const;
	const dims = ['project', 'agent', 'directory'] as const;
	const metrics: { id: Metric; label: string }[] = [
		{ id: 'events', label: 'events' },
		{ id: 'human', label: 'human' },
		{ id: 'sessions', label: 'sessions' }
	];
	let period = $state<(typeof periods)[number]>('week');
	let by = $state<(typeof dims)[number]>('project');
	let metric = $state<Metric>('events');
	let series = $state<Series | null>(null);
	let error = $state('');
	let took = $state(0);
	let seq = 0;

	async function load() {
		const mine = ++seq;
		const t0 = performance.now();
		try {
			const out = await api<Series>('activity', { period, by, top: '6' });
			if (mine !== seq) return;
			series = out;
			took = Math.round(performance.now() - t0);
			error = '';
		} catch (e) {
			if (mine !== seq) return;
			error = e instanceof Error ? e.message : String(e);
		}
	}

	$effect(() => {
		void generation;
		void period;
		void by;
		void load();
	});

	let l = $derived(series ? layout(series, metric) : null);
	let total = $derived(series ? metricOf(series.total, metric) : 0);

	function pct(v: number): string {
		return total ? `${Math.round((100 * v) / total)} %` : '';
	}
</script>

<div class="toolbar">
	<span class="seg">
		{#each periods as p (p)}
			<button class:on={period === p} onclick={() => (period = p)}>{p}</button>
		{/each}
	</span>
	<span class="seg">
		{#each dims as d (d)}
			<button class:on={by === d} onclick={() => (by = d)}>{d}</button>
		{/each}
	</span>
	<span class="seg">
		{#each metrics as m (m.id)}
			<button class:on={metric === m.id} onclick={() => (metric = m.id)}>{m.label}</button>
		{/each}
	</span>
	<span class="hint">
		{#if series}
			{series.buckets.length} {period}s to {series.until?.slice(0, 10)} · {timezone} · {took} ms
		{/if}
	</span>
</div>

{#if error}
	<p class="error">{error}</p>
{/if}

{#if series && l}
	<svg class="chart" viewBox="0 0 {l.width} {l.height}" role="img" aria-label="activity per {period}">
		{#each l.ticks as t (t.value)}
			<line class="axis" x1={l.left} x2={l.width - 8} y1={t.y} y2={t.y} />
			<text x={l.left - 6} y={t.y + 4} text-anchor="end">{t.value}</text>
		{/each}
		{#each l.bars as b (b.label)}
			{#each b.segments as s (s.key)}
				<rect x={b.x} y={s.y} width={b.w} height={s.h} fill={s.color}>
					<title>{b.label} {s.key}: {s.value}</title>
				</rect>
			{/each}
			{#if l.bars.length <= 32 || l.bars.indexOf(b) % 2 === 0}
				<text x={b.x + b.w / 2} y={l.height - 6} text-anchor="middle">{shortLabel(b.label, period)}</text>
			{/if}
		{/each}
	</svg>

	<div class="legend">
		{#each series.keys as k, i (k)}
			<span><span class="sw" style="background: {colorOf(k, i)}"></span>{k}</span>
		{/each}
	</div>

	<table class="stats">
		<thead>
			<tr><th>{by}</th><th class="n">sessions</th><th class="n">human</th><th class="n">agent</th><th class="n">share</th></tr>
		</thead>
		<tbody>
			{#each series.by_key as c (c.key)}
				<tr>
					<td>{c.key}</td>
					<td class="n">{c.sessions.toLocaleString()}</td>
					<td class="n">{c.human.toLocaleString()}</td>
					<td class="n">{c.agent.toLocaleString()}</td>
					<td class="n">{pct(metricOf(c, metric))}</td>
				</tr>
			{/each}
			<tr>
				<td><strong>total</strong></td>
				<td class="n">{series.total.sessions.toLocaleString()}</td>
				<td class="n">{series.total.human.toLocaleString()}</td>
				<td class="n">{series.total.agent.toLocaleString()}</td>
				<td class="n"></td>
			</tr>
		</tbody>
	</table>
{/if}

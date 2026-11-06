// Layout of the stacked bar chart of the stats view: pure functions over
// the series JSON, so the SVG in Stats.svelte only draws rectangles.

import type { Cell, Series } from './api.ts';

export type Metric = 'events' | 'human' | 'sessions';

export type Segment = { key: string; color: string; y: number; h: number; value: number };

export type Bar = { label: string; x: number; w: number; total: number; segments: Segment[] };

export type Layout = {
	width: number;
	height: number;
	bars: Bar[];
	max: number;
	ticks: { y: number; value: number }[];
	left: number;
	bottom: number;
};

// Six series colours plus grey for "other" (the --top fold); the
// palette matches the CSS variables of app.css.
export const COLORS = ['#4b6bfb', '#2e9e6b', '#e0853c', '#b84cb0', '#2aa7c4', '#d9534f'];
export const OTHER_COLOR = '#9aa0a6';
export const OTHER_KEY = 'other';

/** Colour of the key at position i of the series keys. */
export function colorOf(key: string, i: number): string {
	return key === OTHER_KEY ? OTHER_COLOR : COLORS[i % COLORS.length];
}

/** Value of a cell for the metric. */
export function metricOf(c: { human: number; agent: number; sessions: number }, m: Metric): number {
	switch (m) {
		case 'human':
			return c.human;
		case 'sessions':
			return c.sessions;
		default:
			return c.human + c.agent;
	}
}

/** Rounds max up to a friendly tick step and returns the tick values. */
export function ticksFor(max: number, count = 4): number[] {
	if (max <= 0) return [0];
	const raw = max / count;
	const mag = 10 ** Math.floor(Math.log10(raw));
	const step = [1, 2, 5, 10].map((f) => f * mag).find((s) => s >= raw) ?? mag * 10;
	const out: number[] = [];
	for (let v = 0; v <= max + 1e-9; v += step) out.push(v);
	if (out[out.length - 1] < max) out.push(out[out.length - 1] + step);
	return out;
}

/** Lays the series out as stacked bars in a width × height box. */
export function layout(s: Series, metric: Metric, width = 960, height = 260): Layout {
	const left = 44;
	const bottom = 22;
	const top = 8;
	const plotW = width - left - 8;
	const plotH = height - top - bottom;
	const n = s.buckets.length || 1;
	const slot = plotW / n;
	const w = Math.max(2, slot * 0.7);
	const totals = s.buckets.map((b) =>
		b.cells.length ? b.cells.reduce((a, c) => a + metricOf(c, metric), 0) : metricOf(b, metric)
	);
	const tickValues = ticksFor(Math.max(0, ...totals));
	const max = tickValues[tickValues.length - 1] || 1;
	const scale = (v: number) => (v / max) * plotH;
	const index = new Map(s.keys.map((k, i) => [k, i]));
	const bars: Bar[] = s.buckets.map((b, i) => {
		let y = top + plotH;
		const cells: Cell[] = b.cells.length
			? b.cells
			: [{ key: '', human: b.human, agent: b.agent, sessions: b.sessions }];
		const segments: Segment[] = [];
		for (const c of cells) {
			const value = metricOf(c, metric);
			if (value <= 0) continue;
			const h = scale(value);
			y -= h;
			segments.push({ key: c.key, color: c.key ? colorOf(c.key, index.get(c.key) ?? 0) : COLORS[0], y, h, value });
		}
		return { label: b.label, x: left + i * slot + (slot - w) / 2, w, total: totals[i], segments };
	});
	return {
		width,
		height,
		bars,
		max,
		ticks: tickValues.map((v) => ({ y: top + plotH - scale(v), value: v })),
		left,
		bottom
	};
}

/** Short axis label: day → DD, week → Wnn, month → MM. */
export function shortLabel(label: string, period: Series['period']): string {
	switch (period) {
		case 'day':
			return label.slice(8);
		case 'week':
			return label.slice(5);
		default:
			return label.slice(5);
	}
}

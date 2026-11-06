import { describe, expect, it } from 'vitest';
import type { Series } from './api.ts';
import { colorOf, layout, metricOf, OTHER_COLOR, shortLabel, ticksFor } from './chart.ts';

const series: Series = {
	since: null,
	until: null,
	timezone: 'Europe/Berlin',
	period: 'week',
	by: 'project',
	keys: ['kompass', 'other'],
	total: { sessions: 3, human: 30, agent: 300 },
	by_key: [
		{ key: 'kompass', sessions: 2, human: 20, agent: 200 },
		{ key: 'other', sessions: 1, human: 10, agent: 100 }
	],
	buckets: [
		{
			start: '2026-09-28T00:00:00Z',
			label: '2026-W40',
			sessions: 2,
			human: 20,
			agent: 200,
			cells: [
				{ key: 'kompass', sessions: 1, human: 10, agent: 100 },
				{ key: 'other', sessions: 1, human: 10, agent: 100 }
			]
		},
		{ start: '2026-10-05T00:00:00Z', label: '2026-W41', sessions: 0, human: 0, agent: 0, cells: [] },
		{
			start: '2026-10-12T00:00:00Z',
			label: '2026-W42',
			sessions: 1,
			human: 10,
			agent: 100,
			cells: [{ key: 'kompass', sessions: 1, human: 10, agent: 100 }]
		}
	]
};

describe('ticksFor', () => {
	it('picks friendly steps that cover the maximum', () => {
		expect(ticksFor(0)).toEqual([0]);
		expect(ticksFor(220)).toEqual([0, 100, 200, 300]);
		expect(ticksFor(7)).toEqual([0, 2, 4, 6, 8]);
		expect(ticksFor(1000)).toEqual([0, 500, 1000]);
	});
});

describe('layout', () => {
	it('stacks the cells and leaves empty periods as empty bars', () => {
		const l = layout(series, 'events', 400, 200);
		expect(l.bars.map((b) => b.total)).toEqual([220, 0, 110]);
		expect(l.bars[0].segments.map((s) => s.key)).toEqual(['kompass', 'other']);
		expect(l.bars[1].segments).toEqual([]);
		expect(l.bars[0].segments[1].color).toBe(OTHER_COLOR);
		const top = l.bars[0].segments[1].y;
		const bottom = l.bars[0].segments[0].y + l.bars[0].segments[0].h;
		expect(bottom - top).toBeCloseTo((220 / l.max) * (200 - 8 - 22), 5);
		expect(l.ticks[0].value).toBe(0);
		expect(l.ticks[l.ticks.length - 1].value).toBe(l.max);
	});

	it('follows the metric', () => {
		expect(layout(series, 'human').bars.map((b) => b.total)).toEqual([20, 0, 10]);
		expect(layout(series, 'sessions').bars.map((b) => b.total)).toEqual([2, 0, 1]);
	});

	it('draws a series without keys as one segment per period', () => {
		const flat: Series = { ...series, keys: [], by: null, buckets: series.buckets.map((b) => ({ ...b, cells: [] })) };
		const l = layout(flat, 'events');
		expect(l.bars.map((b) => b.segments.length)).toEqual([1, 0, 1]);
	});
});

describe('helpers', () => {
	it('metricOf', () => {
		const c = { human: 3, agent: 7, sessions: 2 };
		expect(metricOf(c, 'events')).toBe(10);
		expect(metricOf(c, 'human')).toBe(3);
		expect(metricOf(c, 'sessions')).toBe(2);
	});
	it('colorOf cycles and greys other', () => {
		expect(colorOf('a', 0)).toBe(colorOf('b', 6));
		expect(colorOf('other', 1)).toBe(OTHER_COLOR);
	});
	it('shortLabel', () => {
		expect(shortLabel('2026-10-05', 'day')).toBe('05');
		expect(shortLabel('2026-W41', 'week')).toBe('W41');
		expect(shortLabel('2026-10', 'month')).toBe('10');
	});
});

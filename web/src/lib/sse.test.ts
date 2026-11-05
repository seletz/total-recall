import { describe, expect, it } from 'vitest';
import { parseEvents } from './sse.ts';

describe('parseEvents', () => {
	it('returns whole frames and keeps the rest', () => {
		const { frames, rest } = parseEvents(
			': connected\n\nevent: change\ndata: {"change":"modified"}\n\nevent: change\ndata: {"cha'
		);
		expect(frames).toEqual([{ event: 'change', data: '{"change":"modified"}' }]);
		expect(rest).toBe('event: change\ndata: {"cha');
	});

	it('drops comments and names a bare data frame message', () => {
		const { frames, rest } = parseEvents(': ping\n\ndata: a\ndata: b\n\n');
		expect(frames).toEqual([{ event: 'message', data: 'a\nb' }]);
		expect(rest).toBe('');
	});

	it('handles an empty buffer', () => {
		expect(parseEvents('')).toEqual({ frames: [], rest: '' });
	});
});

// Live updates: GET /api/events is a text/event-stream the Go server
// writes one "change" event to whenever the archive changed. EventSource
// cannot send the bearer header, so the stream is read with fetch and the
// frames are split by hand (parseEvents). The connection is retried with
// a growing pause when it drops.

import { token } from './api.ts';

export type Frame = { event: string; data: string };

/**
 * Splits the SSE frames out of buffer and returns them with the rest that
 * is not a whole frame yet. Comment lines (": ping") are dropped; an event
 * without an event: line is named "message", as the protocol says.
 */
export function parseEvents(buffer: string): { frames: Frame[]; rest: string } {
	const frames: Frame[] = [];
	let rest = buffer;
	for (;;) {
		const end = rest.indexOf('\n\n');
		if (end < 0) break;
		const block = rest.slice(0, end);
		rest = rest.slice(end + 2);
		let event = 'message';
		const data: string[] = [];
		for (const line of block.split('\n')) {
			if (line.startsWith(':')) continue;
			const i = line.indexOf(':');
			const field = i < 0 ? line : line.slice(0, i);
			let value = i < 0 ? '' : line.slice(i + 1);
			if (value.startsWith(' ')) value = value.slice(1);
			if (field === 'event') event = value;
			else if (field === 'data') data.push(value);
		}
		if (data.length) frames.push({ event, data: data.join('\n') });
	}
	return { frames, rest };
}

/**
 * Subscribes to the change stream; onChange gets "modified" or "replaced",
 * onState the connection state. Returns the function that stops it.
 */
export function subscribe(
	onChange: (change: string) => void,
	onState: (live: boolean) => void
): () => void {
	const ctl = new AbortController();
	let delay = 1000;
	const run = async () => {
		while (!ctl.signal.aborted) {
			try {
				const res = await fetch('/api/events', {
					headers: { Authorization: `Bearer ${token() ?? ''}` },
					signal: ctl.signal
				});
				if (!res.ok || !res.body) throw new Error(`HTTP ${res.status}`);
				onState(true);
				delay = 1000;
				const reader = res.body.getReader();
				const dec = new TextDecoder();
				let buf = '';
				for (;;) {
					const { value, done } = await reader.read();
					if (done) break;
					buf += dec.decode(value, { stream: true });
					const { frames, rest } = parseEvents(buf);
					buf = rest;
					for (const f of frames) {
						if (f.event !== 'change') continue;
						try {
							onChange((JSON.parse(f.data) as { change: string }).change);
						} catch {
							onChange('modified');
						}
					}
				}
			} catch {
				/* dropped: retry below */
			}
			onState(false);
			if (ctl.signal.aborted) return;
			await new Promise((r) => setTimeout(r, delay));
			delay = Math.min(delay * 2, 15000);
		}
	};
	void run();
	return () => ctl.abort();
}

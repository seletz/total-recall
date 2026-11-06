import { describe, expect, it } from 'vitest';
import { HIGHLIGHT_END as E, HIGHLIGHT_START as S, splitSnippet } from './snippet.ts';

describe('splitSnippet', () => {
	it('marks the matches', () => {
		expect(splitSnippet(`…the ${S}cask${E} to the ${S}tap${E}`)).toEqual([
			{ text: '…the ', hit: false },
			{ text: 'cask', hit: true },
			{ text: ' to the ', hit: false },
			{ text: 'tap', hit: true }
		]);
	});

	it('keeps angle brackets as text', () => {
		expect(splitSnippet('<script>x</script>')).toEqual([{ text: '<script>x</script>', hit: false }]);
	});

	it('handles null and empty', () => {
		expect(splitSnippet(null)).toEqual([]);
		expect(splitSnippet('')).toEqual([]);
	});
});

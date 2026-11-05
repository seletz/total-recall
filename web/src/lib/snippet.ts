// Snippets of the search carry their matches between two private-use
// characters (the server's HighlightStart/End). The UI renders the pieces
// as text nodes and <mark> elements, never as HTML: the text is whatever
// was typed into a session.

export const HIGHLIGHT_START = '';
export const HIGHLIGHT_END = '';

export type Part = { text: string; hit: boolean };

/** Splits a snippet into plain and highlighted parts. */
export function splitSnippet(s: string | null): Part[] {
	if (!s) return [];
	const parts: Part[] = [];
	let hit = false;
	let buf = '';
	for (const ch of s) {
		if (ch === HIGHLIGHT_START || ch === HIGHLIGHT_END) {
			if (buf) parts.push({ text: buf, hit });
			buf = '';
			hit = ch === HIGHLIGHT_START;
			continue;
		}
		buf += ch;
	}
	if (buf) parts.push({ text: buf, hit });
	return parts;
}

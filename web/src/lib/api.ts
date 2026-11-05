// The JSON API of `total-recall serve`. The token arrives once in the URL
// fragment of the start page (#token=...), is kept for the tab in
// sessionStorage and sent as a bearer header; the page never has it in
// its address afterwards.

const TOKEN_KEY = 'total-recall-token';

/** Reads the token from the URL fragment (once) or the tab's storage. */
export function token(): string | null {
	const m = /^#token=([0-9a-zA-Z_-]+)$/.exec(location.hash);
	if (m) {
		try {
			sessionStorage.setItem(TOKEN_KEY, m[1]);
		} catch {
			/* private mode: the token lives in memory only */
		}
		memoryToken = m[1];
		history.replaceState(null, '', location.pathname + location.search);
		return m[1];
	}
	if (memoryToken) return memoryToken;
	try {
		return sessionStorage.getItem(TOKEN_KEY);
	} catch {
		return null;
	}
}

let memoryToken: string | null = null;

export class ApiError extends Error {
	constructor(
		readonly status: number,
		readonly kind: string,
		message: string
	) {
		super(message);
		this.name = 'ApiError';
	}
}

/** Calls GET /api/<path>?<params> and resolves with the parsed JSON. */
export async function api<T>(
	path: string,
	params: Record<string, string> = {},
	signal?: AbortSignal
): Promise<T> {
	const qs = new URLSearchParams(
		Object.entries(params).filter(([, v]) => v !== '')
	).toString();
	const res = await fetch(`/api/${path}${qs ? `?${qs}` : ''}`, {
		headers: { Accept: 'application/json', Authorization: `Bearer ${token() ?? ''}` },
		signal
	});
	if (!res.ok) {
		let kind = 'internal';
		let message = `HTTP ${res.status}`;
		try {
			const body = (await res.json()) as { error?: { kind?: string; message?: string } };
			kind = body.error?.kind ?? kind;
			message = body.error?.message ?? message;
		} catch {
			/* no JSON body */
		}
		throw new ApiError(res.status, kind, message);
	}
	return (await res.json()) as T;
}

// Shapes of the answers: the JSON of `status`, `search --json` and
// `stats --period --by --json`, as the CLI defines them.

export type Status = {
	version: string;
	sessions: number;
	events: number;
	sources: number;
	last_ingest: Record<string, string>;
	schema_version: number;
	migration_pending: boolean;
	timezone: string;
};

export type Hit = {
	session_id: string;
	session_name: string | null;
	repo: string | null;
	event_id: string;
	ts: string;
	actor: string;
	kind: string;
	snippet: string | null;
	rank: number;
};

export type Activity = { sessions: number; human: number; agent: number };

export type Cell = Activity & { key: string };

export type Bucket = Activity & { start: string; label: string; cells: Cell[] };

export type Series = {
	since: string | null;
	until: string | null;
	timezone: string;
	period: 'day' | 'week' | 'month';
	by: string | null;
	keys: string[];
	total: Activity;
	by_key: Cell[];
	buckets: Bucket[];
};

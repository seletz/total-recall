import { svelte } from '@sveltejs/vite-plugin-svelte';
import { loadEnv } from 'vite';
import { defineConfig } from 'vitest/config';

export default defineConfig(({ mode }) => {
	// The Go server `total-recall serve` listens on 8787 by default; the
	// dev server proxies /api to it so the UI can be edited with hot reload
	// while the binary answers the API. TOTAL_RECALL_SERVE_PORT in the
	// environment (or a .env file here) points at another port.
	const env = loadEnv(mode, '.', 'TOTAL_RECALL_');
	const apiTarget = `http://127.0.0.1:${env.TOTAL_RECALL_SERVE_PORT ?? 8787}`;
	return {
	plugins: [svelte()],
	build: {
		// The Go package embeds this directory (internal/web/web.go); it is
		// git-ignored except for .gitkeep, which the build must keep.
		outDir: '../internal/web/dist',
		emptyOutDir: true,
		// No source maps in the binary; assets below 4 KiB stay files, not
		// data: URIs, so the content security policy can stay strict.
		sourcemap: false,
		assetsInlineLimit: 0
	},
	server: {
		host: '127.0.0.1',
		port: 5173,
		strictPort: true,
		// changeOrigin: false keeps the browser's Host header (localhost:5173),
		// which the Go server accepts as loopback; the Origin check sees the
		// dev server's origin, also loopback.
		proxy: {
			'/api': { target: apiTarget, changeOrigin: false }
		}
	},
	test: {
		environment: 'node',
		include: ['src/**/*.test.ts']
	}
	};
});

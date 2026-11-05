import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

export default {
	// TypeScript in <script lang="ts">; nothing else is preprocessed.
	preprocess: vitePreprocess()
};

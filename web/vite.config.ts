import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vitest/config';

// The Go server (cmd/server) serves the API and, in production, this app's
// static build. In dev, Vite proxies API/auth calls to it.
const backend = process.env.BACKEND_URL ?? 'http://localhost:8080';

export default defineConfig({
	plugins: [
		sveltekit({
			compilerOptions: {
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			// Single-page app: every unknown path falls back to index.html, which the
			// Go server mirrors in production.
			adapter: adapter({ fallback: 'index.html' })
		})
	],
	server: {
		port: 5173,
		strictPort: true,
		proxy: {
			'/api': backend,
			'/auth': backend,
			'/healthz': backend
		}
	},
	test: {
		include: ['src/**/*.test.ts'],
		environment: 'jsdom'
	}
});

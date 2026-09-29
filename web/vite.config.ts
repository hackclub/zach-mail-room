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
		// Listen beyond localhost so the app is reachable over Tailscale
		// (http://porygon:5173). Vite keeps the Host header when proxying, which
		// the Go server uses to pick the matching OAuth redirect (EXTRA_BASE_URLS).
		host: true,
		allowedHosts: ['localhost', 'porygon', '.ts.net'],
		// xfwd sends X-Forwarded-Host, which the Go server uses to pick the OAuth
		// redirect for whichever origin (localhost / porygon) the browser is on.
		proxy: Object.fromEntries(
			['/api', '/auth', '/healthz'].map((p) => [p, { target: backend, xfwd: true }])
		)
	},
	test: {
		include: ['src/**/*.test.ts'],
		environment: 'jsdom'
	}
});

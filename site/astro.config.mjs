import { defineConfig } from 'astro/config'
import tailwindcss from '@tailwindcss/vite'

// The product site is a static build, deployed on its own origin — it does not
// ship inside the router binary. `site` stays unset until the domain is decided;
// once it is, set it here so canonical and hreflang links resolve.
export default defineConfig({
  vite: { plugins: [tailwindcss()] },
  build: { format: 'directory' },
})

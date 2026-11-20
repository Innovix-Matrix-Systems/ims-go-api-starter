import { defineCollection } from 'astro:content'
import { glob } from 'astro/loaders'
import { docsSchema } from '@astrojs/starlight/schema'

// Pages come from the repository's docs/ folder. README.md is the GitHub
// index of that folder and history/ keeps old reviews; neither is a page.
export const collections = {
  docs: defineCollection({
    loader: glob({
      base: '../docs',
      pattern: ['**/*.{md,mdx}', '!README.md', '!history/**'],
    }),
    schema: docsSchema(),
  }),
}

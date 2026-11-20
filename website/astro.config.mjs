// @ts-check
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'astro/config'
import starlight from '@astrojs/starlight'
import rehypeDocsLinks from './src/plugins/rehype-docs-links.mjs'
import { unified } from '@astrojs/markdown-remark'

// The content lives in ../docs as plain Markdown, so it reads well on GitHub
// too; this folder is only the site shell (config, theme, components).
const version = readFileSync(new URL('../VERSION', import.meta.url), 'utf8').trim()
const repoUrl = 'https://github.com/Innovix-Matrix-Systems/ims-go-api-starter'
const docsDir = fileURLToPath(new URL('../docs', import.meta.url))
const siteUrl = process.env.DOCS_SITE_URL || 'https://ims-go-api-starter.innovixmatrixsystem.com'

export default defineConfig({
  // The public address: canonical links, the sitemap and link previews use it.
  // A fork publishing elsewhere sets DOCS_SITE_URL instead.
  site: siteUrl,
  markdown: {
    // Relative links such as ../guides/tenancy.md work on GitHub and here.
    processor: unified({ rehypePlugins: [[rehypeDocsLinks, { docsDir, repoUrl }]] }),
  },
  vite: {
    resolve: {
      // Pages live outside this folder (../docs), where Node can't find
      // website/node_modules, so imports used in MDX are aliased here.
      alias: [
        { find: '@components', replacement: fileURLToPath(new URL('./src/components', import.meta.url)) },
        {
          find: /^@astrojs\/starlight\/components$/,
          replacement: fileURLToPath(new URL('./node_modules/@astrojs/starlight/dist/components.ts', import.meta.url)),
        },
      ],
    },
    server: { fs: { allow: ['..'] } },
  },
  integrations: [
    starlight({
      title: 'IMS Go API Starter',
      description:
        "Laravel's developer experience, Go's performance: a multi-tenant API starter in one binary, with a developer dashboard.",
      logo: { src: './src/assets/logo.svg', alt: 'IMS' },
      favicon: '/favicon.svg',
      customCss: ['./src/styles/theme.css'],
      lastUpdated: true,
      pagination: true,
      credits: false,
      tableOfContents: { minHeadingLevel: 2, maxHeadingLevel: 3 },
      components: {
        SiteTitle: './src/components/SiteTitle.astro',
        Hero: './src/components/Hero.astro',
        Footer: './src/components/Footer.astro',
        Search: './src/components/Search.astro',
      },
      social: [{ icon: 'github', label: 'Innovix Matrix System on GitHub', href: 'https://github.com/Innovix-Matrix-Systems' }],
      head: [
        { tag: 'meta', attrs: { name: 'docs-version', content: version } },
        { tag: 'meta', attrs: { name: 'author', content: 'Innovix Matrix System' } },
        // Link-preview card (Slack, LinkedIn, X…), built by `npm run brand`.
        { tag: 'meta', attrs: { property: 'og:image', content: `${siteUrl}/og.png` } },
        { tag: 'meta', attrs: { property: 'og:image:width', content: '1200' } },
        { tag: 'meta', attrs: { property: 'og:image:height', content: '630' } },
        { tag: 'meta', attrs: { property: 'og:image:alt', content: 'IMS Go API Starter, open source by Innovix Matrix System' } },
        { tag: 'meta', attrs: { name: 'twitter:image', content: `${siteUrl}/og.png` } },
      ],
      sidebar: [
        {
          label: 'Start here',
          items: [
            'getting-started/introduction',
            'getting-started/quick-start',
            'getting-started/how-it-works',
            'getting-started/project-tour',
          ],
        },
        {
          label: 'Tutorial',
          items: [{ slug: 'tutorial/first-module', badge: { text: '30 min', variant: 'tip' } }],
        },
        {
          label: 'Building the API',
          items: [
            'guides/modules',
            'guides/handlers-and-routes',
            'guides/validation-and-errors',
            'guides/database',
            'guides/tenancy',
            'guides/authentication',
            'guides/permissions',
            'guides/testing',
          ],
        },
        {
          label: 'Features',
          items: [
            'guides/billing',
            'guides/settings',
            'guides/feature-flags',
            'guides/queue-and-scheduler',
            'guides/files-and-storage',
            'guides/activity-log',
            'guides/notifications',
            'guides/import-export',
            'guides/mail-and-i18n',
            'guides/health-and-observability',
          ],
        },
        {
          label: 'Developer dashboard',
          items: ['dashboard/overview', 'dashboard/extending'],
        },
        {
          label: 'Reference',
          items: [
            'reference/http-api',
            'reference/cli',
            'reference/configuration',
            'reference/permissions',
            'reference/make',
          ],
        },
        {
          label: 'Operations',
          items: [
            'operations/local-development',
            'operations/deploy',
            'operations/docker',
            'operations/migrations',
            'operations/backups',
            'operations/rollback',
            'operations/releases',
          ],
        },
        {
          label: 'Architecture',
          items: ['architecture/overview', 'architecture/decisions'],
        },
        {
          label: 'Project',
          items: ['project/contributing', 'project/ai-agents', 'project/changelog'],
        },
      ],
    }),
  ],
})

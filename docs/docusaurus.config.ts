import {themes as prismThemes} from 'prism-react-renderer'
import type {Config} from '@docusaurus/types'

const algoliaAppId = process.env.ALGOLIA_APP_ID
const algoliaApiKey = process.env.ALGOLIA_API_KEY
const algoliaIndexName = process.env.ALGOLIA_INDEX_NAME
const useAlgolia = Boolean(algoliaAppId && algoliaApiKey && algoliaIndexName)

const config: Config = {
  title: 'Velane Docs',
  tagline: 'Cloud infrastructure for your agents and small apps.',

  url: 'https://docs.velane.sh',
  baseUrl: '/',

  onBrokenLinks: 'throw',
  markdown: {
    hooks: {
      onBrokenMarkdownLinks: 'throw'
    }
  },

  i18n: {
    defaultLocale: 'en',
    locales: ['en']
  },

  presets: [
    [
      'classic',
      {
        docs: {
          path: '.',
          routeBasePath: '/',
          sidebarPath: './sidebars.ts',
          exclude: ['**/node_modules/**', '**/build/**', '**/.docusaurus/**']
        },
        blog: false,
        pages: false,
        theme: {
          customCss: './src/css/custom.css'
        },
        gtag: {
          trackingID: 'G-B96HFEY5XQ',
        },
      }
    ]
  ],
  plugins: [],

  favicon: 'favicon-32x32.png',

  themeConfig: {
    navbar: {
      title: 'Velane',
      logo: {
        alt: 'Velane logo',
        src: 'velane_sh.png'
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docs',
          position: 'left',
          label: 'Docs'
        },
        {
          href: 'https://github.com/abskrj/velane',
          className: 'header-github-link',
          'aria-label': 'GitHub repository',
          position: 'right'
        },
        {
          href: 'https://discord.gg/XKFMyhFrq6',
          className: 'header-discord-link',
          'aria-label': 'Discord community',
          position: 'right'
        }
      ]
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Get started',
          items: [
            {label: 'Documentation home', to: '/'},
            {label: 'Local quickstart', to: '/getting-started/local-quickstart'},
            {label: 'Your first snippet', to: '/getting-started/first-snippet'},
            {label: 'CLI installation', to: '/cli/installation-and-auth'}
          ]
        },
        {
          title: 'Guides',
          items: [
            {label: 'Invocation modes', to: '/invoke/invocation-modes'},
            {label: 'Integrations', to: '/integrations/overview'},
            {label: 'MCP overview', to: '/mcp/overview'},
            {label: 'Auth and request flow', to: '/auth-tenancy/auth-and-request-flow'}
          ]
        },
        {
          title: 'Operations',
          items: [
            {label: 'Production checklist', to: '/operations/production-checklist'},
            {label: 'Environment variables', to: '/operations/environment-variables'},
            {label: 'Security non-negotiables', to: '/security/non-negotiables'},
            {label: 'Contributing', to: '/contributing/dev-workflow'}
          ]
        },
        {
          title: 'Project',
          items: [
            {label: 'GitHub', href: 'https://github.com/abskrj/velane'},
            {label: 'Issues', href: 'https://github.com/abskrj/velane/issues'},
            {label: 'App', href: 'https://app.velane.sh'},
            {label: 'License', href: 'https://github.com/abskrj/velane/blob/main/LICENSE'}
          ]
        }
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Velane. Dual-licensed under AGPL-3.0 and commercial terms.`
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula
    },
    ...(useAlgolia
      ? {
          algolia: {
            appId: algoliaAppId!,
            apiKey: algoliaApiKey!,
            indexName: algoliaIndexName!,
            contextualSearch: true
          }
        }
      : {})
  }
}

export default config

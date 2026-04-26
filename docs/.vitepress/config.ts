import { defineConfig } from 'vitepress'
import { withMermaid } from 'vitepress-plugin-mermaid'

export default withMermaid(defineConfig({
  title: 'RingClaw',
  description: 'RingCentral AI Agent Bridge',
  base: '/ringclaw/',

  locales: {
    root: {
      label: 'English',
      lang: 'en',
    },
    zh: {
      label: '简体中文',
      lang: 'zh-CN',
      themeConfig: {
        nav: [
          { text: '指南', link: '/zh/guide/getting-started' },
          { text: '配置', link: '/zh/guide/configuration' },
          { text: '安全', link: '/zh/security/' },
          { text: '功能', link: '/zh/features/summarize' },
          { text: 'API', link: '/zh/api/rest' },
          { text: '部署', link: '/zh/deployment/background' },
          { text: '架构', link: '/zh/architecture/prompt-evolution' },
        ],
        sidebar: {
          '/zh/guide/': [
            {
              text: '指南',
              items: [
                { text: '快速开始', link: '/zh/guide/getting-started' },
                { text: '配置文件', link: '/zh/guide/configuration' },
                { text: 'Agent 配置', link: '/zh/guide/agents' },
                { text: '聊天命令', link: '/zh/guide/commands' },
                { text: '工作原理', link: '/zh/guide/how-it-works' },
              ],
            },
          ],
          '/zh/features/': [
            {
              text: '功能',
              items: [
                { text: '聊天总结', link: '/zh/features/summarize' },
                { text: 'AI 驱动操作', link: '/zh/features/actions' },
                { text: '定时任务', link: '/zh/features/cron' },
                { text: '心跳检测', link: '/zh/features/heartbeat' },
                { text: '图片解析', link: '/zh/features/image-analysis' },
                { text: '媒体与推送', link: '/zh/features/media' },
              ],
            },
          ],
          '/zh/architecture/': [
            {
              text: '架构',
              items: [
                { text: 'Prompt 自进化', link: '/zh/architecture/prompt-evolution' },
              ],
            },
          ],
          '/zh/security/': [
            {
              text: '安全',
              items: [
                { text: '安全概览', link: '/zh/security/' },
              ],
            },
          ],
          '/zh/api/': [
            {
              text: 'API',
              items: [
                { text: 'REST API', link: '/zh/api/rest' },
              ],
            },
          ],
          '/zh/deployment/': [
            {
              text: '部署',
              items: [
                { text: '后台运行', link: '/zh/deployment/background' },
                { text: 'Docker', link: '/zh/deployment/docker' },
              ],
            },
          ],
        },
      },
    },
  },

  themeConfig: {
    nav: [
      { text: 'Guide', link: '/guide/getting-started' },
      { text: 'Configuration', link: '/guide/configuration' },
      { text: 'Security', link: '/security/' },
      { text: 'Features', link: '/features/summarize' },
      { text: 'API', link: '/api/rest' },
      { text: 'Deployment', link: '/deployment/background' },
      { text: 'Architecture', link: '/architecture/prompt-evolution' },
    ],

    sidebar: {
      '/guide/': [
        {
          text: 'Guide',
          items: [
            { text: 'Getting Started', link: '/guide/getting-started' },
            { text: 'Configuration', link: '/guide/configuration' },
            { text: 'Agent Configuration', link: '/guide/agents' },
            { text: 'Chat Commands', link: '/guide/commands' },
            { text: 'How It Works', link: '/guide/how-it-works' },
          ],
        },
      ],
      '/features/': [
        {
          text: 'Features',
          items: [
            { text: 'Chat Summarization', link: '/features/summarize' },
            { text: 'AI-Driven Actions', link: '/features/actions' },
            { text: 'Cron Jobs', link: '/features/cron' },
            { text: 'Heartbeat', link: '/features/heartbeat' },
            { text: 'Image Analysis', link: '/features/image-analysis' },
            { text: 'Media & Messaging', link: '/features/media' },
          ],
        },
      ],
      '/architecture/': [
        {
          text: 'Architecture',
          items: [
            { text: 'Prompt Self-Evolution', link: '/architecture/prompt-evolution' },
          ],
        },
      ],
      '/security/': [
        {
          text: 'Security',
          items: [
            { text: 'Overview', link: '/security/' },
          ],
        },
      ],
      '/api/': [
        {
          text: 'API',
          items: [
            { text: 'REST API', link: '/api/rest' },
          ],
        },
      ],
      '/deployment/': [
        {
          text: 'Deployment',
          items: [
            { text: 'Background Mode', link: '/deployment/background' },
            { text: 'Docker', link: '/deployment/docker' },
          ],
        },
      ],
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/ringclaw/ringclaw' },
    ],

    search: {
      provider: 'local',
    },
  },
}))

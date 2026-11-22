export type NavItem = {
  title: string
  href: string
  summary: string
}

export type NavGroup = {
  title: string
  items: NavItem[]
}

/**
 * 文档目录的单一来源：侧栏、上一页/下一页、首页入口都从这里读。
 * 新增页面时只要在这里加一行，导航不会失联。
 */
export const docsNav: NavGroup[] = [
  {
    title: '上手',
    items: [
      { title: '项目概览', href: '/docs/', summary: '它解决什么问题，由哪些能力组成' },
      { title: '部署安装', href: '/docs/deploy/', summary: 'Docker Compose、单二进制、反向代理与备份' },
      { title: '首次配置', href: '/docs/setup/', summary: '登录、TMDB 密钥、目录与整理策略' },
    ],
  },
  {
    title: '使用',
    items: [
      { title: '智能刮削', href: '/docs/scrape/', summary: '文件名解析、NFO 与海报生成规则' },
      { title: '文件整理', href: '/docs/transfer/', summary: '四种转移模式、冲突策略与蓝光保护' },
      { title: '媒体库与字幕', href: '/docs/library/', summary: '媒体库浏览、外挂/内嵌字幕与简繁转换' },
      { title: '搜索下载', href: '/docs/search/', summary: '索引器、片名识别、推送到 qBittorrent' },
      { title: 'MCP / Agent', href: '/docs/mcp/', summary: 'API Key、MCP 端点与字幕 Agent 工作流' },
    ],
  },
  {
    title: '进阶',
    items: [
      { title: '系统设置详解', href: '/docs/settings/', summary: '九个设置页逐项说明' },
      { title: '架构与接口', href: '/docs/architecture/', summary: '分层、数据表、REST 与 MCP 接口' },
      { title: '常见问题', href: '/docs/faq/', summary: '部署、刮削、整理、搜索排错' },
    ],
  },
]

export const flatDocs = docsNav.flatMap((group) => group.items)

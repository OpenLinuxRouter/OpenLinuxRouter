// Every string a page shows. Two locales, one shape, so a missing translation
// is a type error rather than a page that silently falls back mid-sentence.

export type Lang = 'en' | 'zh'

export const langs: Lang[] = ['en', 'zh']

export const site = {
  name: 'Open Linux Router',
  shortName: 'olr',
  repo: 'https://github.com/open-linux-router/open-linux-router',
  docs: 'docs',
}

export const nav = {
  en: [
    { href: '/', label: 'Home' },
    { href: '/capabilities', label: 'Capabilities' },
    { href: '/gallery', label: 'Gallery' },
    { href: '/docs', label: 'Docs' },
  ],
  zh: [
    { href: '/zh/', label: '首页' },
    { href: '/zh/capabilities', label: '能力' },
    { href: '/zh/gallery', label: '图库' },
    { href: '/zh/docs', label: '文档' },
  ],
} as const

export const ui = {
  en: {
    skip: 'Skip to content',
    menu: 'Menu',
    close: 'Close',
    learnMore: 'Learn more',
    readDocs: 'Read the docs',
    browseGallery: 'Browse the gallery',
    viewAll: 'View all',
    previous: 'Previous',
    next: 'Next',
    search: 'Search',
    searchPlaceholder: 'Search the site…',
    searchEmpty: 'Nothing matched. Try a shorter word.',
    searchHint: 'Search pages, capabilities and docs',
    onThisPage: 'On this page',
    language: '语言',
    themeLight: 'Light',
    themeDark: 'Dark',
    themeSystem: 'System',
    status: 'Status',
    early: 'Early',
    copy: 'Copy',
    copied: 'Copied',
    all: 'All',
    filters: 'Filter',
    screenshotOf: 'Screenshot of',
    screenshotHint: 'Click a screenshot to enlarge. Arrows move between shots, Esc closes.',
    variantLang: 'Interface language',
    variantTheme: 'Theme',
    docsIndex: 'Documentation',
    docsIntro: 'Operator and architecture notes, one file per subject.',
    galleryIntro: 'Every screen in the console, captured from a real box.',
    capabilitiesIntro: 'What the product actually does, module by module.',
    minRead: 'min read',
  },
  zh: {
    skip: '跳到正文',
    menu: '菜单',
    close: '关闭',
    learnMore: '了解更多',
    readDocs: '读文档',
    browseGallery: '看图库',
    viewAll: '查看全部',
    previous: '上一个',
    next: '下一个',
    search: '搜索',
    searchPlaceholder: '搜索站内内容…',
    searchEmpty: '没有匹配的内容，换个短一点的词试试。',
    searchHint: '搜索页面、能力与文档',
    onThisPage: '本页目录',
    language: 'Language',
    themeLight: '浅色',
    themeDark: '深色',
    themeSystem: '跟随系统',
    status: '状态',
    early: '早期',
    copy: '复制',
    copied: '已复制',
    all: '全部',
    filters: '筛选',
    screenshotOf: '界面截图',
    screenshotHint: '点截图放大。左右键切换，Esc 关闭。',
    variantLang: '界面语言',
    variantTheme: '主题',
    docsIndex: '文档',
    docsIntro: '运维与架构说明，一个主题一个文件。',
    galleryIntro: '控制台里的每一个页面，都截自真实设备。',
    capabilitiesIntro: '这个产品实际做什么，按模块说清楚。',
    minRead: '分钟读完',
  },
} as const

export function t(lang: Lang) {
  return ui[lang]
}

export function otherLang(lang: Lang): Lang {
  return lang === 'en' ? 'zh' : 'en'
}

export function href(lang: Lang, path: string): string {
  // `/docs` is English; `/zh/docs` is Chinese. The root is the only English
  // path without a prefix.
  if (lang === 'en') return path
  return path === '/' ? '/zh/' : `/zh${path}`
}

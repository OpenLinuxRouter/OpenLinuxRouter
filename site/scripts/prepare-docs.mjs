// Pre-renders every doc into src/lib/docs.generated.json.
//
// Two reasons this is a build step rather than a page-time import: the pages
// stay pure static HTML (no Markdown parser in the browser), and a Chinese
// translation can be dropped into docs/zh/<slug>.md without touching the site.
//
//   node scripts/prepare-docs.mjs

import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import { extractMeta, renderMarkdown } from './markdown.mjs'

const here = path.dirname(fileURLToPath(import.meta.url))
const siteRoot = path.resolve(here, '..')
const docsRoot = path.resolve(siteRoot, '../docs')
const zhRoot = path.resolve(docsRoot, 'zh')
const outFile = path.resolve(siteRoot, 'src/lib/docs.generated.json')

const docGroups = [
  { id: 'start', en: 'Getting started', zh: '入门', slugs: ['install', 'system', 'administrator-access', 'configuration-safety', 'cli'] },
  { id: 'network', en: 'Network', zh: '网络', slugs: ['gateway', 'dns', 'ddns', 'dial', 'ipv6-controls', 'wireless', 'network-equipment', 'iptv', 'he-tunnel-setup'] },
  { id: 'access', en: 'Access and security', zh: '访问与安全', slugs: ['firewall', 'port-forwarding', 'remote-access', 'socks-out', 'ingress', 'qos'] },
  { id: 'observe', en: 'Observing the network', zh: '观测网络', slugs: ['latency', 'network-quality', 'world-traffic', 'device-inspection', 'device-link-test'] },
  { id: 'services', en: 'Services and tools', zh: '服务与工具', slugs: ['storage', 'backup', 'device-artwork-generation', 'mcp', 'tools-cdn-edge', 'tools-dns-exit', 'tools-split-check', 'tools-traceroute'] },
  { id: 'interface', en: 'Interface and copy', zh: '界面与文案', slugs: ['access-ui', 'ui-style-guide', 'ui-content-guide', 'ui-semantic-components', 'ui-page-review', 'ui-control-language', 'ui-control-audit'] },
]

const titles = {
  'access-ui': { en: 'Remote access interface', zh: '远程访问界面' },
  'administrator-access': { en: 'Administrator access', zh: '管理员访问' },
  'backup': { en: 'Backup and restore', zh: '备份与恢复' },
  'cli': { en: 'CLI conventions', zh: 'CLI 约定' },
  'configuration-safety': { en: 'Configuration safety', zh: '配置安全' },
  'ddns': { en: 'Dynamic DNS', zh: '动态 DNS' },
  'device-artwork-generation': { en: 'Device artwork', zh: '设备图稿' },
  'device-inspection': { en: 'Request inspection', zh: '请求检查' },
  'device-link-test': { en: 'Client link test', zh: '客户端链路测试' },
  'dial': { en: 'Uplink', zh: '上行链路' },
  'dns': { en: 'DNS', zh: 'DNS' },
  'firewall': { en: 'Firewall', zh: '防火墙' },
  'gateway': { en: 'Gateway', zh: '网关' },
  'he-tunnel-setup': { en: 'HE IPv6 tunnel', zh: 'HE IPv6 隧道' },
  'ingress': { en: 'HTTPS ingress', zh: 'HTTPS 入口' },
  'install': { en: 'Install on Debian', zh: '在 Debian 上安装' },
  'iptv': { en: 'IPTV multicast', zh: 'IPTV 组播' },
  'ipv6-controls': { en: 'IPv6 controls', zh: 'IPv6 控制' },
  'latency': { en: 'Latency', zh: '延迟' },
  'mcp': { en: 'MCP surface', zh: 'MCP 接口' },
  'network-equipment': { en: 'Network equipment', zh: '网络设备' },
  'network-quality': { en: 'Network quality', zh: '网络质量' },
  'port-forwarding': { en: 'Port forwarding', zh: '端口转发' },
  'qos': { en: 'Client QoS', zh: '客户端 QoS' },
  'remote-access': { en: 'Remote access', zh: '远程访问' },
  'socks-out': { en: 'SOCKS5 exit', zh: 'SOCKS5 出口' },
  'storage': { en: 'Storage', zh: '存储' },
  'system': { en: 'System model', zh: '系统模型' },
  'tools-cdn-edge': { en: 'CDN edge', zh: 'CDN 边缘' },
  'tools-dns-exit': { en: 'DNS exit', zh: 'DNS 出口' },
  'tools-split-check': { en: 'Split check', zh: '分流检查' },
  'tools-traceroute': { en: 'Traceroute', zh: '路由追踪' },
  'ui-content-guide': { en: 'Content standards', zh: '文案规范' },
  'ui-control-audit': { en: 'Control language audit', zh: '控件语言审计' },
  'ui-control-language': { en: 'Control language', zh: '控件语言' },
  'ui-page-review': { en: 'Page review baseline', zh: '页面评审基线' },
  'ui-semantic-components': { en: 'Semantic components', zh: '语义组件' },
  'ui-style-guide': { en: 'Visual style guide', zh: '视觉规范' },
  'wireless': { en: 'Wireless', zh: '无线' },
  'world-traffic': { en: 'World traffic', zh: '世界流量' },
}

const groupOf = new Map()
for (const group of docGroups) for (const slug of group.slugs) groupOf.set(slug, group.id)

const files = readdirSync(docsRoot).filter((file) => file.endsWith('.md'))
const entries = []
let translated = 0

for (const file of files) {
  const slug = file.slice(0, -3)
  const source = readFileSync(path.join(docsRoot, file), 'utf8')
  const meta = extractMeta(source)
  const en = renderMarkdown(source, { base: '/docs' })

  const zhFile = path.join(zhRoot, `${slug}.md`)
  const hasZh = existsSync(zhFile)
  if (hasZh) translated++

  entries.push({
    slug,
    title: titles[slug]?.en ?? meta.title,
    originalTitle: meta.title,
    zhTitle: titles[slug]?.zh ?? meta.title,
    summary: meta.summary,
    group: groupOf.get(slug) ?? 'services',
    html: en.html,
    headings: en.headings,
    zhHtml: hasZh ? renderMarkdown(readFileSync(zhFile, 'utf8'), { base: '/zh/docs' }).html : null,
    zhHeadings: hasZh ? renderMarkdown(readFileSync(zhFile, 'utf8'), { base: '/zh/docs' }).headings : null,
  })
}

entries.sort((a, b) => a.title.localeCompare(b.title))

mkdirSync(path.dirname(outFile), { recursive: true })
writeFileSync(
  outFile,
  `${JSON.stringify({ groups: docGroups, docs: entries }, null, 2)}\n`,
)

console.log(`docs: ${entries.length} rendered, ${translated} with a Chinese translation`)

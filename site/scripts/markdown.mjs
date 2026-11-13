// Markdown → HTML for the repository's docs/*.md, at build time.
//
// The docs are operator notes: headings, paragraphs, lists, fenced code, tables,
// links and inline code. That is the whole grammar; anything else is left as
// plain text rather than guessed at. Keeping this as a plain module (no Node
// imports) means the site never ships a Markdown parser to the browser — the
// pages are static HTML.

/** Strip the Markdown a title or summary should not show literally. */
export function plain(text) {
  return text
    .replace(/\*\*([^*]+)\*\*/g, '$1')
    .replace(/`([^`]+)`/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replace(/(^|[^*])\*([^*]+)\*/g, '$1$2')
    .replace(/\s+/g, ' ')
    .trim()
}

function escapeHtml(text) {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

export function slugify(text) {
  return text
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, '-')
    .replace(/^-|-$/g, '')
}

function inline(text, base) {
  let out = escapeHtml(text)

  const codes = []
  out = out.replace(/`([^`]+)`/g, (_, code) => {
    codes.push(`<code>${code}</code>`)
    return `\u0000${codes.length - 1}\u0000`
  })

  out = out
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
    .replace(/(^|[^*])\*([^*]+)\*/g, '$1<em>$2</em>')
    .replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_, label, url) => {
      const external = /^https?:\/\//.test(url)
      let rel = url
      if (url.endsWith('.md')) {
        // Links between docs name files; the site addresses routes.
        if (url.startsWith('../')) {
          rel = 'https://github.com/open-linux-router/open-linux-router/blob/main/README.md'
        } else {
          rel = `${base}/${url.replace(/^\.?\/?/, '').replace(/\.md$/, '')}`
        }
      }
      return `<a href="${rel}"${external ? ' target="_blank" rel="noreferrer"' : ''}>${label}</a>`
    })
    .replace(/(^|\s)(https?:\/\/[^\s<)]+)/g, '$1<a href="$2" target="_blank" rel="noreferrer">$2</a>')

  return out.replace(/\u0000(\d+)\u0000/g, (_, i) => codes[Number(i)])
}

function isTableRow(line) {
  return line.trim().startsWith('|') && line.trim().endsWith('|')
}

/** Title is the first `# ` line; the deck is the first paragraph under it. */
export function extractMeta(source) {
  const lines = source.split('\n')
  let title = ''
  let seenTitle = false
  const paragraphs = []
  let buffer = []

  for (const line of lines) {
    if (!seenTitle) {
      if (line.startsWith('# ')) {
        title = line.slice(2).trim()
        seenTitle = true
      }
      continue
    }

    const blockStart = /^(#{1,4}\s|\|\s*$|```)/.test(line)
    if (blockStart) {
      if (buffer.length) {
        paragraphs.push(buffer.join(' ').trim())
        buffer = []
      }
      continue
    }

    if (!line.trim()) {
      if (buffer.length) {
        paragraphs.push(buffer.join(' ').trim())
        buffer = []
      }
      if (paragraphs.length >= 3) break
      continue
    }

    buffer.push(line.trim())
    if (paragraphs.length >= 3) break
  }
  if (buffer.length) paragraphs.push(buffer.join(' ').trim())

  // "Status: built" tells a maintainer where the code is, not a reader what the
  // page is for. Prefer the first paragraph that introduces the subject.
  const summary =
    paragraphs.find((p) => p && !/^Status:/i.test(p)) ?? paragraphs[0] ?? ''

  return { title: plain(title) || 'Untitled', summary: plain(summary) }
}

export function renderMarkdown(source, { base = '/docs' } = {}) {
  const lines = source.split('\n')
  const html = []
  const headings = []
  let i = 0
  let skippedTitle = false

  while (i < lines.length) {
    const line = lines[i]

    if (!skippedTitle && line.startsWith('# ')) {
      skippedTitle = true
      i++
      continue
    }

    if (!line.trim()) {
      i++
      continue
    }

    if (line.startsWith('```')) {
      const fence = line.slice(3).trim()
      const code = []
      i++
      while (i < lines.length && !lines[i].startsWith('```')) {
        code.push(lines[i])
        i++
      }
      i++
      html.push(`<pre data-lang="${escapeHtml(fence)}"><code>${escapeHtml(code.join('\n'))}</code></pre>`)
      continue
    }

    const heading = line.match(/^(#{2,4})\s+(.*)$/)
    if (heading) {
      const level = heading[1].length
      const text = heading[2]
      if (level === 2) {
        const id = slugify(text)
        headings.push({ id, text, level })
        html.push(`<h2 id="${id}">${inline(text, base)}</h2>`)
      } else {
        html.push(`<h${level}>${inline(text, base)}</h${level}>`)
      }
      i++
      continue
    }

    if (isTableRow(line) && i + 1 < lines.length && /^\|[\s:|-]+\|$/.test(lines[i + 1].trim())) {
      const header = line.split('|').slice(1, -1).map((c) => c.trim())
      i += 2
      const rows = []
      while (i < lines.length && isTableRow(lines[i])) {
        rows.push(lines[i].split('|').slice(1, -1).map((c) => c.trim()))
        i++
      }
      html.push(
        `<div class="overflow-x-auto"><table><thead><tr>${header
          .map((c) => `<th>${inline(c, base)}</th>`)
          .join('')}</tr></thead><tbody>${rows
          .map((r) => `<tr>${r.map((c) => `<td>${inline(c, base)}</td>`).join('')}</tr>`)
          .join('')}</tbody></table></div>`,
      )
      continue
    }

    if (/^[-*]\s+/.test(line)) {
      const items = []
      while (i < lines.length && /^[-*]\s+/.test(lines[i])) {
        let item = lines[i].replace(/^[-*]\s+/, '')
        i++
        // A list item may wrap onto indented lines; they belong to it, not to a
        // new paragraph.
        while (i < lines.length && /^\s{2,}\S/.test(lines[i]) && !/^\s*[-*]\s+/.test(lines[i])) {
          item += ` ${lines[i].trim()}`
          i++
        }
        items.push(item)
      }
      html.push(`<ul>${items.map((it) => `<li>${inline(it, base)}</li>`).join('')}</ul>`)
      continue
    }

    if (/^\d+\.\s+/.test(line)) {
      const items = []
      while (i < lines.length && /^\d+\.\s+/.test(lines[i])) {
        let item = lines[i].replace(/^\d+\.\s+/, '')
        i++
        while (i < lines.length && /^\s{2,}\S/.test(lines[i]) && !/^\s*\d+\.\s+/.test(lines[i])) {
          item += ` ${lines[i].trim()}`
          i++
        }
        items.push(item)
      }
      html.push(`<ol>${items.map((it) => `<li>${inline(it, base)}</li>`).join('')}</ol>`)
      continue
    }

    if (line.startsWith('> ')) {
      const quote = []
      while (i < lines.length && lines[i].startsWith('> ')) {
        quote.push(lines[i].slice(2))
        i++
      }
      html.push(`<blockquote>${inline(quote.join(' '), base)}</blockquote>`)
      continue
    }

    if (/^---+$/.test(line.trim())) {
      html.push('<hr />')
      i++
      continue
    }

    const para = []
    while (
      i < lines.length &&
      lines[i].trim() &&
      !lines[i].startsWith('#') &&
      !lines[i].startsWith('```') &&
      !lines[i].startsWith('> ') &&
      !/^[-*]\s+/.test(lines[i]) &&
      !/^\d+\.\s+/.test(lines[i]) &&
      !isTableRow(lines[i])
    ) {
      para.push(lines[i])
      i++
    }
    if (para.length) html.push(`<p>${inline(para.join(' '), base)}</p>`)
    else i++
  }

  return { html: html.join('\n'), headings }
}

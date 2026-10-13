const escapeHTML = value => String(value).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));

function safeURL(raw) {
  try {
    const url = new URL(raw);
    return ['http:', 'https:', 'mailto:'].includes(url.protocol) ? escapeHTML(url.href) : null;
  } catch { return null; }
}

function inline(text, depth = 0) {
  if (depth > 5) return escapeHTML(text);
  const tokens = /!\[([^\[\]\n]*)\]\([^()\s]{0,2048}\)|`([^`]+)`|\*\*([^*]+)\*\*|__([^_]+)__|~~([^~]+)~~|\*([^*]+)\*|_([^_]+)_|\[([^\[\]\n]+)\]\(([^()\s]{1,2048})\)/g;
  let out = '', end = 0;
  for (const m of text.matchAll(tokens)) {
    out += escapeHTML(text.slice(end, m.index));
    if (m[1] !== undefined) out += escapeHTML(m[1]); // Images are text only; never fetch media.
    else if (m[2] !== undefined) out += `<code>${escapeHTML(m[2])}</code>`;
    else if (m[3] !== undefined || m[4] !== undefined) out += `<strong>${inline(m[3] ?? m[4], depth+1)}</strong>`;
    else if (m[5] !== undefined) out += `<del>${inline(m[5], depth+1)}</del>`;
    else if (m[6] !== undefined || m[7] !== undefined) out += `<em>${inline(m[6] ?? m[7], depth+1)}</em>`;
    else {
      const href = safeURL(m[9]);
      out += href ? `<a href="${href}" target="_blank" rel="noopener noreferrer">${escapeHTML(m[8])}</a>` : escapeHTML(m[8]);
    }
    end = m.index + m[0].length;
  }
  return out + escapeHTML(text.slice(end));
}

// Deliberately small Markdown subset. Raw HTML is always escaped.
export function renderMarkdown(source) {
  const lines = String(source).replace(/\r\n?/g, '\n').split('\n');
  const out = [];
  let paragraph = [], list = null, code = null;
  const flushParagraph = () => { if (paragraph.length) out.push(`<p>${inline(paragraph.join('\n')).replace(/\n/g,'<br>')}</p>`); paragraph=[]; };
  const flushList = () => { if (list) out.push(`</${list}>`); list=null; };
  for (const line of lines) {
    if (/^\s*```/.test(line)) {
      flushParagraph(); flushList();
      if (code !== null) { out.push(`<pre><code>${escapeHTML(code.join('\n'))}</code></pre>`); code=null; } else code=[];
      continue;
    }
    if (code !== null) { code.push(line); continue; }
    if (!line.trim()) { flushParagraph(); flushList(); continue; }
    const heading = /^(#{1,6})\s+(.+)$/.exec(line);
    const item = /^\s*(?:([-+*])|\d+[.)])\s+(.+)$/.exec(line);
    if (item) {
      flushParagraph(); const type = item[1] ? 'ul' : 'ol';
      if (list !== type) { flushList(); out.push(`<${type}>`); list=type; }
      const task = /^\[([ xX])\]\s+(.*)$/.exec(item[2]);
      out.push(task ? `<li><input type="checkbox" disabled${task[1].toLowerCase()==='x'?' checked':''}>${inline(task[2])}</li>` : `<li>${inline(item[2])}</li>`);
    } else if (heading) {
      flushParagraph(); flushList(); const level=heading[1].length; out.push(`<h${level}>${inline(heading[2])}</h${level}>`);
    } else if (/^\s*([-*_])(?:\s*\1){2,}\s*$/.test(line)) { flushParagraph(); flushList(); out.push('<hr>'); }
    else if (/^>\s?/.test(line)) { flushParagraph(); flushList(); out.push(`<blockquote>${inline(line.replace(/^>\s?/,''))}</blockquote>`); }
    else { flushList(); paragraph.push(line); }
  }
  if (code !== null) out.push(`<pre><code>${escapeHTML(code.join('\n'))}</code></pre>`);
  flushParagraph(); flushList();
  return out.join('\n');
}

// Safe markdown rendering for note bodies and answers. Raw HTML in the
// source is shown as text, links and images only keep http(s)/mailto/
// relative URLs, so a note (or an answer quoting one) cannot run script.

import { Marked } from 'marked';

const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

// safeURL keeps web, mail and relative links; anything else (javascript:,
// data:, vbscript:...) is dropped.
export function safeURL(href) {
  const u = String(href ?? '').trim();
  if (/^(https?:|mailto:)/i.test(u)) return u;
  if (/^[a-z][a-z0-9+.-]*:/i.test(u) || u.startsWith('//')) return null;
  return u;
}

const md = new Marked({
  gfm: true,
  breaks: false,
  renderer: {
    html(html) {
      return esc(html);
    },
    link(href, title, text) {
      const url = safeURL(href);
      if (url === null) return text;
      const t = title ? ` title="${esc(title)}"` : '';
      return `<a href="${esc(url)}"${t} target="_blank" rel="noopener noreferrer">${text}</a>`;
    },
    image(href, title, text) {
      const url = safeURL(href);
      if (url === null || !/^https?:/i.test(url)) return esc(text || '');
      return `<img src="${esc(url)}" alt="${esc(text || '')}" loading="lazy" referrerpolicy="no-referrer">`;
    },
  },
});

export function renderMarkdown(src) {
  return md.parse(String(src ?? ''));
}

// renderAnswer renders an answer and turns [i], [ii][iii] citations into
// superscript numerals.
export function renderAnswer(src) {
  return renderMarkdown(src).replace(/\[((?:x{0,1}(?:ix|iv|v?i{0,3})))\]/g, (m, n) => (n ? `<sup>${n}</sup>` : m));
}

// Render generated markup only inside an opaque-origin, scriptless iframe.
// Rebuild an allowlisted document: never attach untrusted parsed nodes to the UI.
const allowedTags: Record<string, true> = {
  html: true, head: true, body: true, title: true, style: true, div: true, span: true,
  p: true, main: true, section: true, article: true, header: true, footer: true,
  h1: true, h2: true, h3: true, strong: true, em: true, small: true, br: true,
  svg: true, g: true, path: true, circle: true, ellipse: true, rect: true, line: true,
  polyline: true, polygon: true, text: true, tspan: true, defs: true,
  lineargradient: true, radialgradient: true, stop: true, clippath: true,
  mask: true, pattern: true, filter: true, fegaussianblur: true, feoffset: true,
  femerge: true, femergenode: true, animate: true, animatetransform: true,
  animatemotion: true, mpath: true, use: true, desc: true,
};
const forbiddenAttributes: Record<string, true> = {
  src: true, srcset: true, action: true, formaction: true, target: true, ping: true,
  srcdoc: true, data: true, poster: true, background: true, nonce: true,
  integrity: true, is: true,
};
export function buildPelicanPreview(answer: string): string | null {
  if (typeof document === 'undefined' || answer.length > 1024 * 1024) return null;
  const markup = answer.trim().replace(/^```(?:html|svg|xml)?\s*\n?([\s\S]*?)\n?```$/i, '$1');
  if (!/<(?:html|svg|div|main|section)\b/i.test(markup)) return null;
  const template = document.createElement('template');
  template.innerHTML = markup;
  // Remove entire non-display subtrees, including meta refresh, frames and foreignObject.
  for (const element of Array.from(template.content.querySelectorAll('*'))) {
    if (allowedTags[element.localName.toLowerCase()] !== true) {
      element.remove();
      continue;
    }
    for (const attribute of Array.from(element.attributes)) {
      const name = attribute.name.toLowerCase();
      if (name.startsWith('on') || forbiddenAttributes[name] === true ||
          ((name === 'href' || name === 'xlink:href') && !attribute.value.startsWith('#')) ||
          (name === 'attributename' && /href|src|on/i.test(attribute.value))) {
        element.removeAttribute(attribute.name);
      }
    }
  }
  const csp = "default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; img-src 'none'; font-src 'none'; connect-src 'none'; media-src 'none'; object-src 'none'; frame-src 'none'; base-uri 'none'; form-action 'none'";
  return `<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="${csp}"><meta name="referrer" content="no-referrer"></head><body>${template.innerHTML}</body></html>`;
}

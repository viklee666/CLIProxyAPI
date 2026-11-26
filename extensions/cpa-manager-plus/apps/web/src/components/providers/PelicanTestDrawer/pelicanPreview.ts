// Cockpit-compatible execution in an opaque iframe, wrapped in a passive outer
// frame whose frame-src also blocks the generated frame navigating to a server.
// Never parse generated markup into the host DOM or strip html/body attributes.
const resourcePolicy = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; font-src data:; connect-src 'none'; media-src 'none'; worker-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; webrtc 'block'";
const innerPolicy = `${resourcePolicy}; frame-src 'none'; child-src 'none'`;
const outerPolicy = `${resourcePolicy}; frame-src data: blob:; child-src data: blob:`;
const lockdown = `(() => {
  for (const key of ['RTCPeerConnection', 'webkitRTCPeerConnection', 'mozRTCPeerConnection', 'RTCDataChannel', 'WebTransport']) {
    try { Object.defineProperty(globalThis, key, {value: undefined, writable: false, configurable: false}); } catch {}
  }
  for (const key of ['alert', 'confirm', 'prompt', 'print']) {
    try { Object.defineProperty(globalThis, key, {value: () => undefined, writable: false, configurable: false}); } catch {}
  }
})()`;

export function buildPelicanPreview(answer: string): string | null {
  if (answer.length > 1024 * 1024) return null;
  const fenced = /```(?:html|svg|xml)?\s*\n([\s\S]*?)```/i.exec(answer);
  const markup = (fenced?.[1] ?? answer).trim();
  if (!/<(?:html|svg|div|main|section|canvas)\b/i.test(markup)) return null;
  // The browser merges later html/body attributes into the implied root nodes.
  // Lockdown and CSP precede every generated script, including head scripts.
  const inner = `<!doctype html><meta http-equiv="Content-Security-Policy" content="${innerPolicy}"><meta name="referrer" content="no-referrer"><script>${lockdown}</script>${markup}`;
  const escaped = inner.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  return `<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="${outerPolicy}"><meta name="referrer" content="no-referrer"><style>html,body{margin:0;width:100%;height:100%;overflow:hidden}iframe{display:block;width:100%;height:100%;border:0}</style></head><body><iframe title="Generated animation" sandbox="allow-scripts" referrerpolicy="no-referrer" srcdoc="${escaped}"></iframe></body></html>`;
}

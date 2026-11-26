import { describe, expect, it } from 'vitest';
import { buildPelicanPreview } from './pelicanPreview';

const innerDocument = (outer: string): string => {
  const source = /srcdoc="([^"]*)"/.exec(outer)?.[1];
  expect(source).toBeDefined();
  return source!.replace(/&quot;/g, '"').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&amp;/g, '&');
};

describe('isolated animation document', () => {
  it('preserves the complete document, root styles, canvas, and animation scripts', () => {
    const html = '<!DOCTYPE html><html lang="zh" style="background:#123"><head><script>window.headAnimation = true</script></head><body class="scene" style="margin:0"><canvas id="canvas"></canvas><svg><foreignObject><div>octopus</div></foreignObject></svg><script>requestAnimationFrame(draw)</script></body></html>';
    const outer = buildPelicanPreview(`Here is the animation:\n\`\`\`html\n${html}\n\`\`\``)!;
    const inner = innerDocument(outer);
    expect(inner.endsWith(html)).toBe(true);
    expect(inner.indexOf('RTCPeerConnection')).toBeLessThan(inner.indexOf('window.headAnimation'));
    expect(inner).toContain("script-src 'unsafe-inline'");
    expect(outer).not.toContain('<canvas');
    expect(outer).not.toContain('<script>');
    expect(outer).toContain('sandbox="allow-scripts"');
    expect(outer).not.toContain('allow-same-origin');
  });

  it('blocks network, nested frames, workers, forms and WebRTC before generated content', () => {
    const outer = buildPelicanPreview('<html><body><script>location.href="https://outside.invalid"</script></body></html>')!;
    const inner = innerDocument(outer);
    for (const directive of ["default-src 'none'", "connect-src 'none'", "worker-src 'none'", "form-action 'none'", "object-src 'none'", "base-uri 'none'", "webrtc 'block'"]) {
      expect(inner).toContain(directive);
      expect(outer).toContain(directive);
    }
    expect(inner).toContain("frame-src 'none'; child-src 'none'");
    expect(outer).toContain('frame-src data: blob:; child-src data: blob:');
    expect(outer).toContain('referrerpolicy="no-referrer"');
    expect(inner).toContain('writable: false, configurable: false');
    expect(inner.indexOf('RTCPeerConnection')).toBeLessThan(inner.indexOf('location.href'));
  });

  it('never lets embedded markup escape the inner srcdoc attribute', () => {
    const html = '<html><body><div title="&quot;"></iframe><script>window.generated = true</script></body></html>';
    const outer = buildPelicanPreview(html)!;
    expect(innerDocument(outer).endsWith(html)).toBe(true);
    expect((outer.match(/<iframe /g) ?? []).length).toBe(1);
    expect((outer.match(/<\/iframe>/g) ?? []).length).toBe(1);
  });

  it('does not preview plain-text reasoning and caps oversized generated HTML', () => {
    expect(buildPelicanPreview('21 candies: take 12 stars, then 9 circles.')).toBeNull();
    expect(buildPelicanPreview('<html>' + ' '.repeat(1024 * 1024))).toBeNull();
  });
});

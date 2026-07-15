import { resolve } from 'node:path';

export function parseColor(value) {
  const input = String(value || '').trim().toLowerCase();
  const hex = input.match(/^#([0-9a-f]{3}|[0-9a-f]{6}|[0-9a-f]{8})$/i)?.[1];
  if (hex) {
    const expanded = hex.length === 3 ? [...hex].map((part) => part + part).join('') : hex;
    return {
      r: Number.parseInt(expanded.slice(0, 2), 16),
      g: Number.parseInt(expanded.slice(2, 4), 16),
      b: Number.parseInt(expanded.slice(4, 6), 16),
      a: expanded.length === 8 ? Number.parseInt(expanded.slice(6, 8), 16) / 255 : 1,
    };
  }

  const rgb = input.match(/^rgba?\((.*)\)$/)?.[1];
  if (!rgb) throw new Error(`Unsupported color: ${value}`);
  const normalized = rgb.replace(/,/g, ' ').replace(/\s*\/\s*/, ' ').trim().split(/\s+/);
  if (normalized.length < 3 || normalized.length > 4) throw new Error(`Unsupported color: ${value}`);
  const channels = normalized.slice(0, 3).map((part) =>
    part.endsWith('%') ? (Number.parseFloat(part) / 100) * 255 : Number.parseFloat(part),
  );
  const alpha = normalized[3]
    ? normalized[3].endsWith('%')
      ? Number.parseFloat(normalized[3]) / 100
      : Number.parseFloat(normalized[3])
    : 1;
  if ([...channels, alpha].some((part) => !Number.isFinite(part))) {
    throw new Error(`Unsupported color: ${value}`);
  }
  return { r: channels[0], g: channels[1], b: channels[2], a: alpha };
}

export function compositeColors(foreground, background) {
  const fg = typeof foreground === 'string' ? parseColor(foreground) : foreground;
  const bg = typeof background === 'string' ? parseColor(background) : background;
  const alpha = fg.a + bg.a * (1 - fg.a);
  if (alpha === 0) return { r: 0, g: 0, b: 0, a: 0 };
  return {
    r: (fg.r * fg.a + bg.r * bg.a * (1 - fg.a)) / alpha,
    g: (fg.g * fg.a + bg.g * bg.a * (1 - fg.a)) / alpha,
    b: (fg.b * fg.a + bg.b * bg.a * (1 - fg.a)) / alpha,
    a: alpha,
  };
}

export function relativeLuminance(color) {
  const parsed = typeof color === 'string' ? parseColor(color) : color;
  const channel = (value) => {
    const normalized = value / 255;
    return normalized <= 0.04045
      ? normalized / 12.92
      : ((normalized + 0.055) / 1.055) ** 2.4;
  };
  return channel(parsed.r) * 0.2126 + channel(parsed.g) * 0.7152 + channel(parsed.b) * 0.0722;
}

export function contrastRatio(foreground, background) {
  const foregroundLuminance = relativeLuminance(foreground);
  const backgroundLuminance = relativeLuminance(background);
  const lighter = Math.max(foregroundLuminance, backgroundLuminance);
  const darker = Math.min(foregroundLuminance, backgroundLuminance);
  return (lighter + 0.05) / (darker + 0.05);
}

export function minimumTextContrast({ fontSize, fontWeight }) {
  const size = Number.parseFloat(fontSize);
  const weight = Number.parseInt(fontWeight, 10) || 400;
  return size >= 24 || (size >= 18.66 && weight >= 700) ? 3 : 4.5;
}

export function auditColorPairs(pairs) {
  return pairs.map((pair) => {
    const ratio = contrastRatio(pair.foreground, pair.background);
    return { ...pair, ratio, passed: ratio >= pair.minimum };
  });
}

export function buildContrastReport({ runId, entries, scenarios }) {
  const violations = entries.filter((entry) => entry.status === 'violation');
  const warnings = entries.filter((entry) => entry.status === 'manual-review');
  return {
    version: 1,
    runId,
    generatedAt: new Date().toISOString(),
    scenarios,
    summary: {
      checked: entries.filter((entry) => entry.status === 'passed').length,
      violations: violations.length,
      warnings: warnings.length,
    },
    violations,
    warnings,
  };
}

export function resolveAuditDirectory(artifactDir, runId) {
  if (!artifactDir) throw new Error('ANYCODE_ARTIFACT_DIR is required');
  if (!/^[a-z0-9][a-z0-9._-]*$/i.test(runId)) throw new Error('Invalid audit run id');
  const base = resolve(artifactDir);
  const output = resolve(base, 'dark-theme-audit', runId);
  if (!output.startsWith(`${base}/`)) throw new Error('Audit output escapes ANYCODE_ARTIFACT_DIR');
  return output;
}

export function visibleContrastAuditExpression() {
  return `(() => {
    const parse = (value) => {
      const parts = String(value).match(/[\\d.]+/g)?.map(Number) || [];
      return { r: parts[0] || 0, g: parts[1] || 0, b: parts[2] || 0, a: parts[3] ?? 1 };
    };
    const composite = (foreground, background) => {
      const alpha = foreground.a + background.a * (1 - foreground.a);
      if (!alpha) return { r: 0, g: 0, b: 0, a: 0 };
      return {
        r: (foreground.r * foreground.a + background.r * background.a * (1 - foreground.a)) / alpha,
        g: (foreground.g * foreground.a + background.g * background.a * (1 - foreground.a)) / alpha,
        b: (foreground.b * foreground.a + background.b * background.a * (1 - foreground.a)) / alpha,
        a: alpha,
      };
    };
    const luminance = (color) => {
      const channel = (value) => {
        const normalized = value / 255;
        return normalized <= 0.04045 ? normalized / 12.92 : ((normalized + 0.055) / 1.055) ** 2.4;
      };
      return channel(color.r) * 0.2126 + channel(color.g) * 0.7152 + channel(color.b) * 0.0722;
    };
    const ratio = (foreground, background) => {
      const values = [luminance(foreground), luminance(background)].sort((a, b) => b - a);
      return (values[0] + 0.05) / (values[1] + 0.05);
    };
    const visible = (element) => {
      const style = getComputedStyle(element);
      const rect = element.getBoundingClientRect();
      return style.display !== 'none' && style.visibility !== 'hidden' && Number(style.opacity) > 0 &&
        rect.width > 0 && rect.height > 0 && rect.right > 0 && rect.bottom > 0 && rect.left < innerWidth && rect.top < innerHeight;
    };
    const backgroundFor = (element) => {
      let current = element;
      let background = { r: 255, g: 255, b: 255, a: 1 };
      const layers = [];
      while (current) {
        const style = getComputedStyle(current);
        if (style.backgroundImage && style.backgroundImage !== 'none') return { manual: style.backgroundImage };
        const color = parse(style.backgroundColor);
        if (color.a > 0) layers.push(color);
        current = current.parentElement;
      }
      for (let index = layers.length - 1; index >= 0; index -= 1) background = composite(layers[index], background);
      return { color: background };
    };
    const selectorFor = (element) => {
      if (element.id) return '#' + CSS.escape(element.id);
      const classes = Array.from(element.classList).filter(Boolean).slice(0, 3).map((name) => '.' + CSS.escape(name)).join('');
      return element.tagName.toLowerCase() + classes;
    };
    const results = [];
    const elements = Array.from(document.querySelectorAll('body *')).filter(visible);
    for (const element of elements) {
      if (element.matches(':disabled, [aria-disabled="true"], .disabled') ||
          element.closest(':disabled, [aria-disabled="true"], .disabled')) continue;
      const ownText = Array.from(element.childNodes).filter((node) => node.nodeType === Node.TEXT_NODE).map((node) => node.textContent).join(' ').trim();
      const inputText = element instanceof HTMLInputElement || element instanceof HTMLTextAreaElement
        ? element.value || element.placeholder
        : '';
      const sample = ownText || inputText;
      if (!sample) continue;
      const style = getComputedStyle(element);
      const background = backgroundFor(element);
      if (background.manual) {
        results.push({ status: 'manual-review', selector: selectorFor(element), text: sample.slice(0, 80), reason: 'background-image' });
        continue;
      }
      const foreground = parse(style.color);
      const renderedForeground = composite(foreground, background.color);
      const actual = ratio(renderedForeground, background.color);
      const fontSize = Number.parseFloat(style.fontSize);
      const fontWeight = Number.parseInt(style.fontWeight, 10) || 400;
      const minimum = fontSize >= 24 || (fontSize >= 18.66 && fontWeight >= 700) ? 3 : 4.5;
      results.push({
        status: actual >= minimum ? 'passed' : 'violation',
        selector: selectorFor(element),
        text: sample.slice(0, 80),
        foreground: style.color,
        background: 'rgb(' + [background.color.r, background.color.g, background.color.b].map((value) => Math.round(value)).join(', ') + ')',
        ratio: Number(actual.toFixed(2)),
        minimum,
      });
    }
    return results;
  })()`;
}

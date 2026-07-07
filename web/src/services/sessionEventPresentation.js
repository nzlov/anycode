function escapeHtml(value) {
  return String(value)
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');
}

function inlineMarkdown(value) {
  let html = escapeHtml(value);
  html = html.replace(/`([^`]+)`/g, '<code>$1</code>');
  html = html.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  html = html.replace(/\*([^*\n]+)\*/g, '<em>$1</em>');
  html = html.replace(/\n/g, '<br>');
  return html;
}

export function renderMarkdown(markdown) {
  const lines = String(markdown || '').replace(/\r\n?/g, '\n').split('\n');
  const blocks = [];
  let listItems = [];
  let paragraph = [];
  let codeLines = [];
  let inCode = false;

  function flushParagraph() {
    if (paragraph.length === 0) return;
    blocks.push(`<p>${inlineMarkdown(paragraph.join('\n'))}</p>`);
    paragraph = [];
  }

  function flushList() {
    if (listItems.length === 0) return;
    blocks.push(`<ul>${listItems.map((item) => `<li>${inlineMarkdown(item)}</li>`).join('')}</ul>`);
    listItems = [];
  }

  function flushCode() {
    if (!inCode) return;
    blocks.push(`<pre><code>${escapeHtml(codeLines.join('\n'))}</code></pre>`);
    codeLines = [];
    inCode = false;
  }

  for (const line of lines) {
    if (line.trim().startsWith('```')) {
      if (inCode) {
        flushCode();
      } else {
        flushParagraph();
        flushList();
        inCode = true;
      }
      continue;
    }
    if (inCode) {
      codeLines.push(line);
      continue;
    }

    const heading = /^(#{1,3})\s+(.+)$/.exec(line);
    if (heading) {
      flushParagraph();
      flushList();
      const level = heading[1].length + 2;
      blocks.push(`<h${level}>${inlineMarkdown(heading[2])}</h${level}>`);
      continue;
    }

    const list = /^\s*[-*]\s+(.+)$/.exec(line);
    if (list) {
      flushParagraph();
      listItems.push(list[1]);
      continue;
    }

    if (line.trim() === '') {
      flushParagraph();
      flushList();
      continue;
    }
    flushList();
    paragraph.push(line.trim());
  }

  flushCode();
  flushParagraph();
  flushList();
  return blocks.join('');
}

export function codexCommandResultBody(item) {
  const exitCode = numberValue(item?.exit_code);
  const output = firstString(item?.aggregated_output, item?.output, item?.text);
  const prefix = exitCode === null || exitCode === 0 ? '命令完成' : `命令完成，退出码 ${exitCode}`;
  return [prefix, output].filter(Boolean).join('\n');
}

function firstString(...values) {
  for (const value of values) {
    if (typeof value === 'string') return value;
  }
  return '';
}

function numberValue(value) {
  return typeof value === 'number' ? value : null;
}

function isCommandEvent(event) {
  return event?.kind === 'tool' && event.title === '执行命令';
}

function isResultEvent(event) {
  return event?.kind === 'tool' && event.title === '命令结果';
}

export function mergeShellEvents(events) {
  const merged = [];
  const consumed = new Set();
  for (let index = 0; index < events.length; index += 1) {
    if (consumed.has(index)) continue;
    const event = events[index];
    const resultIndex = findShellResultIndex(events, index + 1);
    const next = resultIndex === -1 ? null : events[resultIndex];
    if (isCommandEvent(event) && next) {
      const command = event.body.trim();
      const result = next.body.trim();
      merged.push({
        ...event,
        id: `${event.id}:${next.id}`,
        title: command ? `Shell ${command}` : 'Shell',
        body: `命令\n${command}${result ? `\n\n结果\n${result}` : ''}`,
        time: next.time || event.time,
      });
      consumed.add(resultIndex);
      continue;
    }
    if (isCommandEvent(event)) {
      const command = event.body.trim();
      merged.push({
        ...event,
        title: command ? `Shell ${command}` : 'Shell',
        body: command ? `命令\n${command}` : '',
      });
      continue;
    }
    merged.push(event);
  }
  return merged;
}

function findShellResultIndex(events, startIndex) {
  for (let index = startIndex; index < events.length; index += 1) {
    const event = events[index];
    if (isResultEvent(event)) return index;
    if (event?.kind === 'tool') return -1;
  }
  return -1;
}

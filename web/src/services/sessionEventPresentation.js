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
  const lines = String(markdown || '')
    .replace(/\r\n?/g, '\n')
    .split('\n');
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

export function codexCommandResultBody(item, normalizedItem) {
  return commandOutputBody(
    firstNonEmptyString(normalizedItem?.output, item?.aggregated_output, item?.output, item?.text),
  );
}

export function codexMessageImages(item) {
  const parts = [item?.content, item?.output].filter((value) => Array.isArray(value)).flat();
  return parts.flatMap((part) => {
    if (!part || part.type !== 'input_image' || typeof part.image_url !== 'string') return [];
    const image = { src: part.image_url };
    if (typeof part.detail === 'string' && part.detail !== '') image.detail = part.detail;
    return [image];
  });
}

export function prepareTerminalOutput(value) {
  const escapeCode = String.fromCharCode(27);
  const orphanSgr = new RegExp(
    `(^|\\n|${escapeCode}\\[[0-?]*[ -/]*[@-~])(?:(?:38|48);2;\\d{1,3};\\d{1,3};\\d{1,3}|(?:38|48);5;\\d{1,3})m`,
    'g',
  );
  return String(value || '')
    .replace(/␛\[/g, `${escapeCode}[`)
    .replace(orphanSgr, '$1');
}

export function compactEventPayload(payload) {
  return Object.entries(payload)
    .filter(([key]) => !['processRunId', 'codexEventId'].includes(key))
    .map(([key, value]) => {
      if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
        return `${key}: ${value}`;
      }
      if (value === null) return `${key}: null`;
      if (typeof value === 'object') return `${key}: ${JSON.stringify(value)}`;
      return '';
    })
    .filter(Boolean)
    .join(' · ');
}

function firstNonEmptyString(...values) {
  for (const value of values) {
    if (typeof value === 'string' && value !== '') return value;
  }
  return '';
}

function shellCommandDisplay(value) {
  const command = String(value || '').trim();
  const shell = /^(?:(?:\S*\/)?(?:bash|sh|zsh))\s+-lc\s+([\s\S]+)$/.exec(command);
  if (!shell) return command;
  return unquoteShellArgument(shell[1].trim());
}

function unquoteShellArgument(value) {
  if (value.length < 2) return value;
  const quote = value[0];
  if ((quote !== "'" && quote !== '"') || value[value.length - 1] !== quote) return value;
  const inner = value.slice(1, -1);
  if (quote === "'") return inner.replace(/'\\''/g, "'");
  return inner.replace(/\\(["\\$`])/g, '$1');
}

function isCommandEvent(event) {
  return event?.kind === 'tool' && event.title === '执行命令';
}

function isResultEvent(event) {
  return event?.kind === 'tool' && event.title === '命令结果';
}

function isToolStartEvent(event) {
  return event?.kind === 'tool' && (event.toolPhase === 'started' || isCommandEvent(event));
}

function isToolResultEvent(event) {
  return event?.kind === 'tool' && (event.toolPhase === 'completed' || isResultEvent(event));
}

function isFileChangeEvent(event) {
  return event?.kind === 'file_change';
}

function resultMatchesCommand(result, command) {
  if (!result?.command) return true;
  return shellCommandDisplay(result.command) === command;
}

export function mergeSessionEvents(events) {
  const merged = [];
  const openTools = [];
  const openFileChanges = new Map();
  for (let index = 0; index < events.length; index += 1) {
    const event = events[index];
    if (isFileChangeEvent(event)) {
      const fileChangeId = event.fileChangeId || event.id;
      const openEntry = openFileChanges.get(fileChangeId);
      if (openEntry) {
        openEntry.title = event.title || openEntry.title;
        openEntry.body = event.body || openEntry.body;
        openEntry.time = event.time || openEntry.time;
        openEntry.fileChanges = event.fileChanges || openEntry.fileChanges;
        openFileChanges.delete(fileChangeId);
        continue;
      }
      const entry = { ...event };
      merged.push(entry);
      openFileChanges.set(fileChangeId, entry);
      continue;
    }
    if (isToolStartEvent(event)) {
      const isCommand = isCommandEvent(event) || Boolean(event.command);
      const command = shellCommandDisplay(event.command || event.body);
      const entry = {
        ...event,
        title: isCommand ? (command ? `Shell ${command}` : 'Shell') : event.title,
        body: isCommand ? '' : event.body,
      };
      merged.push(entry);
      openTools.push({
        command,
        entry,
        input: event.body,
        isCommand,
        toolCallId: event.toolCallId || '',
      });
      continue;
    }
    if (isToolResultEvent(event)) {
      const toolIndex = findOpenToolIndex(openTools, event);
      if (toolIndex !== -1) {
        const tool = openTools[toolIndex];
        tool.entry.body = tool.isCommand ? shellBody(event.body) : toolBody(tool.input, event.body);
        tool.entry.time = event.time || tool.entry.time;
        tool.entry.images = event.images || tool.entry.images;
        openTools.splice(toolIndex, 1);
        continue;
      }
      const command = shellCommandDisplay(event.command);
      const entry = { ...event, body: shellBody(event.body) };
      if (command) entry.title = `Shell ${command}`;
      merged.push(entry);
      continue;
    }
    merged.push(event);
  }
  return merged;
}

function toolBody(input, output) {
  const inputText = String(input || '').trim();
  const outputText = String(output || '').trim();
  if (!inputText) return outputText;
  if (!outputText) return inputText;
  return `输入\n${inputText}\n\n输出\n${outputText}`;
}

function shellBody(resultBody) {
  return commandOutputBody(resultBody);
}

function commandOutputBody(value) {
  return String(value || '').replace(/^命令完成(?:，退出码 \d+)?\n?/, '');
}

function findOpenToolIndex(openTools, result) {
  if (result.toolCallId) {
    return openTools.findIndex((tool) => tool.toolCallId === result.toolCallId);
  }
  if (!isResultEvent(result)) return -1;
  const resultCommand = result.command ? shellCommandDisplay(result.command) : '';
  for (let index = openTools.length - 1; index >= 0; index -= 1) {
    if (!resultCommand) return index;
    if (resultMatchesCommand(result, openTools[index].command)) return index;
  }
  return -1;
}

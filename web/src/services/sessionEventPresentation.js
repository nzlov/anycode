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
  return commandOutputBody(firstNonEmptyString(item?.aggregated_output, item?.output, item?.text));
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

function isFileChangeEvent(event) {
  return event?.kind === 'file_change';
}

function resultMatchesCommand(result, command) {
  if (!result?.command) return true;
  return shellCommandDisplay(result.command) === command;
}

export function mergeShellEvents(events) {
  const merged = [];
  const openCommands = [];
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
    if (isCommandEvent(event)) {
      const command = shellCommandDisplay(event.command || event.body);
      const entry = {
        ...event,
        title: command ? `Shell ${command}` : 'Shell',
        body: '',
      };
      merged.push(entry);
      openCommands.push({ command, entry, toolCallId: event.toolCallId || '' });
      continue;
    }
    if (isResultEvent(event)) {
      const commandIndex = findOpenCommandIndex(openCommands, event);
      if (commandIndex !== -1) {
        const command = openCommands[commandIndex];
        command.entry.body = shellBody(event.body);
        command.entry.time = event.time || command.entry.time;
        openCommands.splice(commandIndex, 1);
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

function shellBody(resultBody) {
  return commandOutputBody(resultBody);
}

function commandOutputBody(value) {
  return String(value || '').replace(/^命令完成(?:，退出码 \d+)?\n?/, '');
}

function findOpenCommandIndex(openCommands, result) {
  if (result.toolCallId) {
    return openCommands.findIndex((command) => command.toolCallId === result.toolCallId);
  }
  const resultCommand = result.command ? shellCommandDisplay(result.command) : '';
  for (let index = openCommands.length - 1; index >= 0; index -= 1) {
    if (!resultCommand) return index;
    if (resultMatchesCommand(result, openCommands[index].command)) return index;
  }
  return -1;
}

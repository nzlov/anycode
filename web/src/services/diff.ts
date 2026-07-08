import { graphqlFetch } from '@/services/graphqlClient';
import type { PageInfo } from '@/services/sessions';

export type DiffMode = 'single' | 'all';
export type DiffLineKind = 'context' | 'add' | 'delete' | 'header';

export interface DiffFile {
  path: string;
  status: string;
  additions: number;
  deletions: number;
}

export interface DiffLine {
  id: string;
  kind: DiffLineKind;
  content: string;
  oldLine: number | null;
  newLine: number | null;
}

export interface DiffHunk {
  id: string;
  header: string;
  oldStart: number;
  newStart: number;
  canExpandBefore: boolean;
  canExpandAfter: boolean;
  lines: DiffLine[];
}

export interface FileDiff {
  file: DiffFile;
  hunks: DiffHunk[];
  lines: DiffLine[];
}

export interface SessionDiff {
  mode: DiffMode;
  filePath: string;
  files: DiffFile[];
  pageInfo: PageInfo;
  fileDiff: FileDiff | null;
  allDiff: FileDiff[];
  available: boolean;
}

export interface GetSessionDiffInput {
  sessionId: string;
  mode: DiffMode;
  filePath?: string;
  page: number;
  pageSize: number;
  contextBefore?: number;
  contextAfter?: number;
}

export interface GetBranchDiffInput {
  projectId: string;
  branch: string;
  mode: DiffMode;
  filePath?: string;
  page: number;
  pageSize: number;
  contextBefore?: number;
  contextAfter?: number;
}

export interface CommitRecord {
  hash: string;
  shortHash: string;
  subject: string;
  authorName: string;
  authorEmail: string;
  createdAt: string;
}

export interface SessionCommitHistory {
  commits: CommitRecord[];
  pageInfo: PageInfo;
  available: boolean;
}

interface GraphQLDiffFile {
  path: string;
  status: string;
  additions: number;
  deletions: number;
}

interface GraphQLDiffLine {
  kind: string;
  content: string;
}

interface GraphQLDiffHunk {
  header: string;
  oldStart: number;
  newStart: number;
  canExpandBefore: boolean;
  canExpandAfter: boolean;
  lines: GraphQLDiffLine[];
}

interface GraphQLFileDiff {
  file: GraphQLDiffFile;
  hunks: GraphQLDiffHunk[];
}

interface GraphQLSessionDiff {
  mode: string;
  filePath: string;
  available: boolean;
  files: {
    items: GraphQLDiffFile[];
    pageInfo: PageInfo;
  };
  fileDiff?: GraphQLFileDiff | null;
  allDiff?: GraphQLFileDiff[];
}

interface GraphQLSessionCommitHistory {
  available: boolean;
  commits: {
    items: CommitRecord[];
    pageInfo: PageInfo;
  };
}

export async function getSessionSingleDiff(input: GetSessionDiffInput): Promise<SessionDiff> {
  const variablesInput: {
    sessionId: string;
    mode: DiffMode;
    filePath?: string;
    page: number;
    pageSize: number;
    contextBefore?: number;
    contextAfter?: number;
  } = {
    sessionId: input.sessionId,
    mode: input.mode,
    page: input.page,
    pageSize: input.pageSize,
  };
  if (input.filePath) {
    variablesInput.filePath = input.filePath;
  }
  if (input.contextBefore) {
    variablesInput.contextBefore = input.contextBefore;
  }
  if (input.contextAfter) {
    variablesInput.contextAfter = input.contextAfter;
  }

  const data = await graphqlFetch<
    { sessionDiff: GraphQLSessionDiff },
    {
      input: {
        sessionId: string;
        mode: DiffMode;
        filePath?: string;
        page: number;
        pageSize: number;
        contextBefore?: number;
        contextAfter?: number;
      };
    }
  >({
    query: `
      query SessionSingleDiff($input: SessionDiffInput!) {
        sessionDiff(input: $input) {
          mode
          filePath
          available
          files {
            items {
              path
              status
              additions
              deletions
            }
            pageInfo {
              page
              pageSize
              total
              nextCursor
            }
          }
          fileDiff {
            file {
              path
              status
              additions
              deletions
            }
            hunks {
              header
              oldStart
              newStart
              canExpandBefore
              canExpandAfter
              lines {
                kind
                content
              }
            }
          }
        }
      }
    `,
    variables: {
      input: variablesInput,
    },
  });

  return normalizeSessionDiff(data.sessionDiff);
}

export async function getSessionAllDiff(input: GetSessionDiffInput): Promise<SessionDiff> {
  const variablesInput: {
    sessionId: string;
    mode: DiffMode;
    page: number;
    pageSize: number;
    contextBefore?: number;
    contextAfter?: number;
  } = {
    sessionId: input.sessionId,
    mode: 'all',
    page: input.page,
    pageSize: input.pageSize,
  };
  if (input.contextBefore) {
    variablesInput.contextBefore = input.contextBefore;
  }
  if (input.contextAfter) {
    variablesInput.contextAfter = input.contextAfter;
  }

  const data = await graphqlFetch<
    { sessionDiff: GraphQLSessionDiff },
    { input: typeof variablesInput }
  >({
    query: `
      query SessionAllDiff($input: SessionDiffInput!) {
        sessionDiff(input: $input) {
          mode
          filePath
          available
          files {
            items {
              path
              status
              additions
              deletions
            }
            pageInfo {
              page
              pageSize
              total
              nextCursor
            }
          }
          allDiff {
            file {
              path
              status
              additions
              deletions
            }
            hunks {
              header
              oldStart
              newStart
              canExpandBefore
              canExpandAfter
              lines {
                kind
                content
              }
            }
          }
        }
      }
    `,
    variables: {
      input: variablesInput,
    },
  });

  return normalizeSessionDiff(data.sessionDiff);
}

export async function getSessionDiffFiles(input: GetSessionDiffInput): Promise<SessionDiff> {
  const data = await graphqlFetch<
    { sessionDiff: GraphQLSessionDiff },
    { input: { sessionId: string; mode: DiffMode; page: number; pageSize: number } }
  >({
    query: `
      query SessionDiffFiles($input: SessionDiffInput!) {
        sessionDiff(input: $input) {
          mode
          filePath
          available
          files {
            items {
              path
              status
              additions
              deletions
            }
            pageInfo {
              page
              pageSize
              total
              nextCursor
            }
          }
        }
      }
    `,
    variables: {
      input: {
        sessionId: input.sessionId,
        mode: input.mode,
        page: input.page,
        pageSize: input.pageSize,
      },
    },
  });

  return normalizeSessionDiff(data.sessionDiff);
}

export async function getSessionFileDiff(input: GetSessionDiffInput): Promise<FileDiff | null> {
  const variablesInput: {
    sessionId: string;
    mode: DiffMode;
    filePath?: string;
    page: number;
    pageSize: number;
    contextBefore?: number;
    contextAfter?: number;
  } = {
    sessionId: input.sessionId,
    mode: 'single',
    page: input.page,
    pageSize: input.pageSize,
  };
  if (input.filePath) {
    variablesInput.filePath = input.filePath;
  }
  if (input.contextBefore) {
    variablesInput.contextBefore = input.contextBefore;
  }
  if (input.contextAfter) {
    variablesInput.contextAfter = input.contextAfter;
  }

  const data = await graphqlFetch<
    { sessionDiff: GraphQLSessionDiff },
    { input: typeof variablesInput }
  >({
    query: `
      query SessionFileDiff($input: SessionDiffInput!) {
        sessionDiff(input: $input) {
          mode
          filePath
          available
          files {
            items {
              path
              status
              additions
              deletions
            }
            pageInfo {
              page
              pageSize
              total
              nextCursor
            }
          }
          fileDiff {
            file {
              path
              status
              additions
              deletions
            }
            hunks {
              header
              oldStart
              newStart
              canExpandBefore
              canExpandAfter
              lines {
                kind
                content
              }
            }
          }
        }
      }
    `,
    variables: { input: variablesInput },
  });

  return normalizeSessionDiff(data.sessionDiff).fileDiff;
}

export async function getBranchSingleDiff(input: GetBranchDiffInput): Promise<SessionDiff> {
  const variablesInput: {
    projectId: string;
    branch: string;
    mode: DiffMode;
    filePath?: string;
    page: number;
    pageSize: number;
    contextBefore?: number;
    contextAfter?: number;
  } = {
    projectId: input.projectId,
    branch: input.branch,
    mode: input.mode,
    page: input.page,
    pageSize: input.pageSize,
  };
  if (input.filePath) {
    variablesInput.filePath = input.filePath;
  }
  if (input.contextBefore) {
    variablesInput.contextBefore = input.contextBefore;
  }
  if (input.contextAfter) {
    variablesInput.contextAfter = input.contextAfter;
  }

  const data = await graphqlFetch<
    { branchDiff: GraphQLSessionDiff },
    { input: typeof variablesInput }
  >({
    query: `
      query BranchSingleDiff($input: BranchDiffInput!) {
        branchDiff(input: $input) {
          mode
          filePath
          available
          files {
            items {
              path
              status
              additions
              deletions
            }
            pageInfo {
              page
              pageSize
              total
              nextCursor
            }
          }
          fileDiff {
            file {
              path
              status
              additions
              deletions
            }
            hunks {
              header
              oldStart
              newStart
              canExpandBefore
              canExpandAfter
              lines {
                kind
                content
              }
            }
          }
        }
      }
    `,
    variables: {
      input: variablesInput,
    },
  });

  return normalizeSessionDiff(data.branchDiff);
}

export async function getBranchAllDiff(input: GetBranchDiffInput): Promise<SessionDiff> {
  const variablesInput: {
    projectId: string;
    branch: string;
    mode: DiffMode;
    page: number;
    pageSize: number;
    contextBefore?: number;
    contextAfter?: number;
  } = {
    projectId: input.projectId,
    branch: input.branch,
    mode: 'all',
    page: input.page,
    pageSize: input.pageSize,
  };
  if (input.contextBefore) {
    variablesInput.contextBefore = input.contextBefore;
  }
  if (input.contextAfter) {
    variablesInput.contextAfter = input.contextAfter;
  }

  const data = await graphqlFetch<
    { branchDiff: GraphQLSessionDiff },
    { input: typeof variablesInput }
  >({
    query: `
      query BranchAllDiff($input: BranchDiffInput!) {
        branchDiff(input: $input) {
          mode
          filePath
          available
          files {
            items {
              path
              status
              additions
              deletions
            }
            pageInfo {
              page
              pageSize
              total
              nextCursor
            }
          }
          allDiff {
            file {
              path
              status
              additions
              deletions
            }
            hunks {
              header
              oldStart
              newStart
              canExpandBefore
              canExpandAfter
              lines {
                kind
                content
              }
            }
          }
        }
      }
    `,
    variables: {
      input: variablesInput,
    },
  });

  return normalizeSessionDiff(data.branchDiff);
}

export async function getSessionCommitHistory(input: {
  sessionId: string;
  page: number;
  pageSize: number;
}): Promise<SessionCommitHistory> {
  const data = await graphqlFetch<
    { sessionCommitHistory: GraphQLSessionCommitHistory },
    { input: { sessionId: string; page: number; pageSize: number } }
  >({
    query: `
      query SessionCommitHistory($input: SessionCommitHistoryInput!) {
        sessionCommitHistory(input: $input) {
          available
          commits {
            items {
              hash
              shortHash
              subject
              authorName
              authorEmail
              createdAt
            }
            pageInfo {
              page
              pageSize
              total
              nextCursor
            }
          }
        }
      }
    `,
    variables: { input },
  });
  return {
    available: data.sessionCommitHistory.available,
    commits: data.sessionCommitHistory.commits.items,
    pageInfo: data.sessionCommitHistory.commits.pageInfo,
  };
}

function normalizeSessionDiff(diff: GraphQLSessionDiff): SessionDiff {
  return {
    mode: diff.mode === 'all' ? 'all' : 'single',
    filePath: diff.filePath,
    available: diff.available,
    files: diff.files.items,
    pageInfo: diff.files.pageInfo,
    fileDiff: diff.fileDiff ? normalizeFileDiff(diff.fileDiff) : null,
    allDiff: (diff.allDiff ?? []).map(normalizeFileDiff),
  };
}

function normalizeFileDiff(diff: GraphQLFileDiff): FileDiff {
  const hunks = diff.hunks.map(normalizeHunk);
  return {
    file: diff.file,
    hunks,
    lines: hunks.flatMap((hunk) => hunk.lines),
  };
}

function normalizeHunk(hunk: GraphQLDiffHunk, hunkIndex: number): DiffHunk {
  let oldLine = hunk.oldStart;
  let newLine = hunk.newStart;
  const lines: DiffLine[] = [
    {
      id: `${hunkIndex}:header`,
      kind: 'header',
      content: hunk.header,
      oldLine: null,
      newLine: null,
    },
  ];

  hunk.lines.forEach((line, lineIndex) => {
    const kind = normalizeLineKind(line.kind);
    const currentOldLine = kind === 'add' ? null : oldLine;
    const currentNewLine = kind === 'delete' ? null : newLine;

    lines.push({
      id: `${hunkIndex}:${lineIndex}`,
      kind,
      content: line.content,
      oldLine: currentOldLine,
      newLine: currentNewLine,
    });

    if (kind !== 'add') {
      oldLine += 1;
    }
    if (kind !== 'delete') {
      newLine += 1;
    }
  });

  return {
    id: String(hunkIndex),
    header: hunk.header,
    oldStart: hunk.oldStart,
    newStart: hunk.newStart,
    canExpandBefore: hunk.canExpandBefore,
    canExpandAfter: hunk.canExpandAfter,
    lines,
  };
}

function normalizeLineKind(kind: string): DiffLineKind {
  switch (kind) {
    case 'add':
    case 'delete':
    case 'header':
      return kind;
    default:
      return 'context';
  }
}

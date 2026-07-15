import { graphqlFetch } from '@/services/graphqlClient';
import type { PageInfo } from '@/services/sessions';

export type DiffMode = 'single' | 'all';
export type DiffLineKind = 'context' | 'add' | 'delete' | 'header';
export type SessionDiffSummaryState = 'changed' | 'clean' | 'unavailable' | 'error';

export type DiffWorkspaceTarget =
  { kind: 'session'; sessionId: string } | { kind: 'branch'; projectId: string; branch: string };

export interface DiffWorkspaceState {
  mode: DiffMode;
  filePath: string;
}

export interface SessionDiffSummary {
  sessionId: string;
  state: SessionDiffSummaryState;
  filesChanged: number;
}

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
  fileDiff: FileDiff | null;
  allDiff: FileDiff[];
  available: boolean;
}

export interface GetSessionDiffInput {
  sessionId: string;
  mode: DiffMode;
  filePath?: string;
  contextBefore?: number;
  contextAfter?: number;
}

export interface GetBranchDiffInput {
  projectId: string;
  branch: string;
  mode: DiffMode;
  filePath?: string;
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
  files: GraphQLDiffFile[];
  fileDiff?: GraphQLFileDiff | null;
  allDiff?: GraphQLFileDiff[];
}

interface GraphQLSessionDiffSummary {
  sessionId: string;
  state: string;
  filesChanged: number;
}

interface GraphQLSessionCommitHistory {
  available: boolean;
  commits: {
    items: CommitRecord[];
    pageInfo: PageInfo;
  };
}

export async function getSessionDiffSummaries(sessionIds: string[]): Promise<SessionDiffSummary[]> {
  const uniqueSessionIds = [...new Set(sessionIds.filter(Boolean))];
  if (uniqueSessionIds.length === 0) return [];

  const data = await graphqlFetch<
    { sessionDiffSummaries: GraphQLSessionDiffSummary[] },
    { sessionIds: string[] }
  >({
    query: `
      query SessionDiffSummaries($sessionIds: [ID!]!) {
        sessionDiffSummaries(sessionIds: $sessionIds) {
          sessionId
          state
          filesChanged
        }
      }
    `,
    variables: { sessionIds: uniqueSessionIds },
    notify: false,
  });

  return data.sessionDiffSummaries.map(normalizeSessionDiffSummary);
}

export async function getSessionSingleDiff(input: GetSessionDiffInput): Promise<SessionDiff> {
  const variablesInput: {
    sessionId: string;
    mode: DiffMode;
    filePath?: string;
    contextBefore?: number;
    contextAfter?: number;
  } = {
    sessionId: input.sessionId,
    mode: input.mode,
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
            path
            status
            additions
            deletions
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
    contextBefore?: number;
    contextAfter?: number;
  } = {
    sessionId: input.sessionId,
    mode: 'all',
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
            path
            status
            additions
            deletions
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

export async function getBranchSingleDiff(input: GetBranchDiffInput): Promise<SessionDiff> {
  const variablesInput: {
    projectId: string;
    branch: string;
    mode: DiffMode;
    filePath?: string;
    contextBefore?: number;
    contextAfter?: number;
  } = {
    projectId: input.projectId,
    branch: input.branch,
    mode: input.mode,
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
            path
            status
            additions
            deletions
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
    contextBefore?: number;
    contextAfter?: number;
  } = {
    projectId: input.projectId,
    branch: input.branch,
    mode: 'all',
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
            path
            status
            additions
            deletions
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
    files: diff.files,
    fileDiff: diff.fileDiff ? normalizeFileDiff(diff.fileDiff) : null,
    allDiff: (diff.allDiff ?? []).map(normalizeFileDiff),
  };
}

function normalizeSessionDiffSummary(summary: GraphQLSessionDiffSummary): SessionDiffSummary {
  const state = normalizeSessionDiffSummaryState(summary.state);
  return {
    sessionId: summary.sessionId,
    state,
    filesChanged: state === 'changed' ? Math.max(0, summary.filesChanged) : 0,
  };
}

function normalizeSessionDiffSummaryState(state: string): SessionDiffSummaryState {
  switch (state) {
    case 'changed':
    case 'clean':
    case 'unavailable':
      return state;
    default:
      return 'error';
  }
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

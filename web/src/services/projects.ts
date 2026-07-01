import { directoryTree, projects as mockProjects } from '@/mocks/workbench';
import { graphqlFetch } from '@/services/graphqlClient';

export interface ProjectSummary {
  id: string;
  name: string;
  path: string;
  active: boolean;
  defaultBranch: string;
  openSessions: number;
}

export interface DirectoryEntry {
  name: string;
  path: string;
  isDir: boolean;
  isGit: boolean;
  canRead: boolean;
  errorCode: string;
  children?: DirectoryEntry[];
}

export interface DirectoryPage {
  path: string;
  parent: string;
  entries: DirectoryEntry[];
}

interface GraphQLProject {
  id: string;
  name: string;
  path: string;
  gitState: {
    currentBranch: string;
    branches: {
      name: string;
      isCurrent: boolean;
    }[];
  };
}

interface GraphQLDirectoryEntry {
  name: string;
  path: string;
  isDir: boolean;
  isGit: boolean;
  canRead: boolean;
  errorCode: string;
}

interface MockDirectoryNode {
  label: string;
  icon: string;
  selectable?: boolean;
  children?: MockDirectoryNode[];
}

const projectFields = `
  id
  name
  path
  gitState {
    currentBranch
    branches {
      name
      isCurrent
    }
  }
`;

export async function listProjects() {
  try {
    const data = await graphqlFetch<{ projects: GraphQLProject[] }>({
      query: `
        query Projects {
          projects {
            ${projectFields}
          }
        }
      `,
    });
    return normalizeProjects(data.projects);
  } catch {
    return mockProjects.map((project, index) => ({
      ...project,
      active: index === 0,
    }));
  }
}

export async function browseDirectory(path = '/') {
  try {
    const data = await graphqlFetch<
      {
        browseDirectory: {
          path: string;
          parent: string;
          entries: GraphQLDirectoryEntry[];
        };
      },
      { input: { path: string } }
    >({
      query: `
        query BrowseDirectory($input: BrowseDirectoryInput!) {
          browseDirectory(input: $input) {
            path
            parent
            entries {
              name
              path
              isDir
              isGit
              canRead
              errorCode
            }
          }
        }
      `,
      variables: { input: { path } },
    });
    return data.browseDirectory;
  } catch {
    return mockDirectoryPage();
  }
}

export async function createProject(input: { path: string; name: string }) {
  try {
    const data = await graphqlFetch<
      { createProject: GraphQLProject },
      { input: { path: string; name: string } }
    >({
      query: `
        mutation CreateProject($input: CreateProjectInput!) {
          createProject(input: $input) {
            ${projectFields}
          }
        }
      `,
      variables: { input },
    });
    return normalizeProject(data.createProject, false);
  } catch {
    return {
      id: slugProjectId(input.name || input.path),
      name: input.name || basename(input.path),
      path: input.path,
      active: false,
      defaultBranch: 'main',
      openSessions: 0,
    };
  }
}

function normalizeProjects(projects: GraphQLProject[]) {
  return projects.map((project, index) => normalizeProject(project, index === 0));
}

function normalizeProject(project: GraphQLProject, active: boolean): ProjectSummary {
  const currentBranch = project.gitState.currentBranch;
  const defaultBranch =
    currentBranch ||
    project.gitState.branches.find((branch) => branch.isCurrent)?.name ||
    project.gitState.branches[0]?.name ||
    'main';

  return {
    id: project.id,
    name: project.name,
    path: project.path,
    active,
    defaultBranch,
    openSessions: 0,
  };
}

function mockDirectoryPage(): DirectoryPage {
  return {
    path: '/',
    parent: '',
    entries: directoryTree.map((node) => mockNodeToDirectoryEntry(node, '')),
  };
}

function mockNodeToDirectoryEntry(node: MockDirectoryNode, parentPath: string): DirectoryEntry {
  const path = `${parentPath}/${node.label}`.replaceAll('//', '/');
  const entry: DirectoryEntry = {
    name: node.label,
    path,
    isDir: true,
    isGit: Boolean(node.label !== 'workspaces'),
    canRead: true,
    errorCode: '',
  };
  if (node.children) {
    entry.children = node.children.map((child) => mockNodeToDirectoryEntry(child, path));
  }
  return entry;
}

function basename(path: string) {
  return path.split('/').filter(Boolean).at(-1) ?? path;
}

function slugProjectId(value: string) {
  return (
    basename(value)
      .toLowerCase()
      .replaceAll(/[^a-z0-9-]+/g, '-') || 'project'
  );
}

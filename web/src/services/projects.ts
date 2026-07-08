import { graphqlFetch } from '@/services/graphqlClient';

export interface ProjectSummary {
  id: string;
  name: string;
  path: string;
  active: boolean;
  isGit: boolean;
  defaultWorkflowId: string;
  openSessions: number;
}

export interface ProjectBranchState {
  defaultBranch: string;
  branches: string[];
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
  isGit: boolean;
  defaultWorkflowId?: string | null;
}

interface GraphQLGitState {
  isRepository: boolean;
  currentBranch: string;
  branches: {
    name: string;
    isCurrent: boolean;
  }[];
}

interface GraphQLDirectoryEntry {
  name: string;
  path: string;
  isDir: boolean;
  isGit: boolean;
  canRead: boolean;
  errorCode: string;
}

const projectFields = `
  id
  name
  path
  isGit
  defaultWorkflowId
`;

export async function listProjects() {
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
}

export async function browseDirectory(path = '/') {
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
}

export async function getProjectBranches(projectId: string, options: { refresh?: boolean } = {}) {
  const data = await graphqlFetch<
    { projectGitState: GraphQLGitState },
    { projectId: string; refresh: boolean }
  >({
    query: `
      query ProjectGitState($projectId: ID!, $refresh: Boolean!) {
        projectGitState(projectId: $projectId, refresh: $refresh) {
          currentBranch
          branches {
            name
            isCurrent
          }
        }
      }
    `,
    variables: { projectId, refresh: Boolean(options.refresh) },
  });
  return normalizeBranchState(data.projectGitState);
}

export async function createProject(input: { path: string; name: string }) {
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
}

export async function removeProject(id: string) {
  const data = await graphqlFetch<{ removeProject: boolean }, { id: string }>({
    query: `
      mutation RemoveProject($id: ID!) {
        removeProject(id: $id)
      }
    `,
    variables: { id },
  });
  return data.removeProject;
}

function normalizeProjects(projects: GraphQLProject[]) {
  return projects.map((project, index) => normalizeProject(project, index === 0));
}

function normalizeProject(project: GraphQLProject, active: boolean): ProjectSummary {
  return {
    id: project.id,
    name: project.name,
    path: project.path,
    active,
    isGit: project.isGit,
    defaultWorkflowId: project.defaultWorkflowId ?? '',
    openSessions: 0,
  };
}

function normalizeBranchState(state: GraphQLGitState): ProjectBranchState {
  const defaultBranch =
    state.currentBranch ||
    state.branches.find((branch) => branch.isCurrent)?.name ||
    state.branches[0]?.name ||
    'main';
  const branches = Array.from(new Set(state.branches.map((branch) => branch.name).filter(Boolean)));
  if (!branches.includes(defaultBranch)) {
    branches.unshift(defaultBranch);
  }
  return { defaultBranch, branches };
}

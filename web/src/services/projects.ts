import { graphqlFetch } from '@/services/graphqlClient';

export interface ProjectSummary {
  id: string;
  name: string;
  path: string;
  active: boolean;
  defaultBranch: string;
  defaultWorkflowId: string;
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
  defaultWorkflowId?: string | null;
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

const projectFields = `
  id
  name
  path
  defaultWorkflowId
  gitState {
    currentBranch
    branches {
      name
      isCurrent
    }
  }
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
    defaultWorkflowId: project.defaultWorkflowId ?? '',
    openSessions: 0,
  };
}

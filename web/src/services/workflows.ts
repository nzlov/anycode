import { graphqlFetch } from '@/services/graphqlClient';

export interface WorkflowCondition {
  field: string;
  op: string;
  value?: unknown;
  all: WorkflowCondition[];
  any: WorkflowCondition[];
  not?: WorkflowCondition | null;
}

export interface WorkflowNode {
  id: string;
  type: string;
  title: string;
  prompt: string;
  approval: {
    beforeRun: boolean;
    afterRun: boolean;
  };
  retry: {
    maxAttempts: number;
  };
  merge?: {
    strategy: string;
  } | null;
}

export interface WorkflowEdge {
  from: string;
  to: string;
  priority: number;
  condition: WorkflowCondition;
}

export interface WorkflowGraph {
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
}

export interface WorkflowDefinition {
  id: string;
  projectId: string;
  name: string;
  version: number;
  graph: WorkflowGraph;
  active: boolean;
}

const workflowDefinitionFields = `
  id
  projectId
  name
  version
  active
  graph {
    nodes {
      id
      type
      title
      prompt
      approval {
        beforeRun
        afterRun
      }
      retry {
        maxAttempts
      }
      merge {
        strategy
      }
    }
    edges {
      from
      to
      priority
      condition {
        ${conditionSelection(4)}
      }
    }
  }
`;

function conditionSelection(depth: number): string {
  if (depth <= 0) {
    return `
      field
      op
      value
    `;
  }
  const child = conditionSelection(depth - 1);
  return `
    field
    op
    value
    all {
      ${child}
    }
    any {
      ${child}
    }
    not {
      ${child}
    }
  `;
}

export async function getWorkflowDefinition(id: string) {
  const data = await graphqlFetch<
    { workflowDefinition: WorkflowDefinition | null },
    { id: string }
  >({
    query: `
      query WorkflowDefinition($id: ID!) {
        workflowDefinition(id: $id) {
          ${workflowDefinitionFields}
        }
      }
    `,
    variables: { id },
  });
  return data.workflowDefinition;
}

export async function saveWorkflowDefinition(input: {
  projectId: string;
  name: string;
  graph: WorkflowGraph;
}) {
  const data = await graphqlFetch<
    { saveWorkflowDefinition: WorkflowDefinition },
    { input: { projectId: string; name: string; graph: WorkflowGraph } }
  >({
    query: `
      mutation SaveWorkflowDefinition($input: SaveWorkflowDefinitionInput!) {
        saveWorkflowDefinition(input: $input) {
          ${workflowDefinitionFields}
        }
      }
    `,
    variables: { input },
  });
  return data.saveWorkflowDefinition;
}

export async function setDefaultWorkflow(input: { projectId: string; workflowId: string }) {
  await graphqlFetch<
    { setDefaultWorkflow: { id: string; defaultWorkflowId: string | null } },
    { input: { projectId: string; workflowId: string } }
  >({
    query: `
      mutation SetDefaultWorkflow($input: SetDefaultWorkflowInput!) {
        setDefaultWorkflow(input: $input) {
          id
          defaultWorkflowId
        }
      }
    `,
    variables: { input },
  });
}

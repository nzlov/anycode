import { graphqlFetch } from '@/services/graphqlClient';

export interface MCPScope {
  kind: 'global' | 'project' | 'session';
  id?: string;
}
export interface MCPDefinition {
  transport: 'stdio' | 'http';
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  url?: string;
  headers?: Record<string, string>;
}
export interface MCPService {
  name: string;
  enabled: boolean;
  source: string;
  overridden: boolean;
  definition: MCPDefinition | null;
}
export async function getMCPServices(scope: MCPScope) {
  const data = await graphqlFetch<{ mcpServices: MCPService[] }, { scope: MCPScope }>({
    query: `query MCPServices($scope: MCPScopeInput!) { mcpServices(scope: $scope) { name enabled source overridden definition } }`,
    variables: { scope },
    notify: false,
  });
  return data.mcpServices;
}
export async function saveMCPService(
  scope: MCPScope,
  name: string,
  enabled?: boolean,
  definition?: MCPDefinition,
) {
  await graphqlFetch({
    query: `mutation SaveMCPService($scope: MCPScopeInput!, $name: String!, $enabled: Boolean, $definition: JSON) { saveMCPService(scope: $scope, name: $name, enabled: $enabled, definition: $definition) }`,
    variables: { scope, name, enabled, definition },
  });
}
export async function deleteMCPService(scope: MCPScope, name: string) {
  await graphqlFetch({
    query: `mutation DeleteMCPService($scope: MCPScopeInput!, $name: String!) { deleteMCPService(scope: $scope, name: $name) }`,
    variables: { scope, name },
  });
}
export async function checkMCPService(scope: MCPScope, name: string) {
  const data = await graphqlFetch<{ checkMCPService: number }, { scope: MCPScope; name: string }>({
    query: `mutation CheckMCPService($scope: MCPScopeInput!, $name: String!) { checkMCPService(scope: $scope, name: $name) }`,
    variables: { scope, name },
    notify: false,
  });
  return data.checkMCPService;
}

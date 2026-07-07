export function buildFlowNode(node) {
  const position = node.position ?? { x: 0, y: 0 };
  return {
    id: node.id,
    type: 'workflow',
    position: { x: Number(position.x) || 0, y: Number(position.y) || 0 },
    data: {
      nodeType: node.type,
      title: node.title || node.id,
      retry: Number(node.retry?.maxAttempts ?? 0),
    },
  };
}

export function buildFlowEdge(edge) {
  return {
    id: workflowEdgeId(edge),
    source: edge.from,
    target: edge.to,
    type: 'smoothstep',
    markerEnd: 'arrowclosed',
    label: edgeCaption(edge),
    data: {
      priority: edge.priority,
      condition: edge.condition,
    },
  };
}

export function syncWorkflowNodePositions(workflowNodes, flowNodes) {
  const positions = new Map(flowNodes.map((node) => [node.id, node.position]));
  workflowNodes.forEach((node) => {
    const position = positions.get(node.id);
    if (!position) return;
    node.position = { x: Number(position.x) || 0, y: Number(position.y) || 0 };
  });
}

export function clientPointToFlowPoint(event, bounds, projectPoint) {
  const localPoint = {
    x: Number(event?.clientX ?? 0) - Number(bounds?.left ?? 0),
    y: Number(event?.clientY ?? 0) - Number(bounds?.top ?? 0),
  };
  return projectPoint(localPoint);
}

export function workflowEdgeId(edge) {
  return `${edge.from}->${edge.to}:${edge.priority}`;
}

function edgeCaption(edge) {
  if (edge.condition?.mode === 'expr') return `priority ${edge.priority} · expr`;
  if (!edge.condition?.field && !edge.condition?.op) return `priority ${edge.priority} · always`;
  return `priority ${edge.priority} · ${edge.condition.field} ${edge.condition.op}`;
}

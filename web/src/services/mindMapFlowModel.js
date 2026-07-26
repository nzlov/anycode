const nodeWidth = 172;
const nodeHeight = 48;

export function buildRadialLayout(nodes, edges, rootNodeId = 'project-root') {
  const layout = {};
  if (!nodes.length) return layout;

  const nodeIds = new Set(nodes.map((node) => node.id));
  const centerId = nodeIds.has(rootNodeId) ? rootNodeId : nodes[0].id;
  const adjacent = new Map(nodes.map((node) => [node.id, []]));
  for (const edge of edges) {
    if (!nodeIds.has(edge.sourceId) || !nodeIds.has(edge.targetId)) continue;
    adjacent.get(edge.sourceId).push(edge.targetId);
    adjacent.get(edge.targetId).push(edge.sourceId);
  }

  const depths = new Map([[centerId, 0]]);
  const queue = [centerId];
  for (let index = 0; index < queue.length; index += 1) {
    const current = queue[index];
    for (const relatedId of adjacent.get(current)) {
      if (depths.has(relatedId)) continue;
      depths.set(relatedId, depths.get(current) + 1);
      queue.push(relatedId);
    }
  }

  const outerDepth = Math.max(1, Math.max(...depths.values()) + 1);
  const rings = new Map();
  for (const node of nodes) {
    if (node.id === centerId) continue;
    const depth = depths.get(node.id) ?? outerDepth;
    const ring = rings.get(depth) ?? [];
    ring.push(node.id);
    rings.set(depth, ring);
  }

  layout[centerId] = { x: -nodeWidth / 2, y: -nodeHeight / 2 };
  for (const [depth, ring] of [...rings.entries()].sort(([a], [b]) => a - b)) {
    const radius = Math.max(depth * 240, (ring.length * 190) / (2 * Math.PI));
    ring.forEach((id, index) => {
      const angle = -Math.PI / 2 + (2 * Math.PI * index) / ring.length;
      layout[id] = {
        x: radius * Math.cos(angle) - nodeWidth / 2,
        y: radius * Math.sin(angle) - nodeHeight / 2,
      };
    });
  }
  return layout;
}

export function radialEdgeHandles(edge, layout) {
  const source = layout[edge.sourceId] ?? { x: 0, y: 0 };
  const target = layout[edge.targetId] ?? { x: 0, y: 0 };
  const sourceSide = direction(source, target);
  return {
    sourceHandle: `source-${sourceSide}`,
    targetHandle: `target-${opposite(sourceSide)}`,
  };
}

function direction(source, target) {
  const dx = target.x - source.x;
  const dy = target.y - source.y;
  if (Math.abs(dx) >= Math.abs(dy)) return dx >= 0 ? 'right' : 'left';
  return dy >= 0 ? 'bottom' : 'top';
}

function opposite(side) {
  return { top: 'bottom', right: 'left', bottom: 'top', left: 'right' }[side];
}

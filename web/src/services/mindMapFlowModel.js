const nodeWidth = 172;
const nodeHeight = 56;

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
  let previousRadius = 0;
  for (const [depth, ring] of [...rings.entries()].sort(([a], [b]) => a - b)) {
    const radius = Math.max(depth * 240, previousRadius + 240, (ring.length * 190) / (2 * Math.PI));
    previousRadius = radius;
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

export function buildNestedLayout(nodes, edges, rootNodeId = 'project-root') {
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
  for (const related of adjacent.values()) related.sort();

  const parent = new Map([[centerId, '']]);
  const queue = [centerId];
  for (let index = 0; index < queue.length; index += 1) {
    const current = queue[index];
    for (const relatedId of adjacent.get(current)) {
      if (parent.has(relatedId)) continue;
      parent.set(relatedId, current);
      queue.push(relatedId);
    }
  }
  for (const node of nodes) {
    if (!parent.has(node.id)) parent.set(node.id, centerId);
  }

  const children = new Map(nodes.map((node) => [node.id, []]));
  for (const [id, parentId] of parent) {
    if (parentId) children.get(parentId).push(id);
  }
  for (const childIds of children.values()) childIds.sort();

  layout[centerId] = { x: -nodeWidth / 2, y: -nodeHeight / 2 };
  const rootChildren = children.get(centerId);
  const branchBreadths = measureBranchBreadths(centerId, children);
  const totalBreadth = rootChildren.reduce((total, id) => total + branchBreadths.get(id), 0);
  const rootRadius = Math.max(320, totalBreadth / (2 * Math.PI));
  let rootCursor = -Math.PI / 2;
  rootChildren.forEach((id) => {
    const share = branchBreadths.get(id) / totalBreadth;
    const angle = rootCursor + Math.PI * share;
    rootCursor += 2 * Math.PI * share;
    placeBranch(id, centerId, angle, rootRadius, children, branchBreadths, layout);
  });

  applyCrossLinkOffsets(edges, parent, layout, centerId);
  resolveNodeOverlaps(layout, centerId);
  return layout;
}

function measureBranchBreadths(id, children, breadths = new Map()) {
  const childIds = children.get(id);
  const breadth = childIds.length
    ? childIds.reduce(
        (total, childId) => total + measureBranchBreadths(childId, children, breadths).get(childId),
        0,
      )
    : 190;
  breadths.set(id, Math.max(190, breadth));
  return breadths;
}

function placeBranch(id, parentId, angle, distance, children, breadths, layout) {
  const parentPosition = layout[parentId];
  layout[id] = {
    x: parentPosition.x + distance * Math.cos(angle),
    y: parentPosition.y + distance * Math.sin(angle),
  };

  const childIds = children.get(id);
  if (!childIds.length) return;
  const spread = Math.min(Math.PI, Math.max(Math.PI / 3, (childIds.length - 1) * 0.42));
  const totalBreadth = childIds.reduce((total, childId) => total + breadths.get(childId), 0);
  const childRadius = Math.max(220, totalBreadth / spread);
  let cursor = angle - spread / 2;
  childIds.forEach((childId) => {
    const share = breadths.get(childId) / totalBreadth;
    const childAngle = childIds.length === 1 ? angle : cursor + (spread * share) / 2;
    cursor += spread * share;
    placeBranch(childId, id, childAngle, childRadius, children, breadths, layout);
  });
}

function applyCrossLinkOffsets(edges, parent, layout, centerId) {
  const offsets = new Map();
  for (const edge of edges) {
    if (!layout[edge.sourceId] || !layout[edge.targetId]) continue;
    if (parent.get(edge.sourceId) === edge.targetId || parent.get(edge.targetId) === edge.sourceId)
      continue;
    addOffset(offsets, edge.sourceId, layout[edge.targetId], layout[edge.sourceId]);
    addOffset(offsets, edge.targetId, layout[edge.sourceId], layout[edge.targetId]);
  }

  for (const [id, offset] of offsets) {
    if (id === centerId) continue;
    const length = Math.hypot(offset.x, offset.y);
    const scale = length > 72 ? 72 / length : 1;
    layout[id] = {
      x: layout[id].x + offset.x * scale,
      y: layout[id].y + offset.y * scale,
    };
  }
}

function addOffset(offsets, id, related, current) {
  const offset = offsets.get(id) ?? { x: 0, y: 0, count: 0 };
  offset.x = (offset.x * offset.count + (related.x - current.x) * 0.12) / (offset.count + 1);
  offset.y = (offset.y * offset.count + (related.y - current.y) * 0.12) / (offset.count + 1);
  offset.count += 1;
  offsets.set(id, offset);
}

function resolveNodeOverlaps(layout, centerId) {
  const nodeIds = Object.keys(layout).sort();
  const horizontalSpacing = nodeWidth + 24;
  const verticalSpacing = nodeHeight + 24;
  for (let iteration = 0; iteration < 40; iteration += 1) {
    let moved = false;
    for (let leftIndex = 0; leftIndex < nodeIds.length; leftIndex += 1) {
      for (let rightIndex = leftIndex + 1; rightIndex < nodeIds.length; rightIndex += 1) {
        const leftId = nodeIds[leftIndex];
        const rightId = nodeIds[rightIndex];
        const left = layout[leftId];
        const right = layout[rightId];
        const dx = right.x - left.x;
        const dy = right.y - left.y;
        const overlapX = horizontalSpacing - Math.abs(dx);
        const overlapY = verticalSpacing - Math.abs(dy);
        if (overlapX <= 0 || overlapY <= 0) continue;

        if (overlapX <= overlapY) {
          separatePair(left, right, leftId, rightId, centerId, 'x', dx, overlapX + 1);
        } else {
          separatePair(left, right, leftId, rightId, centerId, 'y', dy, overlapY + 1);
        }
        moved = true;
      }
    }
    if (!moved) return;
  }
}

function separatePair(left, right, leftId, rightId, centerId, axis, delta, distance) {
  const direction = delta === 0 ? (leftId < rightId ? 1 : -1) : Math.sign(delta);
  if (leftId === centerId) {
    right[axis] += direction * distance;
    return;
  }
  if (rightId === centerId) {
    left[axis] -= direction * distance;
    return;
  }
  left[axis] -= (direction * distance) / 2;
  right[axis] += (direction * distance) / 2;
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

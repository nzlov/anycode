import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  buildFlowEdge,
  buildFlowNode,
  clientPointToFlowPoint,
  syncWorkflowNodePositions,
} from '../src/services/workflowFlowModel.js';

test('buildFlowNode maps workflow node position and metadata into Vue Flow node', () => {
  const node = buildFlowNode({
    id: 'implement',
    type: 'codex',
    title: '实现',
    position: { x: 420, y: -120 },
    retry: { maxAttempts: 2 },
  });

  assert.equal(node.id, 'implement');
  assert.deepEqual(node.position, { x: 420, y: -120 });
  assert.equal(node.type, 'workflow');
  assert.equal(node.data.nodeType, 'codex');
  assert.equal(node.data.title, '实现');
  assert.equal(node.data.retry, 2);
});

test('buildFlowEdge preserves workflow edge data for editing after selection', () => {
  const edge = buildFlowEdge({
    from: 'implement',
    to: 'verify',
    priority: 3,
    condition: { mode: 'field', field: 'results.status', op: 'eq', value: 'passed', expr: '', all: [], any: [], not: null },
  });

  assert.equal(edge.id, 'implement->verify:3');
  assert.equal(edge.source, 'implement');
  assert.equal(edge.target, 'verify');
  assert.equal(edge.type, 'smoothstep');
  assert.equal(edge.data.priority, 3);
  assert.equal(edge.data.condition.field, 'results.status');
});

test('syncWorkflowNodePositions writes Vue Flow positions back to workflow nodes', () => {
  const nodes = [
    { id: 'implement', position: { x: 0, y: 0 } },
    { id: 'verify', position: { x: 300, y: 0 } },
  ];
  syncWorkflowNodePositions(nodes, [
    { id: 'verify', position: { x: 640, y: 220 } },
    { id: 'implement', position: { x: -180, y: 40 } },
  ]);

  assert.deepEqual(nodes, [
    { id: 'implement', position: { x: -180, y: 40 } },
    { id: 'verify', position: { x: 640, y: 220 } },
  ]);
});

test('clientPointToFlowPoint converts mouse coordinates through the canvas bounds', () => {
  const calls = [];
  const result = clientPointToFlowPoint(
    { clientX: 430, clientY: 260 },
    { left: 120, top: 80 },
    (point) => {
      calls.push(point);
      return { x: point.x / 2, y: point.y / 2 };
    },
  );

  assert.deepEqual(calls, [{ x: 310, y: 180 }]);
  assert.deepEqual(result, { x: 155, y: 90 });
});

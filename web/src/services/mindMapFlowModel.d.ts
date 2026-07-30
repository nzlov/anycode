import type { MindMapEdge, MindMapNode } from '@/services/mindMaps';

export interface MindMapFlowPosition {
  x: number;
  y: number;
}

export function buildRadialLayout(
  nodes: MindMapNode[],
  edges: MindMapEdge[],
  rootNodeId?: string,
): Record<string, MindMapFlowPosition>;

export function buildNestedLayout(
  nodes: MindMapNode[],
  edges: MindMapEdge[],
  rootNodeId?: string,
): Record<string, MindMapFlowPosition>;

export function radialEdgeHandles(
  edge: MindMapEdge,
  layout: Record<string, MindMapFlowPosition>,
): {
  sourceHandle: string;
  targetHandle: string;
};

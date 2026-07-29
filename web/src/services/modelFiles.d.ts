export type ModelFileFormat = 'glb' | 'gltf' | 'obj' | 'stl' | '3mf';

export function modelFileFormat(filename: string): ModelFileFormat | null;

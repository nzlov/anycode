const modelFileFormats = new Set(['glb', 'gltf', 'obj', 'stl', '3mf']);

export function modelFileFormat(filename) {
  const extension = String(filename).split(/[?#]/, 1)[0]?.split('.').pop()?.toLowerCase();
  return modelFileFormats.has(extension) ? extension : null;
}

export function filesFromTransfer(transfer) {
  if (!transfer) return [];
  const files = Array.from(transfer.files ?? []);
  if (files.length > 0) return files;
  return Array.from(transfer.items ?? [])
    .filter((item) => item.kind === 'file')
    .map((item) => item.getAsFile())
    .filter(Boolean);
}

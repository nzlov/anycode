const compactTokenCountFormatter = new Intl.NumberFormat('en-US', {
  notation: 'compact',
  maximumFractionDigits: 1,
});

export function formatTokenCount(value) {
  return compactTokenCountFormatter.format(value);
}

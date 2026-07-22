function compareByUpdatedTimeDesc(left, right) {
  const leftTime = Date.parse(left.updatedTime || left.updatedAt || '');
  const rightTime = Date.parse(right.updatedTime || right.updatedAt || '');
  const safeLeft = Number.isNaN(leftTime) ? 0 : leftTime;
  const safeRight = Number.isNaN(rightTime) ? 0 : rightTime;
  if (safeRight !== safeLeft) return safeRight - safeLeft;
  return String(right.id).localeCompare(String(left.id));
}

function compareByCreatedTimeDesc(left, right) {
  const createdOrder = String(right.createdAt || '').localeCompare(String(left.createdAt || ''));
  return createdOrder || String(right.id).localeCompare(String(left.id));
}

export function createOverviewCardGroups(latestRows, historyRows) {
  const latestCards = latestRows
    .filter((card) => card.status !== 'closed')
    .sort(compareByUpdatedTimeDesc);
  const horizontalCards = [...latestCards].sort(compareByCreatedTimeDesc);
  const seenIds = new Set(latestCards.map((card) => card.id));
  const historyCards = [...latestRows.filter((card) => card.status === 'closed'), ...historyRows]
    .filter((card) => {
      if (seenIds.has(card.id)) return false;
      seenIds.add(card.id);
      return true;
    })
    .sort(compareByUpdatedTimeDesc);

  return {
    latestCards,
    horizontalCards,
    historyCards,
  };
}

export function hasNewerRelease(currentVersion, latestVersion) {
  const current = parseVersion(currentVersion);
  const latest = parseVersion(latestVersion);
  if (!current || !latest) return false;

  for (let index = 0; index < current.parts.length; index += 1) {
    if (latest.parts[index] !== current.parts[index]) {
      return latest.parts[index] > current.parts[index];
    }
  }
  return current.prerelease !== '' && latest.prerelease === '';
}

function parseVersion(value) {
  const trimmed = value.trim();
  const gitDescribe = /^(v?\d+\.\d+\.\d+)-\d+-g[0-9a-f]+(?:\+\d{8}T\d{6}Z)?$/i.exec(trimmed);
  const normalized = gitDescribe?.[1] ?? trimmed;
  const match = /^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$/.exec(
    normalized,
  );
  if (!match) return null;
  return { parts: match.slice(1, 4).map(Number), prerelease: match[4] ?? '' };
}

// Keep per-item mutations independent: one failure must not cancel the remainder.
export async function applyToItems(
  ids: string[],
  apply: (id: string) => Promise<void>,
) {
  const failed: string[] = [];
  for (const id of ids) {
    try {
      await apply(id);
    } catch {
      failed.push(id);
    }
  }
  return failed;
}

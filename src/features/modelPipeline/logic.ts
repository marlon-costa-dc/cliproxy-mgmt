import type {
  ModelPipelineInventory,
  ModelPipelineLane,
} from '@/types/modelPipeline';

/**
 * Canonical v3 lanes in display order. Unknown lanes are appended sorted by
 * tier id so they stay visible and are never silently dropped.
 */
export const CANONICAL_LANES = [
  'ai-hub-frontier',
  'ai-hub-most-capable',
  'ai-hub-balanced',
  'ai-hub-fast',
] as const;

export type HealthStatus = 'healthy' | 'degraded' | 'blocked' | 'unknown';

const HEALTH_STATUSES = ['healthy', 'degraded', 'blocked', 'unknown'];

/**
 * Map a backend health status string to a display tone. Unknown or future
 * status strings degrade to `unknown` instead of crashing the page.
 */
export function healthTone(status: string): HealthStatus {
  if (HEALTH_STATUSES.includes(status)) {
    return status as HealthStatus;
  }
  return 'unknown';
}

/**
 * Order lanes: canonical v3 lanes first in canonical order, unknown lanes
 * appended sorted by tier id.
 */
export function sortLanes(aliases: ModelPipelineLane[]): ModelPipelineLane[] {
  const rank = new Map<string, number>(
    CANONICAL_LANES.map((tierId, index) => [tierId, index]),
  );
  return [...aliases].sort((left, right) => {
    const leftRank = rank.get(left.tier_id) ?? CANONICAL_LANES.length;
    const rightRank = rank.get(right.tier_id) ?? CANONICAL_LANES.length;
    if (leftRank !== rightRank) return leftRank - rightRank;
    return left.tier_id.localeCompare(right.tier_id);
  });
}

/**
 * Shorten a `sha256:<64 hex>` digest for display, keeping the payload
 * unambiguous: `sha256` label plus first and last four hex characters.
 */
export function shortenDigest(digest: string): string {
  const prefix = 'sha256:';
  if (!digest.startsWith(prefix)) return digest;
  const hex = digest.slice(prefix.length);
  if (hex.length <= 12) return digest;
  return `sha256 ${hex.slice(0, 4)}…${hex.slice(-4)}`;
}

/**
 * Count selectable candidates across a lane's members. Zero selectable
 * candidates means the lane is effectively unavailable for routing.
 */
export function selectableCandidateCount(lane: ModelPipelineLane): number {
  let count = 0;
  for (const member of lane.members) {
    for (const candidate of member.candidates) {
      if (candidate.health.selectable) count += 1;
    }
  }
  return count;
}

/**
 * Prove the inventory response carries the v3 read model the page renders.
 * Used by tests and by the page before rendering.
 */
export function isV3Inventory(inventory: ModelPipelineInventory): boolean {
  return inventory.schema_version === 3;
}

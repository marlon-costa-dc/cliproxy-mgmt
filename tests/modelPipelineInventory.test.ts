import { describe, expect, test } from 'bun:test';
import {
  CANONICAL_LANES,
  healthTone,
  isV3Inventory,
  selectableCandidateCount,
  shortenDigest,
  sortLanes,
} from '../src/features/modelPipeline/logic';
import {
  MODEL_PIPELINE_INVENTORY_PATH,
  parseModelPipelineInventory,
} from '../src/services/api/modelPipeline';
import type { ModelPipelineInventory } from '../src/types/modelPipeline';

function buildInventory(overrides?: {
  active?: ModelPipelineInventory['active'];
  aliases?: ModelPipelineInventory['aliases'];
}): ModelPipelineInventory {
  return {
    schema_version: 3,
    generated_at: '2026-09-02T00:00:00Z',
    active: overrides?.active === undefined ? DEFAULT_ACTIVE : overrides.active,
    activation_loaded_at: '2026-09-02T00:00:00Z',
    binary_provenance: {
      version: '1.22.10',
      commit: 'abc1234',
      built_at: '2026-09-01T00:00:00Z',
    },
    routing_schema: { version: 2, digest: 'sha256:' + 'd'.repeat(64) },
    direct_models: [],
    aliases: overrides?.aliases ?? [],
  };
}

const DEFAULT_ACTIVE: ModelPipelineInventory['active'] = {
  generation: 7,
  snapshot_digest: 'sha256:' + 'a'.repeat(64),
  projection_digest: 'sha256:' + 'b'.repeat(64),
  config_digest: 'sha256:' + 'c'.repeat(64),
};

describe('parseModelPipelineInventory', () => {
  test('accepts a v3 inventory payload', () => {
    const inventory = buildInventory();
    expect(parseModelPipelineInventory(inventory)).toBe(inventory);
  });

  test('rejects a non-object payload', () => {
    expect(() => parseModelPipelineInventory(null)).toThrow();
    expect(() => parseModelPipelineInventory('x')).toThrow();
  });

  test('rejects missing schema_version', () => {
    const payload = { ...buildInventory() };
    delete payload.schema_version;
    expect(() => parseModelPipelineInventory(payload)).toThrow(/schema_version/);
  });

  test('rejects non-string generated_at', () => {
    const payload = { ...buildInventory(), generated_at: 42 };
    expect(() => parseModelPipelineInventory(payload)).toThrow(/generated_at/);
  });

  test('rejects non-array direct_models', () => {
    const payload = { ...buildInventory(), direct_models: {} };
    expect(() => parseModelPipelineInventory(payload)).toThrow(/direct_models/);
  });

  test('rejects non-array aliases', () => {
    const rejections = { ...buildInventory(), aliases: 'x' };
    expect(() => parseModelPipelineInventory(rejections)).toThrow(/aliases/);
  });
});

describe('modelPipelineApi contract', () => {
  test('exposes the read-only inventory path', () => {
    expect(MODEL_PIPELINE_INVENTORY_PATH).toBe('/model-inventory');
  });
});

describe('healthTone', () => {
  test('maps known statuses to themselves', () => {
    expect(healthTone('healthy')).toBe('healthy');
    expect(healthTone('degraded')).toBe('degraded');
    expect(healthTone('blocked')).toBe('blocked');
    expect(healthTone('unknown')).toBe('unknown');
  });

  test('degrades unknown status strings to unknown', () => {
    expect(healthTone('future-status')).toBe('unknown');
  });
});

describe('shortenDigest', () => {
  test('keeps the label and both ends of a long digest', () => {
    const digest = 'sha256:' + '0123456789abcdef'.repeat(4);
    expect(shortenDigest(digest)).toBe('sha256 0123…cdef');
  });

  test('passes through non-sha256 strings', () => {
    expect(shortenDigest('manual-lane')).toBe('manual-lane');
  });

  test('passes through short hex payloads', () => {
    expect(shortenDigest('sha256:abcd')).toBe('sha256:abcd');
  });
});

describe('sortLanes', () => {
  test('orders canonical lanes first, unknown lanes after sorted by tier id', () => {
    const lanes = [
      { tier_id: 'z-custom', name: 'z', selectable: true, reason: '', members: [] },
      { tier_id: 'ai-hub-fast', name: 'f', selectable: true, reason: '', members: [] },
      { tier_id: 'ai-hub-frontier', name: 'a', selectable: true, reason: '', members: [] },
      { tier_id: 'ai-hub-balanced', name: 'b', selectable: true, reason: '', members: [] },
    ];
    const sorted = sortLanes(lanes);
    expect(sorted.map((lane) => lane.tier_id)).toEqual([
      'ai-hub-frontier',
      'ai-hub-balanced',
      'ai-hub-fast',
      'z-custom',
    ]);
  });

  test('uses canonical lane order from CANONICAL_LANES', () => {
    expect([...CANONICAL_LANES]).toEqual([
      'ai-hub-frontier',
      'ai-hub-most-capable',
      'ai-hub-balanced',
      'ai-hub-fast',
    ]);
  });
});

describe('selectableCandidateCount', () => {
  test('counts candidates whose health is selectable across all members', () => {
    const lane = {
      tier_id: 'ai-hub-balanced',
      name: 'balanced',
      selectable: true,
      reason: '',
      members: [
        {
          model_key: { catalog_provider_id: 'p', canonical_model_id: 'm1' },
          member_rank: 1,
          model_score: '0.9',
          selection_reason: '',
          candidates: [
            { health: { status: 'healthy', selectable: true, observed_at: '', latency_ms: null } },
            { health: { status: 'blocked', selectable: false, observed_at: '', latency_ms: null } },
          ],
        },
        {
          model_key: { catalog_provider_id: 'p', canonical_model_id: 'm2' },
          member_rank: 2,
          model_score: '0.8',
          selection_reason: '',
          candidates: [
            { health: { status: 'healthy', selectable: true, observed_at: '', latency_ms: null } },
          ],
        },
      ],
    };
    expect(selectableCandidateCount(lane)).toBe(2);
  });
});

describe('isV3Inventory', () => {
  test('accepts schema v3', () => {
    expect(isV3Inventory(buildInventory())).toBe(true);
  });

  test('rejects older schemas', () => {
    const payload = { ...buildInventory(), schema_version: 2 };
    expect(isV3Inventory(payload)).toBe(false);
  });
});


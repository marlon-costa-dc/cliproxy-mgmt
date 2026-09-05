/**
 * Model Pipeline v3 — Management UI client for the read-only inventory read
 * model served by the backend at `GET /v0/management/model-inventory`.
 *
 * Display-only by contract: no POST/PUT/PATCH/DELETE call exists here, so the
 * UI exposes no write path for lane selection or routing. GET is the only verb.
 */

import { apiClient } from './client';
import { isRecord } from '@/utils/helpers';
import type { ModelPipelineInventory } from '@/types';

export const MODEL_PIPELINE_INVENTORY_PATH = '/model-inventory';

export function parseModelPipelineInventory(payload: unknown): ModelPipelineInventory {
  if (!isRecord(payload)) {
    throw new Error('invalid model pipeline inventory: response is not an object');
  }
  if (typeof payload.schema_version !== 'number') {
    throw new Error('invalid model pipeline inventory: schema_version is not a number');
  }
  if (typeof payload.generated_at !== 'string') {
    throw new Error('invalid model pipeline inventory: generated_at is not a string');
  }
  if (!isRecord(payload.binary_provenance)) {
    throw new Error('invalid model pipeline inventory: binary_provenance is not an object');
  }
  if (!isRecord(payload.routing_schema)) {
    throw new Error('invalid model pipeline inventory: routing_schema is not an object');
  }
  if (!Array.isArray(payload.direct_models)) {
    throw new Error('invalid model pipeline inventory: direct_models is not an array');
  }
  if (!Array.isArray(payload.aliases)) {
    throw new Error('invalid model pipeline inventory: aliases is not an array');
  }
  return payload as unknown as ModelPipelineInventory;
}

export const modelPipelineApi = {
  async fetchInventory(): Promise<ModelPipelineInventory> {
    const payload = await apiClient.get<unknown>(MODEL_PIPELINE_INVENTORY_PATH);
    return parseModelPipelineInventory(payload);
  },
};

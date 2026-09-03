/**
 * Model Pipeline v3 read model — typed from the backend Management API.
 *
 * Source of truth: `GET /v0/management/model-inventory` (CLIProxyAPI
 * `internal/modelrouting/inventory.go`). This surface is strictly read-only:
 * the UI never mutates pipeline selection or routing.
 */

export interface ModelPipelineModelKey {
  catalog_provider_id: string;
  canonical_model_id: string;
}

export interface ModelPipelineRouteKey {
  model_key: ModelPipelineModelKey;
  route_channel: string;
}

export interface ModelPipelineVariantKey {
  model_key: ModelPipelineModelKey;
  variant_id: string;
}

export interface ModelPipelineHealth {
  status: string;
  selectable: boolean;
  observed_at: string;
  latency_ms: number | null;
}

export interface ModelPipelineRestriction {
  rule_id: string;
  config_path: string;
  active: boolean;
  reason: string;
}

export interface ModelPipelineCredentialRef {
  id: string;
  kind: string;
}

export interface ModelPipelineQuota {
  status: string;
  remaining: string | null;
  resets_at: string | null;
  reason: string | null;
}

export interface ModelPipelineSuspension {
  active: boolean;
  reason: string | null;
  resumes_at: string | null;
}

export interface ModelPipelineCredential {
  credential_ref: ModelPipelineCredentialRef;
  quota_domain: string;
  health: ModelPipelineHealth;
  quota: ModelPipelineQuota;
  suspension: ModelPipelineSuspension;
  restrictions: ModelPipelineRestriction[];
}

export interface ModelPipelineRoute {
  route_key: ModelPipelineRouteKey;
  catalog_route_provider_id: string;
  catalog_route_model_id: string;
  runtime_model_id: string;
  route_selector: string;
  quota_domains: string[];
  protocols: string[];
  restrictions: ModelPipelineRestriction[];
  health: ModelPipelineHealth;
  selectable: boolean;
  selection_reason: string;
  credentials: ModelPipelineCredential[];
}

export interface ModelPipelineVariant {
  variant_key: ModelPipelineVariantKey;
  display_name: string | null;
  protocols: string[];
}

export interface ModelPipelineDirectModel {
  model_key: ModelPipelineModelKey;
  display_name: string;
  active: boolean;
  variants: ModelPipelineVariant[];
  routes: ModelPipelineRoute[];
}

export interface ModelPipelineCandidate {
  route_key: ModelPipelineRouteKey;
  catalog_route_provider_id: string;
  catalog_route_model_id: string;
  runtime_model_id: string;
  route_selector: string;
  variant_id: string | null;
  route_rank: number;
  quota_domains: string[];
  credential_refs: ModelPipelineCredentialRef[];
  protocols: string[];
  health: ModelPipelineHealth;
  restrictions: ModelPipelineRestriction[];
  selection_reason: string;
}

export interface ModelPipelineLaneMember {
  model_key: ModelPipelineModelKey;
  member_rank: number;
  model_score: string;
  selection_reason: string;
  candidates: ModelPipelineCandidate[];
}

export interface ModelPipelineLane {
  name: string;
  tier_id: string;
  selectable: boolean;
  reason: string;
  members: ModelPipelineLaneMember[];
}

export interface ModelPipelineActiveIdentity {
  generation: number;
  snapshot_digest: string;
  projection_digest: string;
  config_digest: string;
}

export interface ModelPipelineBinaryProvenance {
  version: string;
  commit: string;
  built_at: string;
}

export interface ModelPipelineRoutingSchema {
  version: number;
  digest: string;
}

export interface ModelPipelineInventory {
  schema_version: number;
  generated_at: string;
  active: ModelPipelineActiveIdentity | null;
  activation_loaded_at: string | null;
  binary_provenance: ModelPipelineBinaryProvenance;
  routing_schema: ModelPipelineRoutingSchema;
  direct_models: ModelPipelineDirectModel[];
  aliases: ModelPipelineLane[];
}

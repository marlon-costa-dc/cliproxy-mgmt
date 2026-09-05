import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { modelPipelineApi } from '@/services/api';
import { EmptyState } from '@/components/ui/EmptyState';
import { Skeleton } from '@/components/ui/Skeleton';
import { useAuthStore } from '@/stores';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import type { ModelPipelineInventory } from '@/types/modelPipeline';
import { healthTone, selectableCandidateCount, shortenDigest, sortLanes } from './logic';
import styles from './ModelPipelinePage.module.scss';

export function ModelPipelinePage() {
  const { t } = useTranslation();
  const connectionStatus = useAuthStore((state) => state.connectionStatus);
  const [inventory, setInventory] = useState<ModelPipelineInventory | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    try {
      setLoading(true);
      setError('');
      setInventory(await modelPipelineApi.fetchInventory());
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  useHeaderRefresh(load, connectionStatus === 'connected');

  if (loading && !inventory) {
    return (
      <div className={styles.page}>
        <Skeleton height={220} />
        <Skeleton height={140} />
      </div>
    );
  }

  if (error) {
    return (
      <div className={styles.page}>
        <EmptyState title={t('model_pipeline.load_failed_title')} description={error} />
      </div>
    );
  }

  if (!inventory) {
    return <EmptyState title={t('model_pipeline.load_failed_title')} />;
  }

  const lanes = sortLanes(inventory.aliases);
  const active = inventory.active;
  const readOnlyNote = t('model_pipeline.readonly_note');

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <h1>{t('model_pipeline.title')}</h1>
        <p className={styles.meta}>
          {t('model_pipeline.subtitle', {
            generation: active ? active.generation : null,
            schema: inventory.schema_version,
          })}
        </p>
        <p className={styles.readonlyNote}>{readOnlyNote}</p>
      </header>
      <section className={styles.section} aria-labelledby="mp-activation-heading">
        <h2 id="mp-activation-heading">{t('model_pipeline.sections.activation')}</h2>
        {active ? (
          <dl className={styles.dl}>
            <div className={styles.row}>
              <dt>{t('model_pipeline.fields.generation')}</dt>
              <dd>{active.generation}</dd>
            </div>
            <div className={styles.row}>
              <dt>{t('model_pipeline.fields.activation_loaded_at')}</dt>
              <dd>{inventory.activation_loaded_at ?? '—'}</dd>
            </div>
            <div className={styles.row}>
              <dt>{t('model_pipeline.fields.snapshot_digest')}</dt>
              <dd className={styles.mono}>{shortenDigest(active.snapshot_digest)}</dd>
            </div>
            <div className={styles.row}>
              <dt>{t('model_pipeline.fields.projection_digest')}</dt>
              <dd className={styles.mono}>{shortenDigest(active.projection_digest)}</dd>
            </div>
            <div className={styles.row}>
              <dt>{t('model_pipeline.fields.config_digest')}</dt>
              <dd className={styles.mono}>{shortenDigest(active.config_digest)}</dd>
            </div>
            <div className={styles.row}>
              <dt>{t('model_pipeline.fields.routing_schema')}</dt>
              <dd className={styles.mono}>
                v{inventory.routing_schema.version} ·{' '}
                {shortenDigest(inventory.routing_schema.digest)}
              </dd>
            </div>
          </dl>
        ) : (
          <EmptyState
            title={t('model_pipeline.unavailable_title')}
            description={t('model_pipeline.unavailable_description')}
          />
        )}
      </section>
      <section className={styles.section} aria-labelledby="mp-lanes-heading">
        <h2 id="mp-lanes-heading">{t('model_pipeline.sections.lanes')}</h2>
        {lanes.length === 0 && <EmptyState title={t('model_pipeline.no_lanes_title')} />}
        {lanes.map((lane) => {
          const selectableCount = selectableCandidateCount(lane);
          return (
            <article key={lane.tier_id} className={styles.laneCard}>
              <header className={styles.laneHeader}>
                <h3>{lane.name}</h3>
                <span className={styles.laneTier}>{lane.tier_id}</span>
                <span
                  className={styles[`tone-${healthTone(lane.selectable ? 'healthy' : 'blocked')}`]}
                >
                  {lane.selectable
                    ? t('model_pipeline.lane.available')
                    : t('model_pipeline.lane.unavailable')}
                </span>
              </header>
              <p className={styles.laneReason}>{lane.reason}</p>
              <p className={styles.laneMeta}>
                {t('model_pipeline.lane.selectable_candidates', { count: selectableCount })}
              </p>
              {lane.members.map((member) => (
                <div key={member.model_key.canonical_model_id} className={styles.member}>
                  <div className={styles.memberHeader}>
                    <span className={styles.rank}>#{member.member_rank}</span>
                    <span className={styles.modelId}>
                      {member.model_key.canonical_model_id}
                      <span className={styles.provider}>
                        @{member.model_key.catalog_provider_id}
                      </span>
                    </span>
                    <span className={styles.score}>{member.model_score}</span>
                    <span className={styles.reason}>{member.selection_reason}</span>
                  </div>
                  {member.candidates.map((candidate) => (
                    <div key={candidate.route_selector} className={styles.candidateRow}>
                      <span className={styles[`tone-${healthTone(candidate.health.status)}`]}>
                        {candidate.health.status}
                      </span>
                      <span className={styles.mono}>{candidate.runtime_model_id}</span>
                      <span className={styles.reason}>{candidate.selection_reason}</span>
                    </div>
                  ))}
                </div>
              ))}
            </article>
          );
        })}
      </section>
      <section className={styles.section} aria-labelledby="mp-models-heading">
        <h2 id="mp-models-heading">{t('model_pipeline.sections.models')}</h2>
        {inventory.direct_models.length === 0 && (
          <EmptyState title={t('model_pipeline.no_models_title')} />
        )}
        {inventory.direct_models.map((model) => (
          <article key={model.model_key.canonical_model_id} className={styles.laneCard}>
            <header className={styles.laneHeader}>
              <h3>{model.display_name}</h3>
              <span className={styles.laneTier}>
                {model.model_key.canonical_model_id}@{model.model_key.catalog_provider_id}
              </span>
              <span className={styles[`tone-${model.active ? 'healthy' : 'unknown'}`]}>
                {model.active
                  ? t('model_pipeline.models.active')
                  : t('model_pipeline.models.inactive')}
              </span>
            </header>
            {model.routes.map((route) => (
              <div key={route.route_selector} className={styles.candidateRow}>
                <span className={styles[`tone-${healthTone(route.health.status)}`]}>
                  {route.health.status}
                </span>
                <span className={styles.mono}>{route.runtime_model_id}</span>
                <span className={styles.reason}>{route.selection_reason}</span>
                {route.credentials.map((credential) => (
                  <span key={credential.credential_ref.id} className={styles.reason}>
                    {credential.quota.status}
                  </span>
                ))}
                {route.restrictions.filter((restriction) => restriction.active).length > 0 && (
                  <span className={styles.restrictionCount}>
                    {t('model_pipeline.models.active_restrictions', {
                      count: route.restrictions.filter((restriction) => restriction.active).length,
                    })}
                  </span>
                )}
              </div>
            ))}
          </article>
        ))}
      </section>
    </div>
  );
}

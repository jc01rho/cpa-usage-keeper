import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { createCPAInstance, fetchCPAInstances } from '@/lib/api'
import type { CPAInstance, IssuedCPACredential } from '@/lib/types'
import { Button } from '@/components/ui/Button'
import { Card } from '@/components/ui/Card'
import { Input } from '@/components/ui/Input'
import { Modal } from '@/components/ui/Modal'

const INSTANCE_SCOPES = ['identity:test', 'usage:push', 'metadata:push'] as const

function formatDate(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

export function CPAInstancesPanel() {
  const { t } = useTranslation()
  const [instances, setInstances] = useState<CPAInstance[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [modalOpen, setModalOpen] = useState(false)
  const [displayName, setDisplayName] = useState('')
  const [credentialName, setCredentialName] = useState('')
  const [scopes, setScopes] = useState<string[]>([...INSTANCE_SCOPES])
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState('')
  const [issuedCredential, setIssuedCredential] = useState<IssuedCPACredential | null>(null)
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle')

  const loadInstances = useCallback(async () => {
    setLoading(true)
    setLoadError('')
    try {
      setInstances(await fetchCPAInstances())
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : t('usage_stats.cpa_instances_load_failed'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    void loadInstances()
  }, [loadInstances])

  const closeCreateModal = () => {
    if (creating) return
    setModalOpen(false)
    setCreateError('')
  }

  const openCreateModal = () => {
    setDisplayName('')
    setCredentialName('')
    setScopes([...INSTANCE_SCOPES])
    setCreateError('')
    setModalOpen(true)
  }

  const toggleScope = (scope: string) => {
    setScopes((current) => current.includes(scope)
      ? current.filter((item) => item !== scope)
      : [...current, scope])
  }

  const handleCreate = async () => {
    const normalizedDisplayName = displayName.trim()
    const normalizedCredentialName = credentialName.trim()
    if (!normalizedDisplayName || !normalizedCredentialName || !scopes.includes('identity:test')) {
      setCreateError(t('usage_stats.cpa_instances_form_invalid'))
      return
    }
    setCreating(true)
    setCreateError('')
    try {
      const created = await createCPAInstance({
        displayName: normalizedDisplayName,
        credentialName: normalizedCredentialName,
        scopes,
      })
      setInstances((current) => [created.instance, ...current])
      setIssuedCredential(created.credential)
      setModalOpen(false)
      setCopyState('idle')
    } catch (error) {
      setCreateError(error instanceof Error ? error.message : t('usage_stats.cpa_instances_create_failed'))
    } finally {
      setCreating(false)
    }
  }

  const copyToken = async () => {
    if (!issuedCredential) return
    try {
      await navigator.clipboard.writeText(issuedCredential.token)
      setCopyState('copied')
    } catch {
      setCopyState('failed')
    }
  }

  return (
    <>
      <Card
        title={t('usage_stats.cpa_instances_title')}
        subtitle={t('usage_stats.cpa_instances_subtitle')}
        extra={<Button type="button" size="sm" onClick={openCreateModal}>{t('usage_stats.cpa_instances_create')}</Button>}
      >
        <div className="card-content">
          {loadError && <div className="error-box">{loadError}</div>}
          {loading ? (
            <p>{t('common.loading')}</p>
          ) : instances.length === 0 ? (
            <p className="text-muted">{t('usage_stats.cpa_instances_empty')}</p>
          ) : (
            <div className="settings-list">
              {instances.map((instance) => (
                <div className="settings-row" key={instance.instanceId}>
                  <div>
                    <strong>{instance.displayName}</strong>
                    <div className="text-muted">{instance.instanceId}</div>
                  </div>
                  <div className="text-muted">
                    {instance.enabled ? t('usage_stats.cpa_instances_enabled') : t('usage_stats.cpa_instances_disabled')}
                    {' · '}
                    {formatDate(instance.createdAt)}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </Card>

      <Modal
        open={modalOpen}
        title={t('usage_stats.cpa_instances_create_title')}
        onClose={closeCreateModal}
        closeDisabled={creating}
        footer={
          <>
            <Button type="button" variant="secondary" onClick={closeCreateModal} disabled={creating}>
              {t('common.cancel')}
            </Button>
            <Button type="button" onClick={() => void handleCreate()} loading={creating}>
              {t('usage_stats.cpa_instances_create')}
            </Button>
          </>
        }
      >
        <Input
          label={t('usage_stats.cpa_instances_display_name')}
          value={displayName}
          onChange={(event) => setDisplayName(event.target.value)}
          maxLength={128}
          disabled={creating}
        />
        <Input
          label={t('usage_stats.cpa_instances_credential_name')}
          value={credentialName}
          onChange={(event) => setCredentialName(event.target.value)}
          maxLength={128}
          disabled={creating}
        />
        <fieldset className="form-group">
          <legend>{t('usage_stats.cpa_instances_scopes')}</legend>
          {INSTANCE_SCOPES.map((scope) => (
            <label key={scope} className="checkbox-label">
              <input
                type="checkbox"
                checked={scopes.includes(scope)}
                disabled={creating || scope === 'identity:test'}
                onChange={() => toggleScope(scope)}
              />
              {scope}
            </label>
          ))}
        </fieldset>
        {createError && <div className="error-box">{createError}</div>}
      </Modal>

      <Modal
        open={issuedCredential !== null}
        title={t('usage_stats.cpa_instances_credential_title')}
        onClose={() => setIssuedCredential(null)}
      >
        <p>{t('usage_stats.cpa_instances_credential_warning')}</p>
        <Input label={t('usage_stats.cpa_instances_credential_token')} value={issuedCredential?.token ?? ''} readOnly />
        <Button type="button" onClick={() => void copyToken()}>
          {copyState === 'copied' ? t('usage_stats.cpa_instances_token_copied') : t('usage_stats.cpa_instances_copy_token')}
        </Button>
        {copyState === 'failed' && <div className="error-box">{t('usage_stats.cpa_instances_copy_failed')}</div>}
      </Modal>
    </>
  )
}

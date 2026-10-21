import type { PricingSyncSource } from '@/lib/types'

const sourceStorageKey = 'cpa-pricing-sync-source-v1'

// 同步弹窗只持久化来源选择；预览和提交草稿始终属于当前弹窗。
// 未知值统一回到默认来源；openrouter 是 fork 的 "Auto-fill from OpenRouter" 来源。
export function normalizePricingSyncSource(value: string | null): PricingSyncSource {
  return value === 'litellm' || value === 'openrouter' ? value : 'models-dev'
}

export function readPricingSyncSource(): PricingSyncSource {
  try {
    return normalizePricingSyncSource(window.localStorage.getItem(sourceStorageKey))
  } catch {
    return 'models-dev'
  }
}

export function storePricingSyncSource(source: PricingSyncSource): void {
  try { window.localStorage.setItem(sourceStorageKey, source) } catch { /* 浏览器存储不可用时仅保留当前选择。 */ }
}

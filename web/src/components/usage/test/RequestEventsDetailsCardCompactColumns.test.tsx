import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'
import { RequestEventsTestCard, extractTableHeaders, extractFirstTableRowCells, extractFirstTableRowCellMarkup } from './requestEventsFixtures'
import type { UsageEvent } from '@/lib/types'

const event: UsageEvent = {
  id: 'compact-columns',
  timestamp: '2026-08-23T09:10:11+08:00',
  api_key: 'Production Key',
  model: 'gpt-5.6',
  model_alias: 'keeper-gpt',
  reasoning_effort: 'high',
  service_tier: 'priority',
  response_service_tier: 'flex',
  endpoint: 'POST /v1/messages',
  executor_type: 'OpenAIResponsesExecutor',
  source: 'OpenAI Team',
  source_raw: 'openai-team',
  source_type: 'openai',
  auth_index: '1',
  isDelete: true,
  failed: false,
  latency_ms: 120,
  ttft_ms: 45,
  speed_tps: 30,
  client_ip: '192.0.2.10',
  x_forwarded_for: '203.0.113.5, 198.51.100.8',
  user_agent: 'keeper-client/1.0',
  tokens: {
    input_tokens: 100,
    output_tokens: 60,
    reasoning_tokens: 20,
    cache_read_tokens: 20,
    cache_creation_tokens: 5,
    total_tokens: 200,
  },
  cost_usd: 0.1234,
  cost_available: true,
  pricing_style: 'claude',
}

const renderCard = (row: UsageEvent = event) => renderToStaticMarkup(
  <RequestEventsTestCard
    events={[row]}
    modelOptions={['gpt-5.6']}
    sourceOptions={[{ value: 'openai-team', label: 'OpenAI Team' }]}
  />,
)

describe('RequestEventsDetailsCard compact columns', () => {
  // Upstream regression test: speed must not depend on TTFT.
  // Index 10 here (upstream uses 9) because the fork inserts the
  // 'CPA Instance' column at index 3.
  it.each([undefined, 0, 3000])('shows the API speed independently of TTFT %s', (ttft) => {
    const cells = extractFirstTableRowCells(renderCard({ ...event, ttft_ms: ttft }))

    expect(cells[10]).toBe('30.0 t/s')
  })

  it('renders the agreed 18 display columns in order (fork adds CPA Instance)', () => {
    const html = renderCard()

    expect(extractTableHeaders(html)).toEqual([
      'Timestamp',
      'API Key',
      'Source',
      'CPA Instance',
      'Model',
      'Effort',
      'Speed Mode',
      'Result',
      'Request',
      'Latency',
      'Speed',
      'Tokens',
      'Cache',
      'Cost',
      'Executor',
      'Client IP',
      'X-Forwarded-For',
      'User Agent',
    ])
  })

  it('stacks only the fields assigned to each compact column', () => {
    const html = renderCard()
    const cells = extractFirstTableRowCells(html)

    expect(cells).toHaveLength(18)
    expect(cells[2]).toContain('OpenAI Team')
    expect(cells[2]).toContain('Deleted')
    expect(cells[2]).not.toContain('openai')
    expect(html).toMatch(/data-provider-brand-icon="openai"[^>]*style="width:25px;height:25px"/)
    expect(cells[3]).toBe('-')
    expect(cells[4]).toBe('gpt-5.6keeper-gpt')
    expect(cells[5]).toBe('high')
    expect(cells[6]).toBe('Fast / Flex')
    expect(cells[8]).toBe('SSE/messages')
    expect(cells[9]).toBe('120msTTFT 45ms')
    expect(cells[10]).toBe('30.0 t/s')
    expect(cells[11]).toBe('2001006020')
    expect(cells[12]).toBe('20.00%205')
    expect(cells[13]).toBe('$0.1234Claude Style')
    expect(cells[14]).toBe('OpenAIResponsesExecutor')
    expect(cells.slice(15)).toEqual([
      '192.0.2.10',
      '203.0.113.5, 198.51.100.8',
      'keeper-client/1.0',
    ])
  })

  it('emphasizes standalone primary values except client metadata', () => {
    const cellMarkup = extractFirstTableRowCellMarkup(renderCard())

    for (const index of [1, 5, 6, 10, 14]) {
      expect(cellMarkup[index]).toContain('requestEventsPrimaryCell')
    }
    expect(cellMarkup[13]).toContain('requestEventsStackedPrimary')
    for (const index of [3, 15, 16, 17]) {
      expect(cellMarkup[index]).not.toContain('requestEventsPrimaryCell')
    }
  })
})

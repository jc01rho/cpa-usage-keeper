import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { RequestEventsDetailsCard } from '../RequestEventsDetailsCard';
import type { UsageEvent } from '@/lib/types';

const events: UsageEvent[] = [
  {
    id: '201',
    timestamp: '2026-08-17T02:00:00.000Z',
    api_key: 'Production Key',
    model: 'claude-sonnet',
    source: 'Provider A',
    source_raw: 'source-a',
    source_type: 'openai',
    auth_index: '1',
    failed: false,
    latency_ms: 120,
    ttft_ms: 45,
    speed_tps: 30,
    tokens: {
      input_tokens: 100,
      output_tokens: 60,
      reasoning_tokens: 20,
      cache_read_tokens: 20,
      cache_creation_tokens: 0,
      total_tokens: 200,
    },
    cost_usd: 0.1234,
    cost_available: true,
    pricing_style: 'claude',
  },
];

const renderCard = (props: Partial<React.ComponentProps<typeof RequestEventsDetailsCard>> = {}) =>
  renderToStaticMarkup(
    <RequestEventsDetailsCard
      events={events}
      loading={false}
      totalCount={1}
      modelOptions={['claude-sonnet']}
      sourceOptions={[{ value: 'source-a', label: 'Provider A' }]}
      modelFilter="__all__"
      sourceFilter="__all__"
      resultFilter="__all__"
      onModelFilterChange={() => undefined}
      onSourceFilterChange={() => undefined}
      onResultFilterChange={() => undefined}
      {...props}
    />,
  );

const textFromMarkup = (value: string) => value.replace(/<[^>]+>/g, '').replace(/\s+/g, ' ').trim();

const extractTableHeaders = (html: string) =>
  Array.from(html.matchAll(/<th\b[^>]*>(.*?)<\/th>/gs), (match) => textFromMarkup(match[1]));

const extractFirstTableRowCells = (html: string) => {
  const row = html.match(/<tbody><tr>(.*?)<\/tr><\/tbody>/s)?.[1] ?? '';
  return Array.from(row.matchAll(/<td\b[^>]*>(.*?)<\/td>/gs), (match) => textFromMarkup(match[1]));
};

describe('RequestEventsDetailsCard CPA Instance column', () => {
  it('shows CPA Instance between Source and Model by default', () => {
    const html = renderCard({
      events: [{ ...events[0], instance_id: 'inst-001' }],
      instanceNameById: new Map([['inst-001', 'Seoul Edge']]),
    });
    const headers = extractTableHeaders(html);
    const cells = extractFirstTableRowCells(html);
    const sourceIndex = headers.indexOf('Source');
    const instanceIndex = headers.indexOf('CPA Instance');
    const modelIndex = headers.indexOf('Model');

    expect(instanceIndex).toBeGreaterThanOrEqual(0);
    expect(sourceIndex).toBeLessThan(instanceIndex);
    expect(instanceIndex).toBeLessThan(modelIndex);
    expect(cells[instanceIndex]).toBe('Seoul Edge');
  });

  it('falls back to the raw instance id when no display name is known', () => {
    const html = renderCard({
      events: [{ ...events[0], instance_id: 'inst-raw' }],
    });
    const headers = extractTableHeaders(html);
    const cells = extractFirstTableRowCells(html);
    const instanceIndex = headers.indexOf('CPA Instance');

    expect(cells[instanceIndex]).toBe('inst-raw');
  });

  it('renders a dash when the event has no instance', () => {
    const html = renderCard({
      events: [{ ...events[0], instance_id: '' }],
      instanceNameById: new Map([['inst-001', 'Seoul Edge']]),
    });
    const headers = extractTableHeaders(html);
    const cells = extractFirstTableRowCells(html);
    const instanceIndex = headers.indexOf('CPA Instance');

    expect(cells[instanceIndex]).toBe('-');
  });
});

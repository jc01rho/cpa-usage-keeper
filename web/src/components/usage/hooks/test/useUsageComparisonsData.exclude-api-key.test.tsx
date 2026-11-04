// @vitest-environment happy-dom

import React, { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const apiMocks = vi.hoisted(() => ({ fetchUsageOverviewComparisons: vi.fn() }));
vi.mock('@/lib/api', async (original) => ({
  ...await original<typeof import('@/lib/api')>(),
  ...apiMocks,
}));

import { useUsageComparisonsData } from '../useUsageComparisonsData';

function Probe({ excludedApiKeyIds }: { excludedApiKeyIds?: ReadonlyArray<string> }) {
  const { comparisons } = useUsageComparisonsData({ range: 'today', excludedApiKeyIds });
  return <div data-testid="models">{comparisons?.models.map((item) => item.key).join(',') ?? 'missing'}</div>;
}

describe('usage comparisons API key exclusion', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    apiMocks.fetchUsageOverviewComparisons.mockReset();
    container = document.createElement('div');
    document.body.append(container);
    root = createRoot(container);
  });
  afterEach(() => {
    act(() => root.unmount());
    container.remove();
  });

  it('requests the comparisons again with the excluded ids and drops the unfiltered result', async () => {
    apiMocks.fetchUsageOverviewComparisons
      .mockResolvedValueOnce({ models: [{ key: 'with-excluded' }], api_keys: [] })
      .mockResolvedValueOnce({ models: [{ key: 'without-excluded' }], api_keys: [] });
    await act(async () => root.render(<Probe />));
    expect(container.querySelector('[data-testid="models"]')?.textContent).toBe('with-excluded');
    expect(apiMocks.fetchUsageOverviewComparisons.mock.calls[0][1]).not.toHaveProperty('excludedApiKeyIds');

    await act(async () => root.render(<Probe excludedApiKeyIds={['7']} />));
    expect(apiMocks.fetchUsageOverviewComparisons).toHaveBeenCalledTimes(2);
    expect(apiMocks.fetchUsageOverviewComparisons.mock.calls[1][1]).toMatchObject({ excludedApiKeyIds: ['7'] });
    expect(container.querySelector('[data-testid="models"]')?.textContent).toBe('without-excluded');
  });
});

import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  createCPAInstance,
  deleteCPAInstance,
  fetchCPAInstanceCredentials,
  fetchCPAInstances,
  revokeCPAInstanceCredential,
  updateCPAInstance,
} from './api';

const jsonResponse = (body: unknown, status = 200) => new Response(
  JSON.stringify(body),
  { status, headers: { 'Content-Type': 'application/json' } },
);

describe('CPA instance API', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('lists instances and creates an export credential through the base-path-aware admin API', async () => {
    Object.assign(globalThis, { window: { __APP_BASE_PATH__: '/keeper' } });
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({
        protocolVersion: 'keeper-export/v1',
        instances: [{
          instanceId: 'instance-1',
          displayName: 'Production CPA',
          enabled: true,
          createdAt: '2026-08-05T00:00:00.000Z',
          updatedAt: '2026-08-05T00:00:00.000Z',
        }],
      }))
      .mockResolvedValueOnce(jsonResponse({
        protocolVersion: 'keeper-export/v1',
        instance: {
          instanceId: 'instance-2',
          displayName: 'Staging CPA',
          enabled: true,
          createdAt: '2026-08-05T00:00:00.000Z',
          updatedAt: '2026-08-05T00:00:00.000Z',
        },
        credential: {
          credentialId: 'credential-2',
          name: 'staging-export',
          scopes: ['identity:test', 'usage:push', 'metadata:push'],
          token: 'one-time-secret',
          createdAt: '2026-08-05T00:00:00.000Z',
        },
      }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(fetchCPAInstances()).resolves.toHaveLength(1);
    await expect(createCPAInstance({
      displayName: 'Staging CPA',
      credentialName: 'staging-export',
    })).resolves.toMatchObject({
      instance: { instanceId: 'instance-2' },
      credential: { token: 'one-time-secret' },
    });

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      '/keeper/api/v1/instances',
      expect.objectContaining({ cache: 'no-store' }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/keeper/api/v1/instances',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({
          displayName: 'Staging CPA',
          credential: {
            name: 'staging-export',
            scopes: ['identity:test', 'usage:push', 'metadata:push'],
          },
        }),
      }),
    );
    const requestHeaders = new Headers(fetchMock.mock.calls[1]?.[1]?.headers);
    expect(requestHeaders.get('Content-Type')).toBe('application/json');
    expect(requestHeaders.get('X-CPA-Usage-Keeper-Request')).toBe('fetch');
  });

  it('updates an instance and revokes its credential through base-path-aware admin routes', async () => {
    Object.assign(globalThis, { window: { __APP_BASE_PATH__: '/keeper' } });
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({
        protocolVersion: 'keeper-export/v1',
        instance: {
          instanceId: 'instance-1',
          displayName: 'Production CPA',
          enabled: false,
          createdAt: '2026-08-05T00:00:00.000Z',
          updatedAt: '2026-08-05T00:01:00.000Z',
        },
      }))
      .mockResolvedValueOnce(jsonResponse({
        protocolVersion: 'keeper-export/v1',
        credentials: [{
          credentialId: 'credential-1',
          name: 'production-export',
          scopes: ['identity:test', 'usage:push', 'metadata:push'],
          active: true,
          createdAt: '2026-08-05T00:00:00.000Z',
          expiresAt: null,
          lastUsedAt: null,
          revokedAt: null,
        }],
      }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }));
    vi.stubGlobal('fetch', fetchMock);

    await expect(updateCPAInstance('instance-1', { enabled: false })).resolves.toMatchObject({ enabled: false });
    await expect(fetchCPAInstanceCredentials('instance-1')).resolves.toHaveLength(1);
    await expect(revokeCPAInstanceCredential('instance-1', 'credential-1')).resolves.toBeUndefined();
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));
    await expect(deleteCPAInstance('instance-1')).resolves.toBeUndefined();

    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      '/keeper/api/v1/instances/instance-1',
      expect.objectContaining({
        method: 'PATCH',
        body: JSON.stringify({ enabled: false }),
      }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      2,
      '/keeper/api/v1/instances/instance-1/credentials',
      expect.objectContaining({ cache: 'no-store' }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      3,
      '/keeper/api/v1/instances/instance-1/credentials/credential-1',
      expect.objectContaining({ method: 'DELETE' }),
    );
    expect(fetchMock).toHaveBeenNthCalledWith(
      4,
      '/keeper/api/v1/instances/instance-1',
      expect.objectContaining({ method: 'DELETE' }),
    );
    for (const index of [0, 2, 3]) {
      const requestHeaders = new Headers(fetchMock.mock.calls[index]?.[1]?.headers);
      expect(requestHeaders.get('X-CPA-Usage-Keeper-Request')).toBe('fetch');
    }
  });
});

import { MemoryRouter } from 'react-router-dom';
import { act, create, type ReactTestRenderer } from 'react-test-renderer';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Button } from '@/components/ui/Button';
import { usageServiceApi, type AccountActionCandidate } from '@/services/api/usageService';
import { AccountActionCandidatesPage } from './AccountActionCandidatesPage';

const notificationMocks = vi.hoisted(() => ({
  showNotification: vi.fn(),
  showConfirmation: vi.fn(),
}));

vi.mock('@/hooks/usePanelFeatureAvailability', () => ({
  usePanelFeatureAvailability: () => ({
    checking: false,
    managerServiceBase: 'http://manager.local',
    requestMonitoringAvailable: true,
  }),
}));

vi.mock('@/stores', () => ({
  useAuthStore: (selector: (state: { managementKey: string }) => unknown) =>
    selector({ managementKey: 'admin-key' }),
  useNotificationStore: () => notificationMocks,
}));

vi.mock('@/services/api/usageService', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/services/api/usageService')>();
  return {
    ...actual,
    usageServiceApi: {
      ...actual.usageServiceApi,
      listAccountActionCandidates: vi.fn(),
      ignoreAccountActionCandidate: vi.fn(),
      resolveAccountActionCandidate: vi.fn(),
      enableAccountActionCandidate: vi.fn(),
      deleteAccountActionCandidateAuthFile: vi.fn(),
    },
  };
});

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const listMock = vi.mocked(usageServiceApi.listAccountActionCandidates);

const candidate = (id: number): AccountActionCandidate => ({
  id,
  actionType: 'review',
  status: 'pending',
  provider: 'codex',
  authFileName: `candidate-${id}.json`,
  accountSnapshot: `account-${id}@example.com`,
  reason: 'manual review',
  firstSeenAtMs: 1,
  lastSeenAtMs: 2,
  hitCount: 1,
  createdAtMs: 1,
  updatedAtMs: 2,
});

describe('AccountActionCandidatesPage', () => {
  let renderer: ReactTestRenderer | null = null;

  beforeEach(() => {
    listMock.mockReset();
    listMock.mockImplementation(async (_base, _key, params) => {
      const request =
        typeof params === 'string'
          ? { status: params, page: 1, pageSize: 100 }
          : (params ?? { status: 'pending', page: 1, pageSize: 50 });
      const page = request.page ?? 1;
      return {
        items: [candidate(page)],
        total: 60,
        page,
        page_size: request.pageSize ?? 25,
        pendingCount: 60,
      };
    });
  });

  afterEach(() => {
    renderer?.unmount();
    renderer = null;
  });

  const renderPage = async () => {
    await act(async () => {
      renderer = create(
        <MemoryRouter>
          <AccountActionCandidatesPage />
        </MemoryRouter>
      );
      await Promise.resolve();
    });
    return renderer!;
  };

  it('requests server pages and debounced server search instead of filtering a 200-row snapshot', async () => {
    const page = await renderPage();
    expect(listMock).toHaveBeenLastCalledWith('http://manager.local', 'admin-key', {
      status: 'pending',
      search: '',
      page: 1,
      pageSize: 25,
    });

    const paginationButtons = page.root
      .findAllByType(Button)
      .filter((button) => button.props.size === 'xs' && button.props.variant === 'secondary');
    await act(async () => {
      paginationButtons[paginationButtons.length - 1].props.onClick();
      await Promise.resolve();
    });
    expect(listMock).toHaveBeenLastCalledWith('http://manager.local', 'admin-key', {
      status: 'pending',
      search: '',
      page: 2,
      pageSize: 25,
    });

    const searchInput = page.root.findAllByType('input')[0];
    await act(async () => {
      searchInput.props.onChange({ target: { value: 'trace-alpha' } });
      await Promise.resolve();
    });
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 400));
      await Promise.resolve();
    });
    expect(listMock).toHaveBeenLastCalledWith('http://manager.local', 'admin-key', {
      status: 'pending',
      search: 'trace-alpha',
      page: 1,
      pageSize: 25,
    });
  });
});

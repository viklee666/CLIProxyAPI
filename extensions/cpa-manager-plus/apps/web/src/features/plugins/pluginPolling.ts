import { pluginsApi, pluginStoreApi } from '@/services/api';
import type {
  PluginListEntry,
  PluginListResponse,
  PluginStoreEntry,
  PluginStoreResponse,
} from '@/types';

const PLUGIN_STATE_TIMEOUT_MS = 15_000;
const PLUGIN_STATE_INTERVAL_MS = 500;

const normalizePluginVersion = (version: string) => version.trim().replace(/^v/i, '');

export const pluginVersionMatches = (left: string, right: string) =>
  normalizePluginVersion(left) === normalizePluginVersion(right);

export const isPluginStoreInstallSettled = (
  plugin: PluginStoreEntry,
  requestedVersion = ''
): boolean => {
  if (!plugin.installed) return false;
  const version = requestedVersion.trim();
  if (version) return pluginVersionMatches(plugin.installedVersion, version);
  return !plugin.updateAvailable;
};

const wait = (ms: number) =>
  new Promise<void>((resolve) => {
    window.setTimeout(resolve, ms);
  });

export interface PluginStateWaitResult {
  response: PluginListResponse;
  plugin: PluginListEntry | null;
  timedOut: boolean;
}

export interface PluginStoreStateWaitResult {
  response: PluginStoreResponse;
  plugin: PluginStoreEntry | null;
  timedOut: boolean;
}

export async function waitForPluginState(
  id: string,
  predicate: (plugin: PluginListEntry, response: PluginListResponse) => boolean,
  timeoutMs = PLUGIN_STATE_TIMEOUT_MS,
  intervalMs = PLUGIN_STATE_INTERVAL_MS
): Promise<PluginStateWaitResult> {
  const deadline = Date.now() + timeoutMs;
  let interval = intervalMs;
  let latest = await pluginsApi.list({ id });

  for (;;) {
    const plugin = latest.plugins.find((item) => item.id === id) ?? null;
    if (plugin && predicate(plugin, latest)) {
      const response = await pluginsApi.list();
      return {
        response,
        plugin: response.plugins.find((item) => item.id === id) ?? plugin,
        timedOut: false,
      };
    }
    if (Date.now() >= deadline) {
      const response = await pluginsApi.list();
      return {
        response,
        plugin: response.plugins.find((item) => item.id === id) ?? plugin,
        timedOut: true,
      };
    }
    await wait(Math.min(interval, Math.max(0, deadline - Date.now())));
    interval = Math.min(2_000, Math.max(intervalMs, Math.round(interval * 1.5)));
    latest = await pluginsApi.list({ id });
  }
}

export async function waitForPluginStoreState(
  id: string,
  sourceId: string,
  predicate: (plugin: PluginStoreEntry, response: PluginStoreResponse) => boolean,
  timeoutMs = PLUGIN_STATE_TIMEOUT_MS,
  intervalMs = PLUGIN_STATE_INTERVAL_MS
): Promise<PluginStoreStateWaitResult> {
  const deadline = Date.now() + timeoutMs;
  let interval = intervalMs;
  let latest = await pluginStoreApi.list({ id, sourceId });

  for (;;) {
    const plugin =
      latest.plugins.find((item) => item.id === id && (!sourceId || item.sourceId === sourceId)) ??
      null;
    if (plugin && predicate(plugin, latest)) {
      const response = await pluginStoreApi.list();
      return {
        response,
        plugin:
          response.plugins.find(
            (item) => item.id === id && (!sourceId || item.sourceId === sourceId)
          ) ?? plugin,
        timedOut: false,
      };
    }
    if (Date.now() >= deadline) {
      const response = await pluginStoreApi.list();
      return {
        response,
        plugin:
          response.plugins.find(
            (item) => item.id === id && (!sourceId || item.sourceId === sourceId)
          ) ?? plugin,
        timedOut: true,
      };
    }
    await wait(Math.min(interval, Math.max(0, deadline - Date.now())));
    interval = Math.min(2_000, Math.max(intervalMs, Math.round(interval * 1.5)));
    latest = await pluginStoreApi.list({ id, sourceId });
  }
}

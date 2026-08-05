import type { ModelAlias } from '@/types';

export interface ModelEntry {
  name: string;
  alias: string;
  priority?: number;
  testModel?: string;
  image?: boolean;
  forceMapping?: boolean;
  inputModalities?: string[];
  outputModalities?: string[];
  inputModalitiesDraft?: string;
  outputModalitiesDraft?: string;
  thinking?: Record<string, unknown>;
  thinkingDraft?: string;
}

export const createDiscoveredModelEntry = (name: string): ModelEntry => ({
  name: String(name ?? '').trim(),
  alias: '',
});

export const modelsToEntries = (models?: ModelAlias[]): ModelEntry[] => {
  if (!Array.isArray(models) || models.length === 0) {
    return [{ name: '', alias: '' }];
  }
  return models.map((model) => {
    const entry: ModelEntry = {
      name: model.name || '',
      alias: model.alias || '',
    };
    if (model.priority !== undefined) entry.priority = model.priority;
    if (model.testModel !== undefined) entry.testModel = model.testModel;
    if (model.image !== undefined) entry.image = model.image;
    if (model.forceMapping !== undefined) entry.forceMapping = model.forceMapping;
    if (model.inputModalities !== undefined) {
      entry.inputModalities = model.inputModalities;
      entry.inputModalitiesDraft = model.inputModalities.join(', ');
    }
    if (model.outputModalities !== undefined) {
      entry.outputModalities = model.outputModalities;
      entry.outputModalitiesDraft = model.outputModalities.join(', ');
    }
    if (model.thinking !== undefined) {
      entry.thinking = model.thinking;
      entry.thinkingDraft = JSON.stringify(model.thinking);
    }
    return entry;
  });
};

export const entriesToModels = (entries: ModelEntry[]): ModelAlias[] => {
  return entries
    .filter((entry) => entry.name.trim())
    .map((entry) => {
      const model: ModelAlias = { name: entry.name.trim() };
      const alias = entry.alias.trim();
      if (alias && alias !== model.name) {
        model.alias = alias;
      }
      if (entry.priority !== undefined) {
        model.priority = entry.priority;
      }
      if (entry.testModel) {
        model.testModel = entry.testModel;
      }
      if (entry.image !== undefined) {
        model.image = entry.image;
      }
      if (entry.forceMapping !== undefined) {
        model.forceMapping = entry.forceMapping;
      }
      if (entry.inputModalities !== undefined) {
        model.inputModalities = [...entry.inputModalities];
      }
      if (entry.outputModalities !== undefined) {
        model.outputModalities = [...entry.outputModalities];
      }
      if (entry.thinking && typeof entry.thinking === 'object') {
        model.thinking = entry.thinking;
      }
      return model;
    });
};

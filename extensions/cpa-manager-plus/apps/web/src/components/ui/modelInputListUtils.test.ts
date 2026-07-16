import { describe, expect, it } from 'vitest';
import { createDiscoveredModelEntry, entriesToModels } from './modelInputListUtils';

describe('modelInputListUtils', () => {
  it('uses the upstream model name without creating a default alias', () => {
    expect(createDiscoveredModelEntry(' upstream-model ')).toEqual({
      name: 'upstream-model',
      alias: '',
    });
  });

  it('preserves explicit empty modality arrays', () => {
    expect(
      entriesToModels([
        {
          name: 'image-model',
          alias: '',
          inputModalities: [],
          outputModalities: [],
        },
      ])
    ).toEqual([
      {
        name: 'image-model',
        inputModalities: [],
        outputModalities: [],
      },
    ]);
  });

  it('keeps untouched modality fields undefined', () => {
    expect(entriesToModels([{ name: 'text-model', alias: '' }])).toEqual([
      { name: 'text-model' },
    ]);
  });
});

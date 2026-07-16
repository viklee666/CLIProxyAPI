export type AuthJsonInputType = 'cpa' | 'session' | 'sub2api' | 'cockpit';

export type AuthJsonDetectionType = AuthJsonInputType | 'auto';

export type AuthJsonRecord = Record<string, unknown>;

export type AuthJsonConversionResult = AuthJsonRecord | AuthJsonRecord[];

// v.2.1.982
import { ko } from './locales/ko';
import { en } from './locales/en';
import { id } from './locales/id';
import { th } from './locales/th';
import type { I18nDictionary } from './types';

// Why: locale files declare loose `Record<string, any>` for incremental migration; the cast
// pins the consumer-facing type to I18nDictionary so call sites drop their `(I18N_DATA as any)` casts.
export const I18N_DATA = { ko, en, id, th } as unknown as I18nDictionary;

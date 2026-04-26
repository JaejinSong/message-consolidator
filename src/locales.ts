// v.2.1.982
import { ko } from './locales/ko';
import { en } from './locales/en';
import { id } from './locales/id';
import { th } from './locales/th';
import type { I18nDictionary } from './types';

// Why: locale files now declare `I18nEntry` directly (single source of truth in types.ts).
// The literal map is structurally compatible with I18nDictionary, so no cast is needed.
export const I18N_DATA: I18nDictionary = { ko, en, id, th };

/**
 * @file learning-renderer.ts
 * @description Learning review surface -- an inspection panel for the correction
 * observations the AI has learned from users, plus the resulting few-shot examples.
 * Why: learning must be visible and reversible (approve/reject), not a black box.
 */
import { api } from '../api';
import { state } from '../state';
import { CorrectionObservation, LearnedExample } from '../types';
import { escapeHTML, getErrorMessage, TimeService } from '../utils';
import { showToast } from './ui-effects';

let bound = false;

/**
 * Loads and renders both the observations list and the learned examples list.
 * Safe to call repeatedly (e.g. every time the Learning tab is activated).
 */
export async function initLearningTab(): Promise<void> {
    bindOnce();
    await Promise.all([loadObservations(), loadExamples()]);
}

function bindOnce(): void {
    if (bound) return;
    bound = true;

    document.getElementById('learningStatusFilter')?.addEventListener('change', () => void loadObservations());
    document.getElementById('learningObservationsList')?.addEventListener('click', (e) => void onObservationsClick(e));
}

async function onObservationsClick(e: Event): Promise<void> {
    const target = e.target as HTMLElement;
    const btn = target.closest<HTMLButtonElement>('[data-action="approve"], [data-action="reject"]');
    // Why: btn.disabled guards against a duplicate concurrent decide call from a
    // double-click while the previous approve/reject is still in flight.
    if (!btn || btn.disabled) return;
    const id = Number(btn.dataset.id);
    if (!id) return;
    const approve = btn.dataset.action === 'approve';
    const lang = state.currentLang || 'en';

    const row = btn.closest<HTMLElement>('.c-learning-view__row');
    const rowButtons = row
        ? Array.from(row.querySelectorAll<HTMLButtonElement>('[data-action="approve"], [data-action="reject"]'))
        : [btn];
    rowButtons.forEach(b => { b.disabled = true; });

    try {
        await api.decideObservation(id, approve);
        await loadObservations();
    } catch (err: unknown) {
        showToast(getErrorMessage(err) || (lang === 'ko' ? '처리 실패' : 'Failed to update'), 'error');
    } finally {
        rowButtons.forEach(b => { b.disabled = false; });
    }
}

async function loadObservations(): Promise<void> {
    const list = document.getElementById('learningObservationsList');
    const empty = document.getElementById('learningObservationsEmpty');
    if (!list) return;
    const status = (document.getElementById('learningStatusFilter') as HTMLSelectElement | null)?.value || 'promoted';

    try {
        const rows = await api.listObservations(status);
        list.innerHTML = rows.map(renderObservationRow).join('');
        empty?.classList.toggle('hidden', rows.length > 0);
    } catch (err: unknown) {
        list.innerHTML = '';
        empty?.classList.remove('hidden');
        console.error('[Learning] Failed to load observations', err);
    }
}

async function loadExamples(): Promise<void> {
    const list = document.getElementById('learningExamplesList');
    const empty = document.getElementById('learningExamplesEmpty');
    if (!list) return;

    try {
        const rows = await api.listLearnedExamples();
        list.innerHTML = rows.map(renderExampleRow).join('');
        empty?.classList.toggle('hidden', rows.length > 0);
    } catch (err: unknown) {
        list.innerHTML = '';
        empty?.classList.remove('hidden');
        console.error('[Learning] Failed to load examples', err);
    }
}

const EXCERPT_LIMIT = 120;

// Why: code-point slicing (not text.slice, which cuts UTF-16 code units and can
// split a surrogate pair -- e.g. emoji/rare CJK -- into two invalid halves).
function excerpt(text: string): string {
    const codePoints = Array.from(text);
    if (codePoints.length <= EXCERPT_LIMIT) return text;
    return `${codePoints.slice(0, EXCERPT_LIMIT).join('')}...`;
}

function formatNullableDate(value: { Time: string; Valid: boolean } | undefined, lang: string): string {
    if (!value?.Valid) return '-';
    return TimeService.formatDisplayTime(value.Time, lang);
}

// Why: "suppress" observations carry no to_value (a deletion signal, not a replacement).
function renderObservationRow(o: CorrectionObservation): string {
    const lang = state.currentLang || 'en';
    const signature = o.to_value
        ? `${escapeHTML(o.from_value)} &rarr; ${escapeHTML(o.to_value)}`
        : `${escapeHTML(o.from_value)} <span class="c-learning-view__suppress-tag">(${lang === 'ko' ? '제외' : 'suppress'})</span>`;

    return `
        <div class="c-learning-view__row" data-id="${o.id}">
            <div class="c-learning-view__row-main">
                <span class="c-badge c-badge--dim">${escapeHTML(o.kind)}</span>
                <span class="c-learning-view__signature">${signature}</span>
            </div>
            <div class="c-learning-view__row-meta">
                <span>${escapeHTML(o.scope || '-')}</span>
                <span>${lang === 'ko' ? '근거' : 'evidence'} ${o.evidence_count}</span>
                <span>${escapeHTML(o.status)}</span>
                <span>${formatNullableDate(o.updated_at, lang)}</span>
            </div>
            <div class="c-learning-view__row-actions">
                <button type="button" class="c-btn c-btn--success c-btn--sm" data-action="approve" data-id="${o.id}">${lang === 'ko' ? '승인' : 'Approve'}</button>
                <button type="button" class="c-btn c-btn--outline c-btn--sm" data-action="reject" data-id="${o.id}">${lang === 'ko' ? '거부' : 'Reject'}</button>
            </div>
        </div>
    `;
}

function renderExampleRow(ex: LearnedExample): string {
    const lang = state.currentLang || 'en';
    return `
        <div class="c-learning-view__row">
            <div class="c-learning-view__row-main">
                <span class="c-badge c-badge--dim">${escapeHTML(ex.origin)}</span>
                <span class="c-learning-view__excerpt">${escapeHTML(excerpt(ex.input))}</span>
            </div>
            <div class="c-learning-view__row-meta">
                <span>${formatNullableDate(ex.created_at, lang)}</span>
            </div>
        </div>
    `;
}

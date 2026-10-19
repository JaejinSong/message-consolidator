// @vitest-environment happy-dom
import { describe, it, expect, beforeEach } from 'vitest';
import { renderObservationRow, renderFilterDropRow } from './learning-renderer';
import { state } from '../state';
import { CorrectionObservation, FilterDrop } from '../types';

function observation(over: Partial<CorrectionObservation> = {}): CorrectionObservation {
    return {
        id: 1,
        user_email: 'user@example.com',
        kind: 'suppress',
        from_value: 'browser collection continue',
        to_value: '',
        scope: 'whatsapp|Room',
        evidence_count: 1,
        seen_message_ids: '[]',
        status: 'pending',
        created_at: { Time: '', Valid: false },
        updated_at: { Time: '', Valid: false },
        ...over,
    };
}

function row(o: CorrectionObservation): HTMLElement {
    document.body.innerHTML = renderObservationRow(o);
    return document.body.firstElementChild as HTMLElement;
}

// Why: kind = 'precision' is structurally excluded from guardSuppressRule
// (services/precision_observer.go), so the row must not promise suppression.
describe('observation row wording by kind', () => {
    beforeEach(() => {
        state.currentLang = 'ko';
    });

    it('marks a suppress observation as excluding future extractions', () => {
        const el = row(observation());
        expect(el.querySelector('.c-learning-view__suppress-tag')?.textContent).toBe('(제외)');
        expect(el.querySelector('[data-action="approve"]')?.textContent).toBe('승인');
        expect(el.querySelector('[data-action="reject"]')?.textContent).toBe('거부');
    });

    it('marks a precision observation as a diagnostic, never as a suppression', () => {
        const el = row(observation({ kind: 'precision', from_value: 'owner=shared' }));
        expect(el.querySelector('.c-learning-view__suppress-tag')).toBeNull();
        const tag = el.querySelector('.c-learning-view__signal-tag');
        expect(tag?.textContent).toBe('(진단)');
        expect(tag?.getAttribute('title')).toContain('차단하지 않습니다');
    });

    it('labels precision actions as triage rather than enforcement', () => {
        const el = row(observation({ kind: 'precision', from_value: 'verb=check' }));
        expect(el.querySelector('[data-action="approve"]')?.textContent).toBe('확인');
        expect(el.querySelector('[data-action="reject"]')?.textContent).toBe('무시');
    });

    it('uses English wording for both kinds', () => {
        state.currentLang = 'en';
        expect(row(observation()).querySelector('.c-learning-view__suppress-tag')?.textContent).toBe('(suppress)');
        const precision = row(observation({ kind: 'precision', from_value: 'owner=named' }));
        expect(precision.querySelector('.c-learning-view__signal-tag')?.textContent).toBe('(diagnostic)');
        expect(precision.querySelector('[data-action="approve"]')?.textContent).toBe('Acknowledge');
    });

    it('tags no other empty-to_value kind as a suppression', () => {
        const el = row(observation({ kind: 'category_boundary', from_value: 'POLICY' }));
        expect(el.querySelector('.c-learning-view__suppress-tag')).toBeNull();
        expect(el.querySelector('.c-learning-view__signal-tag')).toBeNull();
    });

    it('renders an alignment observation as a from -> to replacement', () => {
        const el = row(observation({ kind: 'assignee_alias', from_value: 'bob', to_value: 'robert' }));
        expect(el.querySelector('.c-learning-view__signature')?.textContent).toContain('bob → robert');
        expect(el.querySelector('.c-learning-view__suppress-tag')).toBeNull();
    });
});

function drop(over: Partial<FilterDrop> = {}): FilterDrop {
    return {
        id: 7,
        source: 'slack',
        room: 'eng',
        p_actionable: 0.2,
        category: '',
        text: 'please <b>ship</b> it',
        created_at: '2026-10-19T00:00:00Z',
        outcome: '',
        ...over,
    };
}

describe('filtered message row', () => {
    beforeEach(() => {
        state.currentLang = 'ko';
    });

    it('offers rescue and confirm actions for an undecided drop', () => {
        document.body.innerHTML = renderFilterDropRow(drop());
        expect(document.querySelector('[data-action="rescue-drop"]')?.textContent).toBe('태스크였음');
        expect(document.querySelector('[data-action="confirm-drop"]')?.textContent).toBe('노이즈 맞음');
        expect(document.querySelector('[data-action="rescue-drop"]')?.getAttribute('data-id')).toBe('7');
    });

    it('escapes the dropped text', () => {
        document.body.innerHTML = renderFilterDropRow(drop());
        expect(document.querySelector('.c-learning-view__drop-text b')).toBeNull();
        expect(document.querySelector('.c-learning-view__drop-text')?.textContent).toBe('please <b>ship</b> it');
    });

    it('shows a decided drop read-only', () => {
        document.body.innerHTML = renderFilterDropRow(drop({ outcome: 'rescued' }));
        expect(document.querySelector('button')).toBeNull();
        expect(document.querySelector('.c-badge--dim:last-child, .c-learning-view__row-actions .c-badge')?.textContent).toBe('복구됨');
    });
});

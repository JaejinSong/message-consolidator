import { describe, it, expect } from 'vitest';
import { classifyBucket, groupCommitments, sortCommitments, formatDeadlineDisplay } from './commitments';
import type { CommitmentItem } from '../types';

function makeItem(overrides: Partial<CommitmentItem> = {}): CommitmentItem {
    return {
        id: 1, task: 'Test', requester: 'alice', assignee: 'bob',
        category: 'PROMISE', room: 'general', source: 'slack', days_open: 0,
        ...overrides,
    };
}

describe('classifyBucket', () => {
    it('undated when no deadline_date', () => {
        expect(classifyBucket(makeItem())).toBe('undated');
    });

    it('overdue when deadline_date < today', () => {
        expect(classifyBucket(makeItem({ deadline_date: '2020-01-01' }))).toBe('overdue');
    });

    it('active when deadline_date >= today', () => {
        const future = new Date();
        future.setDate(future.getDate() + 7);
        const iso = future.toISOString().slice(0, 10);
        expect(classifyBucket(makeItem({ deadline_date: iso }))).toBe('active');
    });
});

describe('groupCommitments', () => {
    it('partitions into correct buckets', () => {
        const items = [
            makeItem({ deadline_date: '2020-01-01' }),
            makeItem({ deadline_date: undefined }),
            makeItem({ deadline_date: '2099-12-31' }),
        ];
        const result = groupCommitments(items);
        expect(result.overdue).toHaveLength(1);
        expect(result.undated).toHaveLength(1);
        expect(result.active).toHaveLength(1);
    });

    it('returns empty buckets for empty input', () => {
        const result = groupCommitments([]);
        expect(result.overdue).toHaveLength(0);
        expect(result.undated).toHaveLength(0);
        expect(result.active).toHaveLength(0);
    });
});

describe('sortCommitments', () => {
    it('sorts overdue by deadline_date asc', () => {
        const input = {
            overdue: [makeItem({ deadline_date: '2021-06-01' }), makeItem({ deadline_date: '2020-01-01' })],
            undated: [],
            active: [],
            stalled: { mine: [], observed: [] },
        };
        const sorted = sortCommitments(input);
        expect(sorted.overdue[0].deadline_date).toBe('2020-01-01');
    });

    it('sorts undated by days_open desc', () => {
        const input = {
            overdue: [],
            undated: [makeItem({ days_open: 2 }), makeItem({ days_open: 10 })],
            active: [],
            stalled: { mine: [], observed: [] },
        };
        const sorted = sortCommitments(input);
        expect(sorted.undated[0].days_open).toBe(10);
    });
});

describe('formatDeadlineDisplay', () => {
    it('returns empty string when no deadline_date', () => {
        expect(formatDeadlineDisplay(makeItem())).toBe('');
    });

    it('returns ISO date without prefix when not inferred', () => {
        expect(formatDeadlineDisplay(makeItem({ deadline_date: '2026-06-10', deadline_inferred: false }))).toBe('2026-06-10');
    });

    it('returns ~ prefix when inferred', () => {
        expect(formatDeadlineDisplay(makeItem({ deadline_date: '2026-06-10', deadline_inferred: true }))).toBe('~2026-06-10');
    });
});

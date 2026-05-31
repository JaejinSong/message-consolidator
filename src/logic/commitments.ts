import { CommitmentItem, CommitmentsResponse } from '../types';

export type CommitmentBucket = 'overdue' | 'undated' | 'active';

/** Classify a single CommitmentItem into a bucket based on deadline_date vs today. */
export function classifyBucket(item: CommitmentItem): CommitmentBucket {
    if (!item.deadline_date) return 'undated';
    const today = new Date().toISOString().slice(0, 10);
    return item.deadline_date < today ? 'overdue' : 'active';
}

/** Group a flat list of CommitmentItems into overdue/undated/active buckets. */
export function groupCommitments(items: CommitmentItem[]): CommitmentsResponse {
    const result: CommitmentsResponse = { overdue: [], undated: [], active: [], stalled: { mine: [], observed: [] } };
    for (const item of items) {
        result[classifyBucket(item)].push(item);
    }
    return result;
}

/** Sort items within each bucket: overdue by deadline_date asc, others by days_open desc. */
export function sortCommitments(resp: CommitmentsResponse): CommitmentsResponse {
    const byDeadlineAsc = (a: CommitmentItem, b: CommitmentItem) =>
        (a.deadline_date ?? '').localeCompare(b.deadline_date ?? '');
    const byDaysDesc = (a: CommitmentItem, b: CommitmentItem) => b.days_open - a.days_open;
    return {
        overdue: [...resp.overdue].sort(byDeadlineAsc),
        undated: [...resp.undated].sort(byDaysDesc),
        active: [...resp.active].sort(byDeadlineAsc),
        stalled: resp.stalled ?? { mine: [], observed: [] },
    };
}

/** Format deadline_date for display. Adds '~' prefix when deadline_inferred is true. */
export function formatDeadlineDisplay(item: CommitmentItem): string {
    if (!item.deadline_date) return '';
    return item.deadline_inferred ? `~${item.deadline_date}` : item.deadline_date;
}

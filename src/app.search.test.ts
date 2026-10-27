// @vitest-environment happy-dom
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { api } from './api';
import { getTaskById, state } from './state';
import { POLLING_INTERVALS } from './constants';
import type { CategorizedMessages, Message } from './types';

vi.mock('./api', () => ({ api: {
    fetchMessages: vi.fn(), searchActiveMessages: vi.fn(), fetchUserProfile: vi.fn(),
    fetchSlackStatus: vi.fn(), fetchWhatsAppStatus: vi.fn(), fetchTelegramStatus: vi.fn(), fetchLINEStatus: vi.fn(),
    toggleDone: vi.fn(), toggleSubtask: vi.fn(), deleteTask: vi.fn(), requestTranslation: vi.fn(),
} }));
vi.mock('./archive', () => ({ archive: { init: vi.fn(), isVisible: () => false } }));
vi.mock('./modals', () => ({ modals: { init: vi.fn() } }));
vi.mock('./insights', () => ({ insights: { init: vi.fn() } }));
vi.mock('./guide', () => ({ guide: { init: vi.fn() } }));
vi.mock('./services/authService', () => ({ authService: { checkGmailStatus: vi.fn().mockResolvedValue({ connected: false }) } }));
vi.mock('./renderers/connections-renderer', () => ({ setupConnectionsTab: vi.fn(), renderConnections: vi.fn(), rerenderConnections: vi.fn(), handleSlackAuthRedirect: vi.fn() }));
vi.mock('./renderers/telegram-modal-renderer', () => ({ bindTelegramModal: vi.fn() }));
vi.mock('./renderers/task-add-renderer', () => ({ initTaskAddPanel: vi.fn() }));
vi.mock('./renderers/i18n-renderer', () => ({ renderUILanguage: vi.fn() }));

function task(id: number, title: string, extra: Partial<Message> = {}): Message {
    return { id, task: title, requester: 'Sender', source: 'slack', done: false, category: 'personal', ...extra };
}
function messages(...inbox: Message[]): CategorizedMessages {
    return { inbox, delegated: [], reference: [] };
}
function deferred<T>() {
    let resolve!: (value: T) => void;
    let reject!: (error: Error) => void;
    const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; });
    return { promise, resolve, reject };
}
function input(query: string) {
    const el = document.getElementById('taskSearch') as HTMLInputElement;
    el.value = query;
    el.dispatchEvent(new Event('input', { bubbles: true }));
}
function changeLanguage(lang: string) {
    const el = document.getElementById('languageSelect') as HTMLSelectElement;
    el.value = lang;
    el.dispatchEvent(new Event('change', { bubbles: true }));
}
function visibleIds() {
    return [...document.querySelectorAll('#receivedTasksList .c-message-card')].map(el => Number(el.getAttribute('data-id')));
}
async function flush() {
    await vi.advanceTimersByTimeAsync(0);
}
async function search(query: string) {
    input(query);
    await vi.advanceTimersByTimeAsync(300);
}

describe('task search through application events', () => {
    beforeAll(async () => { await import('./app'); });
    beforeEach(async () => {
        vi.useFakeTimers();
        vi.clearAllMocks();
        for (const method of Object.values(api)) {
            if (vi.isMockFunction(method)) method.mockReset();
        }
        document.cookie = 'session_active=true; path=/';
        document.body.innerHTML = `
            <select id="languageSelect"><option value="en">en</option><option value="ko">ko</option></select>
            <input id="taskSearch">
            <div id="deadlineFilterBar"><button data-dl-filter="has_deadline">With deadline</button></div>
            <div id="dashboardContent"><button class="tab-btn" data-tab="receivedTasksTab">Tasks</button></div>
            <div id="receivedTasksList"></div><div id="delegatedTasksList"></div>
            <div id="referenceTasksList"></div><div id="allTasksList"></div>`;
        state.currentLang = 'en';
        state.taskSearchQuery = '';
        state.isTaskSearchResult = false;
        state.messages = messages();
        state.deadlineFilter = 'all';
        state.isFetchingMessages = false;
        state.isFetchingStatus = false;
        vi.mocked(api.fetchMessages).mockResolvedValue(messages(task(1, 'Original task')));
        vi.mocked(api.searchActiveMessages).mockResolvedValue(messages());
        vi.mocked(api.fetchUserProfile).mockResolvedValue({ email: '', picture: '', name: '', points: 0, streak: 0, streak_freezes: 0 });
        vi.mocked(api.fetchSlackStatus).mockResolvedValue({ status: 'disconnected', user_token: false, user_token_slack_id: '' });
        vi.mocked(api.fetchWhatsAppStatus).mockResolvedValue({ status: 'disconnected' });
        vi.mocked(api.fetchTelegramStatus).mockResolvedValue({ status: 'disconnected' });
        vi.mocked(api.fetchLINEStatus).mockResolvedValue({ status: 'disconnected' });
        vi.mocked(api.toggleDone).mockResolvedValue({});
        vi.mocked(api.deleteTask).mockResolvedValue({});
        document.dispatchEvent(new Event('DOMContentLoaded'));
        await vi.advanceTimersByTimeAsync(500);
        vi.mocked(api.fetchMessages).mockClear();
    });
    afterEach(() => { vi.clearAllTimers(); vi.useRealTimers(); });

    it('invalidates a pending response as soon as another query is typed, before debounce', async () => {
        const old = deferred<CategorizedMessages>();
        vi.mocked(api.searchActiveMessages).mockReturnValueOnce(old.promise);
        await search('alpha');
        input('beta');
        old.resolve(messages(task(10, 'alpha stale')));
        await flush();
        expect(visibleIds()).not.toContain(10);
    });

    it.each(['', 'xy', '😀😀'])('invalidates pending FTS on clear/short query %j and restores the normal list', async query => {
        const old = deferred<CategorizedMessages>();
        vi.mocked(api.searchActiveMessages).mockReturnValueOnce(old.promise);
        await search('alpha');
        input(query);
        old.resolve(messages(task(10, 'alpha stale')));
        await flush();
        expect(visibleIds()).not.toContain(10);
        await vi.advanceTimersByTimeAsync(300);
        expect(api.fetchMessages).toHaveBeenCalledWith('en');
        if (!query) expect(visibleIds()).toEqual([1]);
    });

    it('keeps the newest result when requests finish in reverse order', async () => {
        const old = deferred<CategorizedMessages>();
        const latest = deferred<CategorizedMessages>();
        vi.mocked(api.searchActiveMessages).mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise);
        await search('alpha');
        await search('beta');
        latest.resolve(messages(task(20, 'beta newest')));
        await flush();
        old.resolve(messages(task(10, 'alpha stale')));
        await flush();
        expect(visibleIds()).toEqual([20]);
        expect(getTaskById(20)?.task).toBe('beta newest');
    });

    it('replaces a pending request on language change and ignores the old language response', async () => {
        const old = deferred<CategorizedMessages>();
        vi.mocked(api.searchActiveMessages).mockReturnValueOnce(old.promise).mockResolvedValue(messages(task(20, '한국어 결과', { task_ko: '한국어 결과' })));
        await search('alpha');
        changeLanguage('ko');
        await flush();
        expect(api.searchActiveMessages).toHaveBeenLastCalledWith('alpha', 'ko');
        old.resolve(messages(task(10, 'alpha old English')));
        await flush();
        expect(visibleIds()).toEqual([20]);
    });

    it('preserves a body-only FTS match through deadline rerender and polling', async () => {
        vi.mocked(api.searchActiveMessages).mockResolvedValue(messages(task(20, 'Different title', { deadline: '2099-01-01' })));
        await search('body match');
        expect(visibleIds()).toEqual([20]);
        document.querySelector<HTMLButtonElement>('[data-dl-filter]')!.click();
        expect(visibleIds()).toEqual([20]);
        await vi.advanceTimersByTimeAsync(POLLING_INTERVALS.MESSAGES);
        expect(vi.mocked(api.searchActiveMessages).mock.calls.length).toBeGreaterThan(1);
        expect(api.searchActiveMessages).toHaveBeenLastCalledWith('body match', 'en');
        expect(api.fetchMessages).not.toHaveBeenCalled();
        expect(visibleIds()).toEqual([20]);
    });

    it('allows actions on a task found only in server search results', async () => {
        vi.mocked(api.searchActiveMessages).mockResolvedValue(messages(task(20, 'Found task')));
        await search('Found');
        expect(getTaskById(20)?.task).toBe('Found task');
        document.querySelector<HTMLButtonElement>('#receivedTasksList [data-action="toggle-done"]')!.click();
        expect(getTaskById(20)?.done).toBe(true);
        await flush();
        expect(api.toggleDone).toHaveBeenCalledWith('20', true);
        document.querySelector<HTMLButtonElement>('#receivedTasksList [data-action="delete"]')!.click();
        await flush();
        expect(api.deleteTask).toHaveBeenCalledWith('20');
        expect(getTaskById(20)).toBeUndefined();
    });

    it('fetches the normal list again after clearing a completed search', async () => {
        vi.mocked(api.searchActiveMessages).mockResolvedValue(messages(task(20, 'Found task')));
        await search('Found');
        await search('');
        expect(api.fetchMessages).toHaveBeenCalledWith('en');
        expect(visibleIds()).toEqual([1]);
    });

    it('shows an error for the current search without redisplaying a previous query result', async () => {
        vi.mocked(api.searchActiveMessages).mockResolvedValueOnce(messages(task(20, 'alpha result'))).mockRejectedValueOnce(new Error('Search unavailable'));
        await search('alpha');
        await search('beta');
        expect(visibleIds()).toEqual([]);
        expect(document.body.textContent).toContain('Search unavailable');
    });

    it('ignores an old request error after a newer query succeeds', async () => {
        const old = deferred<CategorizedMessages>();
        vi.mocked(api.searchActiveMessages).mockReturnValueOnce(old.promise).mockResolvedValueOnce(messages(task(20, 'beta newest')));
        await search('alpha');
        await search('beta');
        old.reject(new Error('Old request failed'));
        await flush();
        expect(visibleIds()).toEqual([20]);
        expect(document.body.textContent).not.toContain('Old request failed');
    });

    it.each(['toggle-done', 'toggle-subtask'])('finishes translation after the same task status changes through %s', async action => {
        const translation = deferred<string>();
        const status = deferred<{ user?: import('./types').UserProfile }>();
        const panel = document.createElement('section');
        panel.className = 'c-tabs__panel active';
        document.getElementById('receivedTasksList')!.before(panel);
        panel.append(document.getElementById('receivedTasksList')!);
        vi.mocked(api.requestTranslation).mockReturnValueOnce(translation.promise);
        vi.mocked(api.toggleDone).mockReturnValueOnce(status.promise);
        vi.mocked(api.toggleSubtask).mockResolvedValueOnce({});
        vi.mocked(api.fetchMessages).mockResolvedValue(messages(task(20, 'Original English', { subtasks: [{ task: 'Subtask', done: false }] })));
        changeLanguage('ko');
        await flush();
        document.querySelector<HTMLButtonElement>(`#receivedTasksList [data-action="${action}"]`)!.click();
        translation.resolve('Translated task');
        await flush();
        expect(getTaskById(20)?.task).toBe('Translated task');
        expect(getTaskById(20)?.is_translating).toBe(false);
        expect(document.getElementById('receivedTasksList')?.textContent).toContain('Translated task');
        status.resolve({});
        await flush();
    });

    it('renders a pending translation after polling accepts an identical untranslated payload', async () => {
        const translation = deferred<string>();
        const panel = document.createElement('section');
        panel.className = 'c-tabs__panel active';
        document.getElementById('receivedTasksList')!.before(panel);
        panel.append(document.getElementById('receivedTasksList')!);
        vi.mocked(api.requestTranslation).mockReturnValue(translation.promise);
        vi.mocked(api.fetchMessages).mockImplementation(async () => messages(task(20, 'Original English')));
        changeLanguage('ko');
        await flush();
        expect(api.requestTranslation).toHaveBeenCalledTimes(1);
        await vi.advanceTimersByTimeAsync(POLLING_INTERVALS.MESSAGES - 500);
        translation.resolve('Translated task');
        await flush();
        expect(getTaskById(20)?.task).toBe('Translated task');
        expect(getTaskById(20)?.is_translating).toBe(false);
        expect(document.getElementById('receivedTasksList')?.textContent).toContain('Translated task');
    });

    it.each(['pending', 'failed'])('finishes current-list translation while a refresh is %s', async refresh => {
        const translation = deferred<string>();
        const poll = deferred<CategorizedMessages>();
        const panel = document.createElement('section');
        panel.className = 'c-tabs__panel active';
        document.getElementById('receivedTasksList')!.before(panel);
        panel.append(document.getElementById('receivedTasksList')!);
        vi.mocked(api.requestTranslation).mockReturnValueOnce(translation.promise);
        vi.mocked(api.fetchMessages).mockResolvedValueOnce(messages(task(20, 'Original English')));
        changeLanguage('ko');
        await flush();
        vi.mocked(api.fetchMessages).mockReturnValueOnce(poll.promise);
        await vi.advanceTimersByTimeAsync(POLLING_INTERVALS.MESSAGES - 500);
        if (refresh === 'failed') {
            poll.reject(new Error('Refresh unavailable'));
            await flush();
        }
        translation.resolve('Translated task');
        await flush();
        expect(getTaskById(20)?.task).toBe('Translated task');
        expect(getTaskById(20)?.is_translating).toBe(false);
        if (refresh === 'pending') {
            poll.resolve(messages(task(20, 'Translated task', { task_ko: 'Translated task' })));
            await flush();
        }
    });

    it.each(['language', 'list'])('ignores translation callbacks after the current %s changes', async transition => {
        const translation = deferred<string>();
        const panel = document.createElement('section');
        panel.className = 'c-tabs__panel active';
        document.getElementById('receivedTasksList')!.before(panel);
        panel.append(document.getElementById('receivedTasksList')!);
        vi.mocked(api.requestTranslation).mockReturnValueOnce(translation.promise);
        vi.mocked(api.fetchMessages).mockResolvedValue(messages(task(20, 'Original English')));
        changeLanguage('ko');
        await flush();
        expect(api.requestTranslation).toHaveBeenCalledWith(20, 'ko');
        if (transition === 'language') {
            changeLanguage('en');
        } else {
            vi.mocked(api.searchActiveMessages).mockResolvedValue(messages(task(20, 'Current result', { task_ko: 'Current result' })));
            await search('Current');
        }
        await flush();
        translation.resolve('Old translated text');
        await flush();
        expect(document.getElementById('receivedTasksList')?.textContent).not.toContain('Old translated text');
        expect(getTaskById(20)?.task).toBe(transition === 'language' ? 'Original English' : 'Current result');
    });

    it.each(['old-first', 'new-first', 'old-error'])('keeps the current title translation after a server refresh: %s', async order => {
        const old = deferred<string>();
        const latest = deferred<string>();
        const panel = document.createElement('section');
        panel.className = 'c-tabs__panel active';
        document.getElementById('receivedTasksList')!.before(panel);
        panel.append(document.getElementById('receivedTasksList')!);
        vi.mocked(api.requestTranslation).mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise);
        vi.mocked(api.fetchMessages).mockResolvedValueOnce(messages(task(20, 'Old title')));
        changeLanguage('ko');
        await flush();
        vi.mocked(api.fetchMessages).mockResolvedValue(messages(task(20, 'New title')));
        await vi.advanceTimersByTimeAsync(POLLING_INTERVALS.MESSAGES - 500);
        expect(api.requestTranslation).toHaveBeenLastCalledWith(20, 'ko', 'New title');
        if (order === 'new-first') {
            latest.resolve('New translation');
            await flush();
        }
        if (order === 'old-error') old.reject(new Error('Old request failed'));
        else old.resolve('Old translation');
        await flush();
        expect(getTaskById(20)?.task).toBe(order === 'new-first' ? 'New translation' : 'New title');
        expect(getTaskById(20)?.translation_error).toBeUndefined();
        if (order !== 'new-first') {
            expect(getTaskById(20)?.is_translating).toBe(true);
            latest.resolve('New translation');
            await flush();
        }
        expect(getTaskById(20)?.task).toBe('New translation');
        expect(getTaskById(20)?.is_translating).toBe(false);
        expect(document.getElementById('receivedTasksList')?.textContent).toContain('New translation');
    });

    it('keeps a pending search locked when an independent profile refresh fails', async () => {
        const profile = deferred<import('./types').UserProfile>();
        const latest = deferred<CategorizedMessages>();
        vi.mocked(api.searchActiveMessages).mockResolvedValueOnce(messages(task(20, 'Found task')));
        await search('Found');
        vi.mocked(api.fetchUserProfile).mockReturnValueOnce(profile.promise);
        document.querySelector<HTMLButtonElement>('#receivedTasksList [data-action="toggle-done"]')!.click();
        await flush();
        vi.mocked(api.searchActiveMessages).mockReturnValueOnce(latest.promise);
        await search('Next');
        profile.reject(new Error('Profile unavailable'));
        await flush();
        expect(state.isFetchingMessages).toBe(true);
        latest.resolve(messages(task(30, 'Next task')));
        await flush();
        expect(visibleIds()).toEqual([30]);
        expect(state.isFetchingMessages).toBe(false);
    });

    it.each([false, true])('restores the previous subtask value %s and progress after a failed toggle', async previous => {
        const request = deferred<{}>();
        vi.mocked(api.searchActiveMessages).mockResolvedValueOnce(messages(task(20, 'Found task', {
            subtasks: [{ task: 'First step', done: previous }, { task: 'Second step', done: false }],
        })));
        await search('Found');
        vi.mocked(api.toggleSubtask).mockReturnValueOnce(request.promise);
        const selector = '#receivedTasksList [data-action="toggle-subtask"][data-index="0"]';
        const initialProgress = document.querySelector('#receivedTasksList .c-message-card__subtasks-progress')?.textContent ?? '';
        document.querySelector<HTMLElement>(selector)!.click();
        expect(getTaskById(20)?.subtasks?.[0].done).toBe(!previous);
        request.reject(new Error('Subtask unavailable'));
        await flush();
        expect(getTaskById(20)?.subtasks?.[0].done).toBe(previous);
        expect(document.querySelector(selector)?.classList.contains('c-message-card__subtask-item--done')).toBe(previous);
        expect(document.querySelector(selector)?.querySelector('.c-message-card__subtask-check')?.textContent).toBe(previous ? '✅' : '•');
        expect(document.querySelector('#receivedTasksList .c-message-card__subtasks-progress')?.textContent ?? '').toBe(initialProgress);
        expect(document.body.textContent).toContain('Failed to update subtask');
    });

    it('ignores duplicate subtask clicks across grids while its save is pending', async () => {
        const request = deferred<{}>();
        vi.mocked(api.searchActiveMessages).mockResolvedValueOnce(messages(task(20, 'Found task', {
            subtasks: [{ task: 'First step', done: false }],
        })));
        await search('Found');
        vi.mocked(api.toggleSubtask).mockReturnValueOnce(request.promise).mockResolvedValue({});
        document.querySelector<HTMLElement>('#receivedTasksList [data-action="toggle-subtask"]')!.click();
        document.querySelector<HTMLElement>('#allTasksList [data-action="toggle-subtask"]')!.click();
        expect(api.toggleSubtask).toHaveBeenCalledTimes(1);
        expect(getTaskById(20)?.subtasks?.[0].done).toBe(true);
        request.resolve({});
        await flush();
        document.querySelector<HTMLElement>('#receivedTasksList [data-action="toggle-subtask"]')!.click();
        await flush();
        expect(api.toggleSubtask).toHaveBeenCalledTimes(2);
        expect(getTaskById(20)?.subtasks?.[0].done).toBe(false);
    });

    it('preserves another subtask success when an earlier change fails', async () => {
        const failed = deferred<{}>();
        vi.mocked(api.searchActiveMessages).mockResolvedValueOnce(messages(task(20, 'Found task', {
            subtasks: [{ task: 'First step', done: false }, { task: 'Second step', done: false }],
        })));
        await search('Found');
        vi.mocked(api.toggleSubtask).mockReturnValueOnce(failed.promise).mockResolvedValueOnce({});
        document.querySelector<HTMLElement>('#receivedTasksList [data-action="toggle-subtask"][data-index="0"]')!.click();
        document.querySelector<HTMLElement>('#receivedTasksList [data-action="toggle-subtask"][data-index="1"]')!.click();
        await flush();
        failed.reject(new Error('First subtask unavailable'));
        await flush();
        expect(getTaskById(20)?.subtasks?.map(subtask => subtask.done)).toEqual([false, true]);
        expect(document.querySelector('#receivedTasksList .c-message-card__subtasks-progress')?.textContent).toBe('(1/2)');
        expect(document.querySelector('#receivedTasksList [data-index="1"]')?.classList.contains('c-message-card__subtask-item--done')).toBe(true);
    });

    it('lets a user query replace an in-flight poll and keeps its lock until the latest request finishes', async () => {
        const poll = deferred<CategorizedMessages>();
        const latest = deferred<CategorizedMessages>();
        vi.mocked(api.fetchMessages).mockReturnValueOnce(poll.promise);
        await vi.advanceTimersByTimeAsync(POLLING_INTERVALS.MESSAGES - 500);
        vi.mocked(api.searchActiveMessages).mockReturnValueOnce(latest.promise);
        await search('Found');
        expect(api.searchActiveMessages).toHaveBeenCalledWith('Found', 'en');
        poll.resolve(messages(task(10, 'poll stale')));
        await flush();
        expect(state.isFetchingMessages).toBe(true);
        expect(visibleIds()).not.toContain(10);
        latest.resolve(messages(task(20, 'Found task')));
        await flush();
        expect(visibleIds()).toEqual([20]);
        expect(state.isFetchingMessages).toBe(false);
    });
});

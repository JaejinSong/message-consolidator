/**
 * @file verify_logic.js
 * @description Logic validation script for Node.js environment.
 */

import {
    sortAndFilterMessages,
    classifyMessages,
    calculateHeatmapLevel,
    calculateSourceDistribution,
    processTimeSeriesData
} from './logic.js';

const mockMessages = [
    { id: 1, requester: 'Alice', task: 'Hello', source: 'slack', timestamp: '2023-01-01T12:00:00Z', done: false, assignee: 'me' },
    { id: 2, requester: 'Bob', task: 'World', source: 'whatsapp', timestamp: '2023-01-01T10:00:00Z', done: false, assignee: 'other' },
    { id: 3, requester: 'Charlie', task: 'Wait', source: 'slack', timestamp: '2023-01-01T11:00:00Z', done: false, waiting_on: 'Dave' },
];

function testSortAndFilter() {
    console.log('--- Testing sortAndFilterMessages ---');

    const now = new Date();
    const recentDate = new Date(now.getTime() - 2 * 24 * 60 * 60 * 1000).toISOString(); // 2일 전
    const oldDate = new Date(now.getTime() - 10 * 24 * 60 * 60 * 1000).toISOString(); // 10일 전

    const dynamicMock = [
        ...mockMessages,
        { id: 4, requester: 'User D', task: 'Recent Done task', source: 'slack', timestamp: recentDate, done: true, assignee: 'me' },
        { id: 5, requester: 'User E', task: 'Old Done task', source: 'slack', timestamp: oldDate, done: true, assignee: 'me' },
        { id: 6, requester: 'NoDate', task: 'No date task', source: 'slack', done: false, assignee: 'me' }, // 날짜 정보 누락 케이스
        { id: 7, requester: 'Waiting Me', task: 'Waiting for me', source: 'slack', done: false, assignee: 'me', waiting_on: 'Dave' } // 'me' 이면서 waiting_on 인 경우
    ];

    // My Tasks Tab (Must NOT include waiting_on even if assignee is 'me')
    const myTasks = sortAndFilterMessages(dynamicMock, 'myTasksTab', '');
    console.assert(myTasks.some(t => t.id === 1), 'Should include ID 1');
    console.assert(!myTasks.some(t => t.id === 7), 'Should EXCLUDE waiting tasks even if assigned to me');
    console.assert(myTasks.length === 3, 'Should have 3 my tasks (ID 1, ID 4, ID 6)');

    // Other Tasks Tab (Must EXCLUDE 'me' and EXCLUDE waiting_on)
    const otherTasks = sortAndFilterMessages(dynamicMock, 'otherTasksTab', '');
    console.assert(otherTasks.length === 1 && otherTasks[0].id === 2, 'Should only have ID 2');

    // Waiting Tasks Tab
    const waitingTasks = sortAndFilterMessages(dynamicMock, 'waitingTasksTab', '');
    console.assert(waitingTasks.length === 2, 'Should have 2 waiting tasks (ID 3, ID 7)');

    // All Tasks Tab (Should ignore OLD completed tasks only)
    const allTasks = sortAndFilterMessages(dynamicMock, 'allTasksTab', '');
    console.assert(allTasks.length === 6, 'Should have 6 tasks total (ID 5 old done excluded)');

    console.log('✅ sortAndFilterMessages passed');
}

function testClassify() {
    console.log('--- Testing classifyMessages ---');

    const complexMock = [
        { id: 1, done: false, assignee: 'me' },
        { id: 2, done: false, assignee: 'other' },
        { id: 3, done: false, assignee: 'me', waiting_on: 'Someone' },
        { id: 4, done: false, assignee: 'other', waiting_on: 'Someone' },
        { id: 5, done: true, assignee: 'me' } // Done tasks must be ignored
    ];

    const counts = classifyMessages(complexMock);

    // Logic: 
    // - all: all !done (1, 2, 3, 4) = 4
    // - my: !done && assignee === 'me' && !waiting_on (1) = 1
    // - others: !done && assignee !== 'me' && !waiting_on (2) = 1
    // - waiting: !done && waiting_on (3, 4) = 2

    console.assert(counts.all === 4, `Expected all=4, got ${counts.all}`);
    console.assert(counts.my === 1, `Expected my=1, got ${counts.my}`);
    console.assert(counts.others === 1, `Expected others=1, got ${counts.others}`);
    console.assert(counts.waiting === 2, `Expected waiting=2, got ${counts.waiting}`);

    // Extra Case: Verify ONLY completed tasks results in 0 counts
    const onlyDoneMock = [
        { id: 10, done: true, assignee: 'me' },
        { id: 11, done: true, waiting_on: 'boss' }
    ];
    const emptyCounts = classifyMessages(onlyDoneMock);
    console.assert(emptyCounts.all === 0, 'Completed tasks must not count towards total');
    console.assert(emptyCounts.my === 0, 'Completed tasks must not count as my tasks');
    console.assert(emptyCounts.waiting === 0, 'Completed tasks must not count as waiting tasks');

    console.log('✅ classifyMessages passed');
}

function testHeatmapLevel() {
    console.log('--- Testing calculateHeatmapLevel ---');
    console.assert(calculateHeatmapLevel(0) === 0, '0 tasks -> level 0');
    console.assert(calculateHeatmapLevel(2) === 1, '2 tasks -> level 1');
    console.assert(calculateHeatmapLevel(4) === 2, '4 tasks -> level 2');
    console.assert(calculateHeatmapLevel(6) === 3, '6 tasks -> level 3');
    console.assert(calculateHeatmapLevel(10) === 4, '10 tasks -> level 4');

    // 음수 값 예외 처리
    console.assert(calculateHeatmapLevel(-5) === 0, 'Negative tasks -> level 0');
    console.log('✅ calculateHeatmapLevel passed');
}

function testSourceDistribution() {
    console.log('--- Testing calculateSourceDistribution ---');
    const dist = calculateSourceDistribution({ slack: 10, whatsapp: 10, gmail: 20 });
    console.assert(dist.slack === 25, 'Slack should be 25%');
    console.assert(dist.whatsapp === 25, 'WhatsApp should be 25%');
    console.assert(dist.gmail === 50, 'Gmail should be 50%');
    console.assert(dist.slack + dist.whatsapp + dist.gmail === 100, 'Sum should be 100');

    // 데이터가 0일 때 (Zero total) 예외 처리
    const distZero = calculateSourceDistribution({ slack: 0, whatsapp: 0, gmail: 0 });
    console.assert(Object.keys(distZero).length === 0, 'Should handle 0 total tasks');

    // 동적 소스(Dynamic source) 확장 지원 확인
    const distUnknown = calculateSourceDistribution({ slack: 10, telegram: 90 });
    console.assert(distUnknown.slack === 10 && distUnknown.telegram === 90, 'Should handle new dynamic sources correctly');
    console.log('✅ calculateSourceDistribution passed');
}

function testProcessTimeSeriesData() {
    console.log('--- Testing processTimeSeriesData ---');
    const today = new Date();
    today.setHours(0, 0, 0, 0);
    const yesterday = new Date(today);
    yesterday.setDate(yesterday.getDate() - 1);
    const yStr = yesterday.toISOString().split('T')[0];

    const rawHistory = [
        { date: yStr, counts: { slack: 5, telegram: 2 } }
    ];

    const processed = processTimeSeriesData(rawHistory, 3);
    console.assert(processed.length === 3, 'Should generate exactly 3 continuous days');
    console.assert(processed[1].cumulative === 7, 'Cumulative should calculate correctly');
    console.log('✅ processTimeSeriesData passed');
}

function runAllTests() {
    try {
        testSortAndFilter();
        testClassify();
        testHeatmapLevel();
        testSourceDistribution();
        testProcessTimeSeriesData();
        console.log('\n✨ ALL TESTS PASSED SUCCESSFULLY! ✨');
    } catch (e) {
        console.error('\n❌ TEST FAILED:');
        console.error(e);
        process.exit(1);
    }
}

runAllTests();

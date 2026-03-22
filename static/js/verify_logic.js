/**
 * @file verify_logic.js
 * @description Logic validation script for Node.js environment.
 */

import {
    sortAndFilterMessages,
    classifyMessages,
    calculateHeatmapLevel,
    calculateSourceDistribution
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

    const dynamicMock = [...mockMessages,
    { id: 4, requester: 'User D', task: 'Recent Done task', source: 'slack', timestamp: recentDate, done: true, assignee: 'me' },
    { id: 5, requester: 'User E', task: 'Old Done task', source: 'slack', timestamp: oldDate, done: true, assignee: 'me' },
    { id: 6, requester: 'NoDate', task: 'No date task', source: 'slack', done: false, assignee: 'me' } // 날짜 정보 누락 케이스
    ];

    // My Tasks
    const myTasks = sortAndFilterMessages(dynamicMock, 'myTasksTab', '');
    console.assert(myTasks.length === 3, 'Should have 3 my tasks (ID 1, ID 4, ID 6)');
    console.assert(myTasks[0].id === 1 && !myTasks[0].done, 'Pending task should be at the top');
    console.assert(myTasks[myTasks.length - 1].id === 4 && myTasks[myTasks.length - 1].done, 'Done task should be sorted to the bottom');

    // Other Tasks
    const otherTasks = sortAndFilterMessages(dynamicMock, 'otherTasksTab', '');
    console.assert(otherTasks.length === 1, 'Should have 1 other task');

    // All Tasks Tab (Should ignore OLD completed tasks only)
    const allTasks = sortAndFilterMessages(dynamicMock, 'allTasksTab', '');
    console.assert(allTasks.length === 5, 'Should have 5 tasks total (ID 5 old done excluded)');

    // Search
    const searchResult = sortAndFilterMessages(dynamicMock, 'allTasksTab', 'Wait');
    console.assert(searchResult.length === 1 && searchResult[0].id === 3, 'Search should find specific message');

    // No Date Graceful Handling
    const noDateSearch = sortAndFilterMessages(dynamicMock, 'allTasksTab', 'No date task');
    console.assert(noDateSearch.length === 1 && noDateSearch[0].id === 6, 'Should handle tasks without any date fields gracefully');

    // 빈 데이터 예외 처리 (Empty/Null inputs)
    const emptyResult = sortAndFilterMessages(null, 'allTasksTab', '');
    console.assert(emptyResult.length === 0, 'Should handle null messages gracefully');

    console.log('✅ sortAndFilterMessages passed');
}

function testClassify() {
    console.log('--- Testing classifyMessages ---');
    const counts = classifyMessages(mockMessages);
    console.assert(counts.all === 3, 'Should have 3 non-done tasks total');
    console.assert(counts.my === 1, 'Should have 1 task assigned to me');
    console.assert(counts.others === 1, 'Should have 1 task assigned to others');
    console.assert(counts.waiting === 1, 'Should have 1 waiting task');

    // 빈 배열 예외 처리
    const emptyCounts = classifyMessages([]);
    console.assert(emptyCounts.all === 0 && emptyCounts.my === 0, 'Should handle empty arrays');
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
    console.assert(distZero.slack === 0 && distZero.whatsapp === 0 && distZero.gmail === 0, 'Should handle 0 total tasks');

    // 정의되지 않은 소스(Unknown source) 예외 처리
    const distUnknown = calculateSourceDistribution({ slack: 10, unknown_app: 90 });
    console.assert(distUnknown.slack === 100, 'Should ignore unknown sources and calculate strictly based on known channels');
    console.log('✅ calculateSourceDistribution passed');
}

function runAllTests() {
    try {
        testSortAndFilter();
        testClassify();
        testHeatmapLevel();
        testSourceDistribution();
        console.log('\n✨ ALL TESTS PASSED SUCCESSFULLY! ✨');
    } catch (e) {
        console.error('\n❌ TEST FAILED:');
        console.error(e);
        process.exit(1);
    }
}

runAllTests();

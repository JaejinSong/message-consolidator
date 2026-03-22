/**
 * @file verify_renderer.js
 * @description Node.js script to verify renderer logic by mocking the DOM.
 */

// --- Mock DOM Environment (MUST BE BEFORE IMPORT) ---
global.localStorage = {
    getItem: (key) => null,
    setItem: (key, val) => { }
};

global.window = {
    location: { href: '' },
    addEventListener: () => { },
    dispatchEvent: () => { }
};

global.navigator = {
    language: 'ko-KR'
};

const mockElements = {};
global.document = {
    getElementById: (id) => {
        if (!mockElements[id]) {
            mockElements[id] = {
                id,
                textContent: '',
                innerHTML: '',
                classList: {
                    classes: new Set(['hidden']),
                    remove: function (cls) { this.classes.delete(cls); },
                    add: function (cls) { this.classes.add(cls); },
                    toggle: function (cls, force) {
                        if (force === undefined) {
                            if (this.classes.has(cls)) this.classes.delete(cls);
                            else this.classes.add(cls);
                        } else if (force) this.classes.add(cls);
                        else this.classes.delete(cls);
                    },
                    contains: function (cls) { return this.classes.has(cls); }
                },
                style: { width: '' },
                src: '',
                appendChild: () => { },
                querySelector: () => null,
                querySelectorAll: () => []
            };
        }
        return mockElements[id];
    },
    createElement: () => ({ classList: { add: () => { } }, appendChild: () => { }, setAttribute: () => { } }),
    body: { appendChild: () => { } },
    querySelector: (sel) => {
        if (sel === '.tab-btn.active') {
            return { getAttribute: (attr) => attr === 'data-tab' ? (global.document.currentTab || 'myTasksTab') : null };
        }
        return null;
    },
    querySelectorAll: () => ({ forEach: () => { } }),
    currentTab: 'myTasksTab'
};

// --- Now Import the Logic ---
async function runTests() {
    try {
        const { renderer } = await import('./renderer.js');

        // Mock state
        global.state = {
            currentLang: 'ko'
        };

        console.log('--- Testing renderer.updateUserProfile ---');

        const profile = {
            email: 'test@example.com',
            picture: 'http://pic.url',
            streak: 5,
            xp: 100,
            points: 50,
            level: 3
        };

        renderer.updateUserProfile(profile);

        const userProfile = global.document.getElementById('userProfile');
        const userEmail = global.document.getElementById('userEmail');
        const userStreak = global.document.getElementById('userStreak');
        const userPic = global.document.getElementById('userPicture'); // ID change in renderer.js

        console.assert(!userProfile.classList.contains('hidden'), 'userProfile should be visible');
        console.assert(userEmail.textContent === 'test@example.com', 'Email should match');
        console.assert(userStreak.textContent === '5🔥', 'Streak should match profile.streak with icon');
        console.assert(userPic.src === 'http://pic.url', 'Profile picture URL should match');
        console.assert(!userPic.classList.contains('hidden'), 'Profile picture should be visible');

        console.log('✅ updateUserProfile passed');

        console.log('--- Testing renderer.renderReleaseNotes (Markdown Parser) ---');

        const markdownContent = "### 업데이트\n## 주요 기능\n# 메인\n**강조**\n`코드`\n- 리스트\n---";
        renderer.renderReleaseNotes(markdownContent);

        const notesContainer = global.document.getElementById('releaseNotesContent');
        const htmlOutput = notesContainer.innerHTML;

        console.assert(htmlOutput.includes('<h3'), 'Should parse H3 (###)');
        console.assert(htmlOutput.includes('<h2'), 'Should parse H2 (##)');
        console.assert(htmlOutput.includes('<h1'), 'Should parse H1 (#)');
        console.assert(htmlOutput.includes('<strong'), 'Should parse Bold (**)');
        console.assert(htmlOutput.includes('<code'), 'Should parse Code (`)');
        console.assert(htmlOutput.includes('•'), 'Should parse List (-)');
        console.assert(htmlOutput.includes('<hr'), 'Should parse Divider (---)');

        console.log('✅ renderReleaseNotes passed (Markdown successfully parsed)');

        console.log('--- Testing renderer.renderMessages (created_at fallback) ---');
        // Switch mock tab to myTasksTab
        global.document.currentTab = 'myTasksTab';

        const fallbackTasks = [
            { id: 101, requester: 'Fallback', task: 'Created At Task', source: 'whatsapp', created_at: '2023-01-02T12:00:00Z', done: false, assignee: 'me' }
        ];
        renderer.renderMessages(fallbackTasks, {});
        const myTasksList = global.document.getElementById('myTasksList');
        console.assert(myTasksList.innerHTML.includes('Created At Task'), 'Should render task in myTasksList');
        console.log('✅ renderMessages (fallback) passed');

        console.log('--- Testing renderer.renderArchive ---');
        const archiveTasks = [
            { id: 201, source: 'slack', room: 'Project-A', task: 'Archived task', requester: 'Boss', assignee: 'me', timestamp: '2023-01-01T00:00:00Z', completed_at: '2023-01-01T12:00:00Z' }
        ];
        renderer.renderArchive(archiveTasks);
        const archiveBody = global.document.getElementById('archiveBody');
        console.assert(archiveBody.innerHTML.includes('Archived task'), 'Archive table should contain task');
        console.assert(archiveBody.innerHTML.includes('Project-A'), 'Archive table should contain room');
        console.assert(archiveBody.innerHTML.includes('<input type="checkbox"'), 'Archive table should contain checkboxes');
        console.log('✅ renderArchive passed');

        console.log('--- Testing renderer.renderMessages (Dynamic Tab Container) ---');
        // Switch mock tab to allTasksTab
        global.document.currentTab = 'allTasksTab';

        const mockTasks = [
            { id: 99, requester: 'Bob', task: 'Fix issue', source: 'slack', timestamp: '2023-01-01T12:00:00Z', done: false, assignee: 'me' }
        ];

        renderer.renderMessages(mockTasks, {});

        const allTasksList = global.document.getElementById('allTasksList');
        const allCount = global.document.getElementById('allCount');

        console.assert(allCount.textContent === 1, 'Total count badge should be updated to 1');
        console.assert(allTasksList.innerHTML.includes('Fix issue'), 'Container allTasksList should be populated dynamically');

        // --- 새로 추가된 평탄화(Flatten) 레이아웃 및 SVG/버튼 클래스 검증 ---
        console.assert(allTasksList.innerHTML.includes('col-source'), 'Should render flattened col-source structure');
        console.assert(allTasksList.innerHTML.includes('col-task'), 'Should render flattened col-task structure');
        console.assert(allTasksList.innerHTML.includes('<svg'), 'Should render SVG icon instead of emoji');
        console.assert(allTasksList.innerHTML.includes('done-btn'), 'Should render specific done-btn class');
        console.assert(allTasksList.innerHTML.includes('badge-abandoned'), 'Should render abandoned badge for old task');
        console.assert(allTasksList.innerHTML.includes('circle cx="12"'), 'Should render SVG icon inside badge or action');
        console.assert(allTasksList.innerHTML.includes('polyline points="3 6 5 6 21 6"'), 'Should render SVG trash icon for delete button');

        console.log('✅ renderMessages passed (Rendered to proper dynamic tab container)');

        console.log('--- Testing renderer.renderMessages (Empty State Grid) ---');
        global.document.currentTab = 'waitingTasksTab';
        renderer.renderMessages([], {});
        const waitingTasksList = global.document.getElementById('waitingTasksList');
        console.assert(waitingTasksList.innerHTML.includes('empty-state'), 'Should render empty state properly into dynamic tab container');
        console.log('✅ renderMessages (Empty State) passed');

        console.log('\n✨ RENDERER LOGIC VERIFIED! ✨');
    } catch (e) {
        console.error('\n❌ RENDERER TEST FAILED:');
        console.error(e);
        process.exit(1);
    }
}

runTests();

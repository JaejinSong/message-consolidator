/**
 * @file verify_renderer.js
 * @description Script to verify renderer-related logic without a browser.
 */

// Mock localStorage and other browser globals for Node.js environment
global.localStorage = {
    getItem: (key) => null,
    setItem: (key, value) => { }
};
global.window = global;
global.document = {
    getElementById: () => null,
    querySelector: () => null,
    querySelectorAll: () => []
};

const { renderer } = await import('./renderer.js');
const { I18N_DATA } = await import('./locales.js');

function testEmptyStateMessages() {
    console.log('--- Testing Empty State Messages ---');
    const lang = 'ko';
    const messages = I18N_DATA[lang].emptyStateMessages;

    console.assert(messages && messages.length >= 15, 'Should have at least 15 witty messages for Korean');

    // Check for specific natural phrasing improvements
    const hasCoffee = messages.some(m => m.includes('커피'));
    const hasPowerHouse = messages.some(m => m.includes('화력 발전소'));

    console.assert(hasCoffee, 'Witty message should contain "커피"');
    console.assert(hasPowerHouse, 'Witty message should contain "화력 발전소"');

    console.log('✅ Empty State Messages verified');
}

function testUpdateTokenBadge() {
    console.log('--- Testing updateTokenBadge ---');

    // 1. Mock DOM 환경 구성 (Node.js에는 document가 없으므로 가짜 객체 생성)
    const mockBadge = {
        classList: {
            removed: [],
            remove(cls) { this.removed.push(cls); }
        },
        textContent: '',
        attributes: {},
        setAttribute(attr, val) { this.attributes[attr] = val; },
        style: { transform: '' },
        onclick: null
    };

    // 글로벌 document와 alert 가로채기
    global.document = {
        getElementById: (id) => id === 'tokenUsageBadge' ? mockBadge : null
    };
    global.alert = (msg) => { mockBadge.lastAlert = msg; };

    // 2. 테스트 케이스 1: 백엔드에서 null이나 빈 객체가 넘어왔을 때 (방어 로직 확인)
    renderer.updateTokenBadge(null);
    console.assert(mockBadge.classList.removed.includes('hidden'), '사용량이 null이어도 hidden 클래스가 제거되어 무조건 보여야 함');
    console.assert(mockBadge.textContent === 'Token: 0', 'null일 때 Token: 0으로 표시되어야 함');
    console.assert(mockBadge.attributes['title'].includes('출력: 0'), 'null일 때 툴팁에 0이 표시되어야 함');

    // 3. 테스트 케이스 2: 값이 명시적으로 0이 넘어왔을 때
    renderer.updateTokenBadge({ todayTotal: 0, todayPrompt: 0, todayCompletion: 0, monthTotal: 0 });
    console.assert(mockBadge.textContent === 'Token: 0', '0일 때 숨겨지지 않고 Token: 0으로 표시되어야 함');

    // 4. 테스트 케이스 3: 정상적인 값이 넘어왔을 때 숫자 포맷팅(,) 확인
    renderer.updateTokenBadge({ todayTotal: 1500, todayPrompt: 1000, todayCompletion: 500, monthTotal: 50000 });
    console.assert(mockBadge.textContent === 'Token: 1,500', '1000 이상일 때 콤마가 포함되어야 함');
    console.assert(mockBadge.attributes['title'].includes('총합: 50,000'), '툴팁에도 콤마가 포함되어야 함');

    console.log('✅ updateTokenBadge verified');
}

function testRenderTenantAliasList() {
    console.log('--- Testing renderTenantAliasList ---');
    const mockContainer = {
        innerHTML: '',
        querySelectorAll: (selector) => []
    };

    global.document = {
        getElementById: (id) => id === 'normList' ? mockContainer : null,
        querySelector: () => null,
        querySelectorAll: () => []
    };

    // 테스트 케이스 1: 정상적인 배열 응답 (최신 API 규격)
    const arrayData = [{ original_name: "YOSEP", primary_name: "John" }];
    try {
        renderer.renderTenantAliasList(arrayData, () => { });
        console.assert(mockContainer.innerHTML.includes('YOSEP'), '원본 이름이 포함되어야 함');
        console.assert(mockContainer.innerHTML.includes('John'), '매핑된 이름이 포함되어야 함');
        console.assert(mockContainer.innerHTML.includes('→'), '매핑 화살표가 포함되어야 함');
        console.log('✅ renderTenantAliasList handled array of objects');
    } catch (e) {
        console.error('❌ renderTenantAliasList failed with array:', e);
        process.exit(1);
    }

    // 테스트 케이스 2: 래핑된 객체 응답 (e.map 에러 유발 케이스 방어 확인)
    const wrappedData = { aliases: [{ original_name: "OLD", primary_name: "NEW" }] };
    try {
        renderer.renderTenantAliasList(wrappedData, () => { });
        console.assert(mockContainer.innerHTML.includes('OLD'), '래핑된 객체에서 원본 이름을 파싱해야 함');
        console.assert(mockContainer.innerHTML.includes('NEW'), '래핑된 객체에서 매핑된 이름을 파싱해야 함');
        console.log('✅ renderTenantAliasList handled object-wrapped array');
    } catch (e) {
        console.error('❌ renderTenantAliasList failed with wrapped object:', e);
        process.exit(1);
    }

    // 테스트 케이스 3: 빈 객체
    renderer.renderTenantAliasList({}, () => { });
    console.assert(mockContainer.innerHTML.includes('No tenant aliases'), '빈 객체일 때 안내 메시지가 표시되어야 함');
    console.log('✅ renderTenantAliasList handled empty object');
}

function testRenderContactMappings() {
    console.log('--- Testing renderContactMappings ---');
    const mockContainer = {
        innerHTML: '',
        querySelectorAll: () => []
    };

    global.document = {
        getElementById: (id) => id === 'contactList' ? mockContainer : null,
        querySelector: () => null,
        querySelectorAll: () => []
    };

    // 테스트 케이스 1: 래핑된 객체 (e.map 방어 및 동적 필드명 파싱 확인)
    const wrappedData = { mappings: [{ rep_name: "팀장님", aliases: "boss, leader" }] };
    try {
        renderer.renderContactMappings(wrappedData, () => { });
        console.assert(mockContainer.innerHTML.includes('팀장님'), '대표 이름이 파싱되어야 함');
        console.assert(mockContainer.innerHTML.includes('boss, leader'), '별칭들이 파싱되어야 함');
        console.log('✅ renderContactMappings handled object-wrapped array');
    } catch (e) {
        console.error('❌ renderContactMappings failed with wrapped object:', e);
        process.exit(1);
    }

    // 테스트 케이스 2: 빈 객체
    renderer.renderContactMappings({}, () => { });
    console.assert(mockContainer.innerHTML.includes('No contact mappings'), '빈 객체일 때 안내 메시지가 표시되어야 함');
    console.log('✅ renderContactMappings handled empty object');
}

function testShowToast() {
    console.log('--- Testing showToast ---');

    // 1. Toast 전용 가짜 DOM 및 애니메이션 프레임 환경 구성
    global.requestAnimationFrame = (cb) => cb();
    const mockBody = {
        appended: [],
        appendChild(el) { this.appended.push(el); }
    };

    global.document = {
        createElement: (tag) => ({
            tagName: tag, className: '', style: {}, textContent: '', children: [],
            appendChild(child) { this.children.push(child); },
            remove() { this._removed = true; }
        }),
        querySelectorAll: (selector) => [], // Added to fix TypeError in showToast
        body: mockBody
    };

    // 2. 에러 토스트(error) 생성 검증
    renderer.showToast('결제에 실패했습니다.', 'error');
    let toast = mockBody.appended[0];
    console.assert(toast !== undefined, '토스트 요소가 body에 추가되어야 함');
    console.assert(toast.className === 'toast-popup toast-error', 'error 클래스가 정확히 지정되어야 함');
    console.assert(toast.children[1].textContent === '결제에 실패했습니다.', '메시지 텍스트가 정상적으로 삽입되어야 함');
    console.assert(toast.style.background.includes('255, 59, 48'), '에러용 붉은색 배경이 적용되어야 함');

    // 3. 성공 토스트(success) 생성 검증
    mockBody.appended = []; // 리셋
    renderer.showToast('구매 성공!', 'success');
    toast = mockBody.appended[0];
    console.assert(toast.className === 'toast-popup toast-success', 'success 클래스가 지정되어야 함');
    console.assert(toast.children[0].textContent === '✅', '성공 아이콘이 정상적으로 삽입되어야 함');

    console.log('✅ showToast verified');
}

testEmptyStateMessages();
testUpdateTokenBadge();
testRenderTenantAliasList();
testRenderContactMappings();
testShowToast();

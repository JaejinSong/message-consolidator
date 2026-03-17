const I18N_DATA = {
    ko: {
        subTitle: "자동화된 Slack & WhatsApp 업무 대시보드",
        realTimeTasks: "실행 중인 업무",
        scanNow: "🔄 지금 스캔",
        scanning: "⌛ 스캔 중...",
        slackMonitoring: "Slack: 모니터링 중",
        waMonitoring: "WhatsApp: 모니터링 중",
        waConnected: "WhatsApp: 연결됨",
        waDisconnected: "WhatsApp: 연결 끊김",
        waReqTitle: "WhatsApp 연결 필요",
        waReqDesc: "모바일 WhatsApp 앱으로 QR 코드를 스캔해 주세요.",
        generating: "생성 중...",
        getQR: "새 QR 코드 받기",
        loading: "Gemini가 새로운 업무를 찾는 중입니다...",
        noTasks: "수집된 업무가 없습니다. 백그라운드 스캔을 기다려주세요.",
        viewOriginal: "🔗 원문 보기",
        markDone: "완료로 표시",
        doneBtn: "✓ 완료",
        qrError: "QR 발급 실패: ",
        myTasks: "내 업무",
        otherTasks: "기타 업무",
        allTasks: "전체",
        logout: "로그아웃"
    },
    en: {
        subTitle: "Automated Slack & WhatsApp Task Dashboard",
        realTimeTasks: "Real-time Tasks",
        scanNow: "🔄 Scan Now",
        scanning: "⌛ Scanning...",
        slackMonitoring: "Slack: Monitoring",
        waMonitoring: "WhatsApp: Monitoring",
        waConnected: "WhatsApp: Connected",
        waDisconnected: "WhatsApp: Disconnected",
        waReqTitle: "WhatsApp Connection Required",
        waReqDesc: "Please scan the QR code with your mobile WhatsApp app.",
        generating: "Generating...",
        getQR: "Get New QR Code",
        loading: "Gemini is scanning for new tasks...",
        noTasks: "No tasks collected. Please wait for background scan.",
        viewOriginal: "🔗 View Original",
        markDone: "Mark as Done",
        doneBtn: "✓ Done",
        qrError: "QR Generation Failed: ",
        myTasks: "My Tasks",
        otherTasks: "Other Tasks",
        allTasks: "All Tasks",
        logout: "Logout"
    },
    id: {
        subTitle: "Dasbor Tugas Slack & WhatsApp Otomatis",
        realTimeTasks: "Tugas Real-time",
        scanNow: "🔄 Pindai Sekarang",
        scanning: "⌛ Memindai...",
        slackMonitoring: "Slack: Memantau",
        waMonitoring: "WhatsApp: Memantau",
        waConnected: "WhatsApp: Terhubung",
        waDisconnected: "WhatsApp: Terputus",
        waReqTitle: "Koneksi WhatsApp Diperlukan",
        waReqDesc: "Silakan pindai kode QR dengan aplikasi WhatsApp seluler Anda.",
        generating: "Menghasilkan...",
        getQR: "Dapatkan Kode QR Baru",
        loading: "Gemini sedang memindai tugas baru...",
        noTasks: "Tidak ada tugas yang dikumpulkan. Harap tunggu pemindaian latar belakang.",
        viewOriginal: "🔗 Lihat Asli",
        markDone: "Tandai Selesai",
        doneBtn: "✓ Selesai",
        qrError: "Gagal Menghasilkan QR: ",
        myTasks: "Tugas Saya",
        otherTasks: "Tugas Lainnya",
        allTasks: "Semua Tugas",
        logout: "Keluar"
    },
    th: {
        subTitle: "แดชบอร์ดงาน Slack & WhatsApp อัตโนมัติ",
        realTimeTasks: "งานแบบเรียลไทม์",
        scanNow: "🔄 สแกนทันที",
        scanning: "⌛ กำลังสแกน...",
        slackMonitoring: "Slack: กำลังตรวจสอบ",
        waMonitoring: "WhatsApp: กำลังตรวจสอบ",
        waConnected: "WhatsApp: เชื่อมต่อแล้ว",
        waDisconnected: "WhatsApp: ตัดการเชื่อมต่อ",
        waReqTitle: "จำเป็นต้องเชื่อมต่อ WhatsApp",
        waReqDesc: "โปรดสแกนรหัส QR ด้วยแอป WhatsApp บนมือถือของคุณ",
        generating: "กำลังสร้าง...",
        getQR: "รับรหัส QR ใหม่",
        loading: "Gemini กำลังสแกนหางานใหม่...",
        noTasks: "ไม่มีงานที่รวบรวมไว้ โปรดรอการสแกนพื้นหลัง",
        viewOriginal: "🔗 ดูต้นฉบับ",
        markDone: "ทำเครื่องหมายว่าเสร็จสิ้น",
        doneBtn: "✓ เสร็จสิ้น",
        qrError: "การสร้าง QR ล้มเหลว: ",
        myTasks: "งานของฉัน",
        otherTasks: "งานอื่นๆ",
        allTasks: "งานทั้งหมด",
        logout: "ออกจากระบบ"
    }
};

let userProfile = { email: "", picture: "", name: "" };
let userAliases = [];

let currentLang = localStorage.getItem('mc_lang') || 'ko';

const updateUILanguage = (lang) => {
    const data = I18N_DATA[lang];
    if (!data) return;

    document.getElementById('subTitle').textContent = data.subTitle;
    document.querySelector('.dashboard-header h2').textContent = data.realTimeTasks;
    
    const scanBtn = document.getElementById('scanBtn');
    if (!scanBtn.disabled) {
        scanBtn.textContent = data.scanNow;
    }

    document.getElementById('slackStatus').textContent = data.slackMonitoring;
    // WhatsApp status is handled by checkWhatsAppStatus but we can update its base text if needed
    
    document.querySelector('#waLoginSection h3').textContent = data.waReqTitle;
    document.querySelector('#waLoginSection p').textContent = data.waReqDesc;
    document.getElementById('qrPlaceholder').textContent = data.generating;
    document.getElementById('getQRBtn').textContent = data.getQR;
    document.querySelector('#loading p').textContent = data.loading;

    // Update Tab Labels
    const myTab = document.querySelector('[data-tab="myTasksTab"]');
    const otherTab = document.querySelector('[data-tab="otherTasksTab"]');
    const allTab = document.querySelector('[data-tab="allTasksTab"]');
    
    if (myTab) myTab.innerHTML = `${data.myTasks} <span class="badge count" id="myCount">0</span>`;
    if (otherTab) otherTab.innerHTML = `${data.otherTasks} <span class="badge count" id="otherCount">0</span>`;
    if (allTab) allTab.innerHTML = `${data.allTasks} <span class="badge count" id="allCount">0</span>`;
    const logoutBtn = document.getElementById('logoutBtn');
    if (logoutBtn) {
        logoutBtn.textContent = `🚪 ${data.logout || 'Logout'}`;
    }

    // Refresh messages to update card buttons
    fetchMessages();
};

const renderMessages = (messages) => {
    const myGrid = document.getElementById('myTasksList');
    const otherGrid = document.getElementById('otherTasksList');
    const allGrid = document.getElementById('allTasksList');
    if (myGrid) myGrid.innerHTML = '';
    if (otherGrid) otherGrid.innerHTML = '';
    if (allGrid) allGrid.innerHTML = '';

    const data = I18N_DATA[currentLang];
    
    // Keywords for local filtering (name/email based)
    const keywords = [userProfile.email];
    if (userProfile.name) keywords.push(userProfile.name);
    // Add common variations
    if (userProfile.email && userProfile.email.includes('@')) {
        keywords.push(userProfile.email.split('@')[0]);
    }
    
    // Add Aliases from backend for precise matches
    if (userAliases && Array.isArray(userAliases)) {
        userAliases.forEach(alias => {
            if (alias && alias.trim()) keywords.push(alias.trim());
        });
    }

    if (!messages || messages.length === 0) {
        const noMsg = `<p style="text-align: center; color: var(--text-dim); margin-top: 1rem; width: 100%;">${data.noTasks}</p>`;
        if (myGrid) myGrid.innerHTML = noMsg;
        if (otherGrid) otherGrid.innerHTML = noMsg;
        if (allGrid) allGrid.innerHTML = noMsg;
        const myCountEl = document.getElementById('myCount');
        const otherCountEl = document.getElementById('otherCount');
        const allCountEl = document.getElementById('allCount');
        if (myCountEl) myCountEl.textContent = '0';
        if (otherCountEl) otherCountEl.textContent = '0';
        if (allCountEl) allCountEl.textContent = '0';
        return;
    }
    
    // Filter out deleted messages if any (server side handles this but just in case)
    const activeMessages = messages.filter(m => !m.is_deleted);

    const sorted = activeMessages.sort((a, b) => {
        // First priority: Done status (done at the bottom)
        const aDone = !!a.done;
        const bDone = !!b.done;
        if (aDone !== bDone) {
            return aDone ? 1 : -1;
        }
        
        // Second priority: CreatedAt (newest first)
        const dateA = new Date(a.created_at);
        const dateB = new Date(b.created_at);
        return dateB - dateA;
    });
    console.log("Sorted messages sample:", sorted.slice(0, 3).map(m => ({task: m.task, done: m.done, created: m.created_at})));

    let myCount = 0;
    let otherCount = 0;
    let allCount = 0;

    sorted.forEach(m => {
        // Medium confidence: keyword match (name, email prefix, aliases)
        const isMyTask = keywords.some(kw => 
            (m.task && m.task.toLowerCase().includes(kw.toLowerCase())) || 
            (m.assignee && m.assignee.toLowerCase().includes(kw.toLowerCase())) ||
            (m.requester && m.requester.toLowerCase().includes(kw.toLowerCase()))
        );

        const card = createCardElement(m, data);
        
        // All tasks grid
        if (allGrid) {
            allGrid.appendChild(card.cloneNode(true));
        } else {
            console.error('allTasksList element not found!');
        }
        allCount++;

        if (isMyTask) {
            if (myGrid) myGrid.appendChild(card);
            myCount++;
        } else {
            if (otherGrid) otherGrid.appendChild(card);
            otherCount++;
        }
    });

    const myCountEl = document.getElementById('myCount');
    const otherCountEl = document.getElementById('otherCount');
    const allCountEl = document.getElementById('allCount');
    if (myCountEl) myCountEl.textContent = myCount;
    if (otherCountEl) otherCountEl.textContent = otherCount;
    if (allCountEl) allCountEl.textContent = allCount;
};

const createCardElement = (m, data) => {
    const card = document.createElement('div');
    card.className = `card ${m.source} ${m.done ? 'done' : ''}`;
    
    let linkHtml = '';
    if (m.link) {
        linkHtml = `<a href="${m.link}" target="_blank" class="link-btn" style="margin-top: 0;">${data.viewOriginal}</a>`;
    }

    let sourceIcon = '';
    if (m.source.toLowerCase() === 'slack') {
        sourceIcon = `
            <svg viewBox="0 0 100 100" style="width: 20px; height: 20px;">
                <path fill="#E01E5A" d="M22.9,53.8V42.3c0-3.2-2.6-5.8-5.8-5.8s-5.8,2.6-5.8,5.8v11.5c0,3.2,2.6,5.8,5.8,5.8S22.9,57,22.9,53.8z"/>
                <path fill="#E01E5A" d="M28.6,42.3c0-3.2,2.6-5.8,5.8-5.8h11.5c3.2,0,5.8,2.6,5.8,5.8s-2.6,5.8-5.8,5.8H34.4C31.2,48.1,28.6,45.5,28.6,42.3z"/>
                <path fill="#36C5F0" d="M46.2,22.9h11.5c3.2,0,5.8-2.6,5.8-5.8s-2.6-5.8-5.8-5.8H46.2c-3.2,0-5.8,2.6-5.8,5.8S43,22.9,46.2,22.9z"/>
                <path fill="#36C5F0" d="M57.7,28.6c3.2,0,5.8,2.6,5.8,5.8v11.5c0,3.2-2.6,5.8-5.8,5.8s-5.8-2.6-5.8-5.8V34.4C51.9,31.2,54.5,28.6,57.7,28.6z"/>
                <path fill="#2EB67D" d="M77.1,46.2v11.5c0,3.2,2.6,5.8,5.8,5.8s5.8-2.6,5.8-5.8V46.2c0-3.2-2.6-5.8-5.8-5.8S77.1,43,77.1,46.2z"/>
                <path fill="#2EB67D" d="M71.4,57.7c0,3.2-2.6,5.8-5.8,5.8H54.1c-3.2,0-5.8-2.6-5.8-5.8s2.6-5.8,5.8-5.8h11.5C68.8,51.9,71.4,54.5,71.4,57.7z"/>
                <path fill="#ECB22E" d="M53.8,77.1H42.3c-3.2,0-5.8,2.6-5.8,5.8s2.6,5.8,5.8,5.8h11.5c3.2,0,5.8-2.6,5.8-5.8S57,77.1,53.8,77.1z"/>
                <path fill="#ECB22E" d="M42.3,71.4c-3.2,0-5.8-2.6-5.8-5.8V54.1c0-3.2,2.6-5.8,5.8-5.8c3.2,0,5.8,2.6,5.8,5.8v11.5C48.1,68.8,45.5,71.4,42.3,71.4z"/>
            </svg>`;
    } else if (m.source.toLowerCase() === 'whatsapp') {
        sourceIcon = `
            <svg viewBox="0 0 448 512" style="width: 20px; height: 20px; fill: #25d366;">
                <path d="M380.9 97.1C339 55.1 283.2 32 223.9 32c-122.4 0-222 99.6-222 222 0 39.1 10.2 77.3 29.6 111L0 480l117.7-30.9c32.4 17.7 68.9 27 106.1 27h.1c122.3 0 224.1-99.6 224.1-222 0-59.3-25.2-115-67.1-157zm-157 341.6c-33.2 0-65.7-8.9-94-25.7l-6.7-4-69.8 18.3L72 359.2l-4.4-7c-18.5-29.4-28.2-63.3-28.2-98.2 0-101.7 82.8-184.5 184.6-184.5 49.3 0 95.6 19.2 130.4 54.1 34.8 34.9 56.2 81.2 56.1 130.5 0 101.8-84.9 184.6-186.6 184.6zm101.2-138.2c-5.5-2.8-32.8-16.2-37.9-18-5.1-1.9-8.8-2.8-12.5 2.8-3.7 5.6-14.3 18-17.6 21.8-3.2 3.7-6.5 4.2-12 1.4-5.5-2.8-23.2-8.5-44.2-27.1-16.4-14.6-27.4-32.7-30.6-38.2-3.2-5.6-.3-8.6 2.4-11.3 2.5-2.4 5.5-6.5 8.3-9.7 2.8-3.3 3.7-5.6 5.6-9.3 1.8-3.7.9-6.9-.5-9.7-1.4-2.8-12.5-30.1-17.1-41.2-4.5-10.8-9.1-9.3-12.5-9.5-3.2-.2-6.9-.2-10.6-.2-3.7 0-9.7 1.4-14.8 6.9-5.1 5.6-19.4 19-19.4 46.3 0 27.3 19.9 53.7 22.6 57.4 2.8 3.7 39.1 59.7 94.8 83.8 13.2 5.7 23.5 9.2 31.6 11.8 13.3 4.2 25.4 3.6 35 2.2 10.7-1.6 32.8-13.4 37.4-26.4 4.6-13 4.6-24.1 3.2-26.4-1.3-2.5-5-3.9-10.5-6.6z"/>
            </svg>`;
    }

    card.innerHTML = `
        <div class="col-source" title="${m.source}">${sourceIcon || '<span class="badge">' + m.source + '</span>'}</div>
        <div class="col-room meta-val">${m.room || '-'}</div>
        <div class="col-task task-title" title="${m.task}">${m.task}</div>
        <div class="col-requester meta-val">${m.requester}</div>
        <div class="col-assignee meta-val">${m.assignee}</div>
        <div class="col-time meta-val" style="font-size: 0.75rem;">${m.assigned_at}</div>
        <div class="col-actions">
            ${linkHtml}
            <button class="done-btn" data-id="${m.id}" data-done="${!m.done}">
                ${m.done ? data.doneBtn : data.markDone}
            </button>
            <button class="delete-btn" data-id="${m.id}" style="background: rgba(255, 59, 48, 0.1); color: #ff3b30; border-color: #ff3b30; padding: 0.3rem 0.6rem; border-radius: 8px; border: 1px solid; cursor: pointer; font-size: 0.7rem;">
                🗑️
            </button>
        </div>
    `;

    // Add event listeners to the buttons within the card
    card.querySelector('.done-btn').addEventListener('click', () => toggleDone(m.id, !m.done));
    card.querySelector('.delete-btn').addEventListener('click', () => deleteTask(m.id));

    return card;
};

const toggleDone = async (id, done) => {
    try {
        const resp = await fetch('/api/messages/done', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id, done })
        });
        if (resp.ok) {
            fetchMessages();
        }
    } catch (e) {
        console.error("Failed to toggle done:", e);
    }
};

const deleteTask = async (id) => {
    if (!confirm("Are you sure you want to delete this task? It will be moved to the archive.")) return;
    try {
        const resp = await fetch('/api/messages/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id })
        });
        if (resp.ok) {
            fetchMessages();
            fetchArchive(); // Update archive table if it's currently open
        }
    } catch (e) {
        console.error("Failed to delete task:", e);
    }
};

let waConnected = false;

const checkWhatsAppStatus = async () => {
    try {
        const resp = await fetch('/api/whatsapp/status');
        const data = await resp.json();
        const waStatusBadge = document.getElementById('waStatus');
        const waLoginSection = document.getElementById('waLoginSection');
        const i18n = I18N_DATA[currentLang];

        if (data.status && data.status.toLowerCase() === 'connected') {
            waConnected = true;
            waStatusBadge.textContent = i18n.waConnected;
            waStatusBadge.style.background = 'rgba(37, 211, 102, 0.2)';
            waStatusBadge.style.color = '#25d366';
            waLoginSection.classList.add('hidden');
        } else {
            waConnected = false;
            waStatusBadge.textContent = i18n.waDisconnected;
            waStatusBadge.style.background = 'rgba(255, 255, 255, 0.08)';
            waStatusBadge.style.color = 'var(--text-dim)';
            waLoginSection.classList.remove('hidden');
        }
    } catch (e) {
        console.error("Failed to check WA status:", e);
    }
};

// QR logic moved to initApp

const triggerScan = async () => {
    const btn = document.getElementById('scanBtn');
    const loading = document.getElementById('loading');
    const i18n = I18N_DATA[currentLang];
    
    btn.disabled = true;
    btn.textContent = i18n.scanning;
    loading.classList.remove('hidden');

    // Map code to Gemini readable language name
    const langMap = {
        'ko': 'Korean',
        'en': 'English',
        'id': 'Indonesian',
        'th': 'Thai'
    };

    try {
        const langParam = langMap[currentLang] || 'Korean';
        await fetch(`/api/scan?lang=${langParam}`);
        setTimeout(() => {
            fetchMessages();
            btn.disabled = false;
            btn.textContent = i18n.scanNow;
            loading.classList.add('hidden');
        }, 5000);
    } catch (e) {
        console.error(e);
        btn.disabled = false;
        btn.textContent = i18n.scanNow;
        loading.classList.add('hidden');
    }
};

// Scan listener moved to initApp

const translateExistingTasks = async (lang) => {
    const loading = document.getElementById('loading');
    loading.classList.remove('hidden');
    
    const langMap = {
        'ko': 'Korean',
        'en': 'English',
        'id': 'Indonesian',
        'th': 'Thai'
    };

    try {
        const langParam = langMap[lang] || 'Korean';
        const resp = await fetch(`/api/translate?lang=${langParam}`);
        if (resp.ok) {
            await fetchMessages();
        }
    } catch (e) {
        console.error("Translation failed:", e);
    } finally {
        loading.classList.add('hidden');
    }
};

// Language Selector Logic
// Language listeners moved to initApp

// Periodic check functions (referenced in initApp)
const startIntervals = () => {
    if (window.fetchInterval) clearInterval(window.fetchInterval);
    if (window.statusInterval) clearInterval(window.statusInterval);
    
    window.fetchInterval = setInterval(fetchMessages, 30000);
    window.statusInterval = setInterval(checkWhatsAppStatus, 30000);
};

// Archive View Logic
const fetchArchive = async () => {
    try {
        const resp = await fetch('/api/messages/archive');
        const data = await resp.json();
        renderArchive(data);
    } catch (e) {
        console.error("Failed to fetch archive:", e);
    }
};

const renderArchive = (messages) => {
    const body = document.getElementById('archiveBody');
    body.innerHTML = '';
    
    document.getElementById('archiveCount').textContent = messages.length;

    if (!messages || messages.length === 0) {
        body.innerHTML = `<tr><td colspan="7" style="text-align: center; color: var(--text-dim);">No archived tasks.</td></tr>`;
        return;
    }

    messages.forEach(m => {
        const tr = document.createElement('tr');
        const compAt = m.completed_at ? new Date(m.completed_at).toLocaleString() : '-';
        tr.innerHTML = `
            <td><span class="badge">${m.source}</span></td>
            <td>${m.room || '-'}</td>
            <td style="font-weight: 600;">${m.task}</td>
            <td>${m.requester}</td>
            <td>${m.assignee}</td>
            <td style="font-size: 0.8rem; color: var(--text-dim);">${m.assigned_at}</td>
            <td style="font-size: 0.8rem; color: var(--accent-color);">${compAt}</td>
        `;
        body.appendChild(tr);
    });
};

// Event listeners moved to initApp

const fetchMessages = async () => {
    try {
        const resp = await fetch('/api/messages');
        if (!resp.ok) {
            console.error("Messages fetch failed:", resp.status);
            return;
        }
        const data = await resp.json();
        console.log("Fetched active messages count:", data.length);
        renderMessages(data);
    } catch (e) {
        console.error("fetchMessages error:", e);
    }
};

const fetchUserProfile = async () => {
    try {
        console.log("Fetching user profile...");
        const resp = await fetch('/api/user/info');
        if (!resp.ok) {
            console.error("User info fetch failed:", resp.status);
            fetchMessages(); // Try fetching anyway
            return;
        }
        const data = await resp.json();
        console.log("User profile received:", data.email);
        userProfile = data;
        userAliases = data.aliases || [];
        
        const profileDiv = document.getElementById('userProfile');
        const img = document.getElementById('userPicture');
        const email = document.getElementById('userEmail');
        
        if (data.email) {
            email.textContent = data.email;
            if (data.picture && data.picture !== "") {
                img.src = data.picture;
            } else {
                img.src = 'https://www.gravatar.com/avatar/00000000000000000000000000000000?d=mp&f=y';
            }
            profileDiv.classList.remove('hidden');
            console.log("Profile visible for:", data.email);
        } else {
            console.warn("User profile data empty!");
        }
        // Re-render messages with user info for better filtering
        fetchMessages();
    } catch (e) {
        console.error("Failed to fetch user profile:", e);
        fetchMessages(); // Try fetching anyway
    }
};

// Alias Management Functions
const fetchAliases = async () => {
    try {
        const resp = await fetch('/api/user/aliases');
        if (resp.ok) {
            const aliases = await resp.json();
            userAliases = aliases;
            renderAliasList();
            fetchMessages(); // Refresh filter
        }
    } catch (e) {
        console.error("Failed to fetch aliases:", e);
    }
};

const renderAliasList = () => {
    const list = document.getElementById('aliasList');
    if (!list) return;
    list.innerHTML = '';
    userAliases.forEach(alias => {
        const item = document.createElement('div');
        item.className = 'alias-item';
        item.innerHTML = `
            <span>${alias}</span>
            <button class="remove-alias" data-alias="${alias}">&times;</button>
        `;
        list.appendChild(item);
    });

    // Add listners for removal
    list.querySelectorAll('.remove-alias').forEach(btn => {
        btn.addEventListener('click', () => {
            const alias = btn.getAttribute('data-alias');
            removeAlias(alias);
        });
    });
};

const addAlias = async () => {
    const input = document.getElementById('newAliasInput');
    const alias = input.value.trim();
    if (!alias) return;

    try {
        const resp = await fetch('/api/user/alias/add', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ alias })
        });
        if (resp.ok) {
            input.value = '';
            fetchAliases();
        }
    } catch (e) {
        console.error("Failed to add alias:", e);
    }
};

const removeAlias = async (alias) => {
    try {
        const resp = await fetch('/api/user/alias/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ alias })
        });
        if (resp.ok) {
            fetchAliases();
        }
    } catch (e) {
        console.error("Failed to remove alias:", e);
    }
};

// Initial load
const initApp = () => {
    console.log("Initializing App...");
    
    // Set initial value
    const langSelect = document.getElementById('languageSelect');
    if (langSelect) {
        langSelect.value = currentLang;
        langSelect.addEventListener('change', async (e) => {
            currentLang = e.target.value;
            localStorage.setItem('mc_lang', currentLang);
            updateUILanguage(currentLang);
            await translateExistingTasks(currentLang);
        });
    }

    // Tab Switching Logic
    const switchTab = (tabId) => {
        console.log("Switching to tab:", tabId);
        const tabs = document.querySelectorAll('.tab-btn');
        const contents = document.querySelectorAll('.tab-content');
        
        tabs.forEach(b => b.classList.remove('active'));
        contents.forEach(c => c.classList.remove('active'));
        
        const activeBtn = document.querySelector(`[data-tab="${tabId}"]`);
        const activeContent = document.getElementById(tabId);
        
        if (activeBtn) activeBtn.classList.add('active');
        if (activeContent) {
            activeContent.classList.add('active');
            console.log("Tab content is now active:", tabId);
        } else {
            console.error("CRITICAL: Tab content element NOT FOUND for ID:", tabId);
            // Fallback: search for all content divs and log their IDs
            console.log("Available tab-content IDs:", Array.from(contents).map(c => c.id));
        }
    };

    document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            const tabId = btn.getAttribute('data-tab');
            switchTab(tabId);
        });
    });

    // Forced Initial Tab Setup
    setTimeout(() => {
        console.log("Forcing initial tab state: myTasksTab");
        switchTab('myTasksTab');
    }, 500);

    // Archive Logic
    const archiveLink = document.getElementById('archiveLink');
    if (archiveLink) {
        archiveLink.addEventListener('click', (e) => {
            e.preventDefault();
            const tabsContainer = document.querySelector('.tabs-container');
            const dashboardHeader = document.querySelector('.dashboard-header');
            if (tabsContainer) tabsContainer.classList.add('hidden');
            if (dashboardHeader) dashboardHeader.classList.add('hidden');
            document.getElementById('archiveSection').classList.remove('hidden');
            fetchArchive();
        });
    }

    const closeArchiveBtn = document.getElementById('closeArchiveBtn');
    if (closeArchiveBtn) {
        closeArchiveBtn.addEventListener('click', () => {
            const tabsContainer = document.querySelector('.tabs-container');
            const dashboardHeader = document.querySelector('.dashboard-header');
            if (tabsContainer) tabsContainer.classList.remove('hidden');
            if (dashboardHeader) dashboardHeader.classList.remove('hidden');
            document.getElementById('archiveSection').classList.add('hidden');
        });
    }

    const exportCsvBtn = document.getElementById('exportCsvBtn');
    if (exportCsvBtn) {
        exportCsvBtn.addEventListener('click', () => {
            window.location.href = '/api/messages/archive/export';
        });
    }

    // QR Logic
    const getQRBtn = document.getElementById('getQRBtn');
    if (getQRBtn) {
        getQRBtn.addEventListener('click', async () => {
            const btn = document.getElementById('getQRBtn');
            const img = document.getElementById('waQRImg');
            const placeholder = document.getElementById('qrPlaceholder');
            const i18n = I18N_DATA[currentLang];

            btn.disabled = true;
            placeholder.textContent = i18n.generating;
            placeholder.classList.remove('hidden');
            img.classList.add('hidden');

            try {
                const resp = await fetch('/api/whatsapp/qr');
                const data = await resp.json();
                if (data.qr) {
                    img.src = `data:image/png;base64,${data.qr}`;
                    img.classList.remove('hidden');
                    placeholder.classList.add('hidden');

                    const poll = setInterval(async () => {
                        await checkWhatsAppStatus();
                        if (waConnected) {
                            clearInterval(poll);
                            btn.disabled = false;
                        }
                    }, 3000);
                }
            } catch (e) {
                placeholder.textContent = 'Error';
                alert(i18n.qrError + e.message);
                btn.disabled = false;
            }
        });
    }

    // Scan Logic
    const scanBtn = document.getElementById('scanBtn');
    if (scanBtn) {
        scanBtn.addEventListener('click', triggerScan);
    }

    // Settings Modal Logic
    const settingsBtn = document.getElementById('settingsBtn');
    const settingsModal = document.getElementById('settingsModal');
    const closeSettingsBtn = document.getElementById('closeSettingsBtn');

    if (settingsBtn) {
        settingsBtn.addEventListener('click', () => {
            settingsModal.classList.remove('hidden');
            renderAliasList();
        });
    }

    if (closeSettingsBtn) {
        closeSettingsBtn.addEventListener('click', () => {
            settingsModal.classList.add('hidden');
        });
    }

    window.addEventListener('click', (e) => {
        if (e.target === settingsModal) {
            settingsModal.classList.add('hidden');
        }
    });

    const addAliasBtn = document.getElementById('addAliasBtn');
    if (addAliasBtn) {
        addAliasBtn.addEventListener('click', addAlias);
    }
    const aliasInput = document.getElementById('newAliasInput');
    if (aliasInput) {
        aliasInput.addEventListener('keypress', (e) => {
            if (e.key === 'Enter') addAlias();
        });
    }

    // Initial Data Fetch
    updateUILanguage(currentLang);
    checkWhatsAppStatus();
    fetchUserProfile();

    // Start periodic refreshes
    startIntervals();
    console.log("App initialization complete.");
};

window.addEventListener('error', (e) => {
    console.error("Global JS Error:", e.message, "at", e.filename, ":", e.lineno);
});

if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initApp);
} else {
    initApp();
}

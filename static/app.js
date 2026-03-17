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
    const myTab = document.querySelector('.tab-btn[data-tab="my"]');
    const otherTab = document.querySelector('.tab-btn[data-tab="other"]');
    
    if (myTab) myTab.innerHTML = `${data.myTasks || 'My Tasks'} <span class="badge count" id="myCount">0</span>`;
    if (otherTab) otherTab.innerHTML = `${data.otherTasks || 'Other Tasks'} <span class="badge count" id="otherCount">0</span>`;

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
    myGrid.innerHTML = '';
    otherGrid.innerHTML = '';

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
        myGrid.innerHTML = noMsg;
        otherGrid.innerHTML = noMsg;
        document.getElementById('myCount').textContent = '0';
        document.getElementById('otherCount').textContent = '0';
        return;
    }
    
    // Filter out deleted messages if any (server side handles this but just in case)
    const activeMessages = messages.filter(m => !m.is_deleted);

    const sorted = activeMessages.sort((a, b) => {
        if (a.done === b.done) {
            return new Date(b.created_at) - new Date(a.created_at);
        }
        return a.done ? 1 : -1;
    });

    let myCount = 0;
    let otherCount = 0;

    sorted.forEach(m => {
        // Medium confidence: keyword match (name, email prefix, aliases)
        const isMyTask = keywords.some(kw => 
            (m.task && m.task.toLowerCase().includes(kw.toLowerCase())) || 
            (m.assignee && m.assignee.toLowerCase().includes(kw.toLowerCase())) ||
            (m.requester && m.requester.toLowerCase().includes(kw.toLowerCase()))
        );

        const card = createCardElement(m, data);
        
        if (isMyTask) {
            myGrid.appendChild(card);
            myCount++;
        } else {
            otherGrid.appendChild(card);
            otherCount++;
        }
    });

    document.getElementById('myCount').textContent = myCount;
    document.getElementById('otherCount').textContent = otherCount;
};

const createCardElement = (m, data) => {
    const card = document.createElement('div');
    card.className = `card ${m.source} ${m.done ? 'done' : ''}`;
    
    let linkHtml = '';
    if (m.link) {
        linkHtml = `<a href="${m.link}" target="_blank" class="link-btn" style="margin-top: 0;">${data.viewOriginal}</a>`;
    }

    card.innerHTML = `
        <div class="col-source"><span class="badge">${m.source}</span></div>
        <div class="col-room meta-val">${m.room || '-'}</div>
        <div class="col-task task-title" title="${m.task}">${m.task}</div>
        <div class="col-requester meta-val">${m.requester}</div>
        <div class="col-assignee meta-val">${m.assignee}</div>
        <div class="col-time meta-val" style="font-size: 0.75rem;">${m.assigned_at}</div>
        <div class="col-actions">
            ${linkHtml}
            <button class="done-btn" onclick="toggleDone(${m.id}, ${!m.done})">
                ${m.done ? data.doneBtn : data.markDone}
            </button>
            <button class="delete-btn" onclick="deleteTask(${m.id})" style="background: rgba(255, 59, 48, 0.1); color: #ff3b30; border-color: #ff3b30; padding: 0.3rem 0.6rem; border-radius: 8px; border: 1px solid; cursor: pointer; font-size: 0.7rem;">
                🗑️
            </button>
        </div>
    `;
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
    document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            console.log("Tab clicked:", btn.getAttribute('data-tab'));
            document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
            document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
            
            btn.classList.add('active');
            const tabId = btn.getAttribute('data-tab');
            const targetTab = document.getElementById(`${tabId}TasksTab`);
            if (targetTab) {
                targetTab.classList.add('active');
                console.log("Active tab set to:", targetTab.id);
            } else {
                console.error("Target tab not found for ID:", tabId);
            }
        });
    });

    // Archive Logic
    const archiveLink = document.getElementById('archiveLink');
    if (archiveLink) {
        archiveLink.addEventListener('click', (e) => {
            e.preventDefault();
            document.querySelector('.layout-split').classList.add('hidden');
            document.querySelector('.dashboard-header').classList.add('hidden');
            document.getElementById('archiveSection').classList.remove('hidden');
            fetchArchive();
        });
    }

    const closeArchiveBtn = document.getElementById('closeArchiveBtn');
    if (closeArchiveBtn) {
        closeArchiveBtn.addEventListener('click', () => {
            document.querySelector('.layout-split').classList.remove('hidden');
            document.querySelector('.dashboard-header').classList.remove('hidden');
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

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
        qrError: "QR 발급 실패: "
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
        qrError: "QR Generation Failed: "
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
        qrError: "Gagal Menghasilkan QR: "
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
        qrError: "การสร้าง QR ล้มเหลว: "
    }
};

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

    // Refresh messages to update card buttons
    fetchMessages();
};

const fetchMessages = async () => {
    try {
        const resp = await fetch('/api/messages');
        if (!resp.ok) throw new Error("Failed to fetch");
        const data = await resp.json();
        renderMessages(data);
    } catch (e) {
        console.error(e);
    }
};

const renderMessages = (messages) => {
    const myGrid = document.getElementById('myTasksGrid');
    const otherGrid = document.getElementById('otherTasksGrid');
    myGrid.innerHTML = '';
    otherGrid.innerHTML = '';

    const data = I18N_DATA[currentLang];
    const keywords = ["송재진", "jj", "jjsong", "jaejin song"];

    if (!messages || messages.length === 0) {
        myGrid.innerHTML = `<p style="text-align: center; color: var(--text-dim); margin-top: 1rem;">${data.noTasks}</p>`;
        otherGrid.innerHTML = `<p style="text-align: center; color: var(--text-dim); margin-top: 1rem;">${data.noTasks}</p>`;
        document.getElementById('myCount').textContent = '0';
        document.getElementById('otherCount').textContent = '0';
        return;
    }

    const sorted = [...messages].sort((a, b) => {
        if (a.done === b.done) {
            return new Date(b.created_at) - new Date(a.created_at);
        }
        return a.done ? 1 : -1;
    });

    let myCount = 0;
    let otherCount = 0;

    sorted.forEach(m => {
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
        linkHtml = `<a href="${m.link}" target="_blank" class="link-btn">${data.viewOriginal}</a>`;
    }

    card.innerHTML = `
        <div class="task-title">${m.task}</div>
        <div class="meta-row">
            <div><span class="tag">ROOM:</span> ${m.room || 'Unknown'}</div>
            <div><span class="tag">FROM:</span> ${m.requester}</div>
            <div><span class="tag">TO:</span> ${m.assignee}</div>
            <div><span class="tag">TIME:</span> ${m.assigned_at}</div>
        </div>
        <div class="btn-group">
            ${linkHtml}
            <button class="done-btn" onclick="toggleDone(${m.id}, ${!m.done})">
                ${m.done ? data.doneBtn : data.markDone}
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

let waConnected = false;

const checkWhatsAppStatus = async () => {
    try {
        const resp = await fetch('/api/whatsapp/status');
        const data = await resp.json();
        const waStatusBadge = document.getElementById('waStatus');
        const waLoginSection = document.getElementById('waLoginSection');
        const i18n = I18N_DATA[currentLang];

        if (data.status === 'connected') {
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

document.getElementById('getQRBtn').addEventListener('click', async () => {
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

document.getElementById('scanBtn').addEventListener('click', triggerScan);

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
document.getElementById('languageSelect').addEventListener('change', async (e) => {
    currentLang = e.target.value;
    localStorage.setItem('mc_lang', currentLang);
    updateUILanguage(currentLang);
    await translateExistingTasks(currentLang);
});

// Set initial value
document.getElementById('languageSelect').value = currentLang;
updateUILanguage(currentLang);

// Initial check
checkWhatsAppStatus();
setInterval(checkWhatsAppStatus, 30000);

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

document.getElementById('archiveLink').addEventListener('click', (e) => {
    e.preventDefault();
    document.querySelector('.layout-split').classList.add('hidden');
    document.querySelector('.dashboard-header').classList.add('hidden');
    document.getElementById('archiveSection').classList.remove('hidden');
    fetchArchive();
});

document.getElementById('closeArchiveBtn').addEventListener('click', () => {
    document.querySelector('.layout-split').classList.remove('hidden');
    document.querySelector('.dashboard-header').classList.remove('hidden');
    document.getElementById('archiveSection').classList.add('hidden');
});

document.getElementById('exportCsvBtn').addEventListener('click', () => {
    window.location.href = '/api/messages/archive/export';
});

// Initial load
fetchMessages();
setInterval(fetchMessages, 30000);

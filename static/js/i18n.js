import { state } from './state.js';

export const I18N_DATA = {
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
        logout: "로그아웃",
        statusOn: "연결됨",
        statusOff: "연결 안됨"
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
        logout: "Logout",
        statusOn: "ON",
        statusOff: "OFF"
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
        logout: "Keluar",
        statusOn: "AKTIF",
        statusOff: "MATI"
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
        logout: "ออกจากระบบ",
        statusOn: "เปิด",
        statusOff: "ปิด"
    }
};

export const updateUILanguage = (lang) => {
    const data = I18N_DATA[lang];
    if (!data) return;

    document.getElementById('subTitle').textContent = data.subTitle;
    
    // Update Scan Button
    const scanBtnText = document.getElementById('scanBtnText');
    if (scanBtnText) {
        // Use a static 'SCAN' or a short i18n string if preferred, 
        // but the user wants it professional like Slack/WhatsApp.
        scanBtnText.textContent = lang === 'ko' ? '스캔' : 'SCAN';
    }

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

    // Refresh status text
    const slackText = document.getElementById('slackStatusText');
    const waText = document.getElementById('waStatusText');
    const slackIcon = document.getElementById('slackStatusLarge');
    const waIcon = document.getElementById('waStatusLarge');

    if (slackText && slackIcon) {
        slackText.textContent = slackIcon.classList.contains('active') ? data.statusOn : data.statusOff;
    }
    if (waText && waIcon) {
        waText.textContent = waIcon.classList.contains('active') ? data.statusOn : data.statusOff;
    }
};

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
        viewOriginal: "원문",
        markDone: "완료",
        doneBtn: "✓ 완료",
        qrError: "QR 발급 실패: ",
        myTasks: "내 업무",
        otherTasks: "기타 업무",
        allTasks: "전체",
        logout: "로그아웃",
        statusOn: "연결됨",
        statusOff: "연결 안됨",
        hSource: "채널",
        hRoom: "룸",
        hTask: "업무 내역",
        hRequester: "요청자",
        hAssignee: "담당자",
        hTime: "시간",
        hActions: "관리",
        hCompletedAt: "완료 시간",
        settingsTitle: "사용자 설정",
        settingsAliasTitle: "내 별칭 설정",
        settingsAliasDesc: "자신을 지칭하는 이름이나 별명(예: '송재진', 'JJ')을 추가하세요. 이 이름이 포함된 업무는 '내 업무' 탭에 표시됩니다.",
        originalMessageTitle: "메시지 원문",
        archiveTitle: "보관함",
        confirmDelete: "정말 이 업무를 삭제하시겠습니까? 삭제된 업무는 보관함으로 이동합니다."
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
        viewOriginal: "Original",
        markDone: "Done",
        doneBtn: "✓ Done",
        qrError: "QR Generation Failed: ",
        myTasks: "My Tasks",
        otherTasks: "Other Tasks",
        allTasks: "All Tasks",
        logout: "Logout",
        statusOn: "ON",
        statusOff: "OFF",
        hSource: "Source",
        hRoom: "Room",
        hTask: "Task",
        hRequester: "Requester",
        hAssignee: "Assignee",
        hTime: "Time",
        hActions: "Actions",
        hCompletedAt: "Completed At",
        settingsTitle: "User Settings",
        settingsAliasTitle: "My Aliases",
        settingsAliasDesc: "Add names or nicknames that refer to you (e.g., 'JJ'). Tasks with these names will appear in 'My Tasks'.",
        originalMessageTitle: "Original Message",
        archiveTitle: "Archive",
        confirmDelete: "Are you sure you want to delete this task? It will be moved to the archive."
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
        viewOriginal: "Asli",
        markDone: "Selesai",
        doneBtn: "✓ Selesai",
        qrError: "Gagal Menghasilkan QR: ",
        myTasks: "Tugas Saya",
        otherTasks: "Tugas Lainnya",
        allTasks: "Semua Tugas",
        logout: "Keluar",
        statusOn: "AKTIF",
        statusOff: "MATI",
        hSource: "Sumber",
        hRoom: "Ruangan",
        hTask: "Tugas",
        hRequester: "Pemohon",
        hAssignee: "Penerima",
        hTime: "Waktu",
        hActions: "Tindakan",
        hCompletedAt: "Selesai Pada",
        settingsTitle: "Pengaturan Pengguna",
        settingsAliasTitle: "Alias Saya",
        settingsAliasDesc: "Tambahkan nama atau nama panggilan yang merujuk pada Anda. Tugas dengan nama-nama ini akan muncul di 'Tugas Saya'.",
        originalMessageTitle: "Pesan Asli",
        archiveTitle: "Arsip",
        confirmDelete: "Apakah Anda yakin ingin menghapus tugas ini? Tugas akan dipindahkan ke arsip."
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
        viewOriginal: "ต้นฉบับ",
        markDone: "เสร็จสิ้น",
        doneBtn: "✓ เสร็จสิ้น",
        qrError: "การสร้าง QR ล้มเหลว: ",
        myTasks: "งานของฉัน",
        otherTasks: "งานอื่นๆ",
        allTasks: "งานทั้งหมด",
        logout: "ออกจากระบบ",
        statusOn: "เปิด",
        statusOff: "ปิด",
        hSource: "แหล่งที่มา",
        hRoom: "ห้อง",
        hTask: "งาน",
        hRequester: "ผู้ร้องขอ",
        hAssignee: "ผู้รับผิดชอบ",
        hTime: "เวลา",
        hActions: "การดำเนินการ",
        hCompletedAt: "เสร็จสิ้นเมื่อ",
        settingsTitle: "การตั้งค่าผู้ใช้",
        settingsAliasTitle: "นามแฝงของฉัน",
        settingsAliasDesc: "เพิ่มชื่อหรือชื่อเล่นที่อ้างถึงคุณ งานที่มีชื่อเหล่านี้จะปรากฏใน 'งานของฉัน'",
        originalMessageTitle: "ข้อความต้นฉบับ",
        archiveTitle: "คลังข้อมูล",
        confirmDelete: "คุณแน่ใจหรือไม่ว่าต้องการลบงานนี้? งานจะถูกย้ายไปยังคลังข้อมูล"
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

    // Update Table Headers
    ['hSource', 'hRoom', 'hTask', 'hRequester', 'hAssignee', 'hTime', 'hActions', 'ahSource', 'ahRoom', 'ahTask', 'ahRequester', 'ahAssignee', 'ahTime', 'ahCompletedAt'].forEach(id => {
        const el = document.getElementById(id);
        if (el) el.textContent = data[id.replace('ah', 'h')] || data[id];
    });

    // Update Modal/Section Titles
    const settingsTitle = document.querySelector('#settingsModal h3');
    if (settingsTitle) settingsTitle.textContent = data.settingsTitle;
    
    const settingsAliasTitle = document.getElementById('settingsAliasTitle');
    if (settingsAliasTitle) settingsAliasTitle.textContent = data.settingsAliasTitle;
    
    const settingsAliasDesc = document.getElementById('settingsAliasDesc');
    if (settingsAliasDesc) settingsAliasDesc.textContent = data.settingsAliasDesc;

    const originalMessageTitle = document.querySelector('#originalMessageModal h3');
    if (originalMessageTitle) originalMessageTitle.textContent = data.originalMessageTitle;

    const archiveTitle = document.querySelector('#archiveSection h2');
    if (archiveTitle) {
        const archiveCount = document.getElementById('archiveCount');
        const countText = archiveCount ? archiveCount.outerHTML : '';
        archiveTitle.innerHTML = `${data.archiveTitle} ${countText}`;
    }

    // Update Tab Spans (labels only)
    const tabMy = document.getElementById('tabMy');
    const tabOther = document.getElementById('tabOther');
    const tabAll = document.getElementById('tabAll');
    if (tabMy) tabMy.textContent = data.myTasks;
    if (tabOther) tabOther.textContent = data.otherTasks;
    if (tabAll) tabAll.textContent = data.allTasks;
};

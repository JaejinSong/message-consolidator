export const insights = {
    init() {
        console.log("[Insights] Initialized");
        // Initial render for Dashboard Summary Bar if data is available
        this.updateDashboardSummary();
    },

    onShow() {
        console.log("[Insights] View Shown");
        this.renderAll();
    },

    renderAll() {
        this.renderDailyGlance();
        this.renderActivityHeatmap();
        this.renderSourceDistribution();
        this.renderWaitingMetrics();
    },

    /**
     * Dashboard에 표시되는 요약 바 업데이트
     */
    updateDashboardSummary() {
        const container = document.getElementById('dailySummaryBar');
        if (!container) return;

        // Skeleton Data (In real app, this comes from state or API)
        const summaryData = {
            todayTasks: 12,
            completedTasks: 5,
            myTasks: 3,
            urgentTasks: 1
        };

        container.innerHTML = `
            <div class="daily-summary-item">
                <span class="label">Today's Scan</span>
                <span class="value">${summaryData.todayTasks}</span>
            </div>
            <div class="daily-summary-item">
                <span class="label">Completed</span>
                <span class="value accent">${summaryData.completedTasks}</span>
            </div>
            <div class="daily-summary-item">
                <span class="label">My Tasks</span>
                <span class="value">${summaryData.myTasks}</span>
            </div>
            <div class="daily-summary-item">
                <span class="label">Urgent</span>
                <span class="value" style="color: #ff3b30;">${summaryData.urgentTasks}</span>
            </div>
        `;
        container.classList.remove('hidden');
    },

    /**
     * Insights 탭의 'Daily Glance' (AI 요약) 렌더링
     */
    renderDailyGlance() {
        const content = document.getElementById('wittySummaryContent');
        if (!content) return;

        // Witty message based on skeleton data
        content.innerHTML = `
            <p>어제보다 <span class="accent">15%</span> 더 많은 업무를 처리하셨네요! 
            오늘 오전에는 Slack에서 요청된 3개의 긴급 건을 먼저 처리하는 것을 추천드립니다. 
            오후 3시 이후에는 업무량이 줄어드니 집중이 필요한 작업을 배치해보세요. ✨</p>
        `;
    },

    /**
     * Activity Heatmap (최근 30일) 렌더링
     */
    renderActivityHeatmap() {
        const container = document.getElementById('activityHeatmap');
        if (!container) return;

        let html = '<div class="heatmap-grid">';
        const today = new Date();
        
        // 최근 30일 데이터 생성
        for (let i = 29; i >= 0; i--) {
            const d = new Date(today);
            d.setDate(d.getDate() - i);
            const dateStr = d.toISOString().split('T')[0];
            
            // Random level for skeleton
            const level = Math.floor(Math.random() * 5); 
            const taskCount = level * 2;
            
            html += `<div class="heatmap-day" data-level="${level}" data-tooltip="${taskCount} tasks (${dateStr})"></div>`;
        }
        
        html += '</div>';
        container.innerHTML = html;
    },

    /**
     * Channel Distribution (Stacked Bar) 렌더링
     */
    renderSourceDistribution() {
        const container = document.getElementById('sourceDistribution');
        if (!container) return;

        // Skeleton distribution
        const dist = { slack: 45, whatsapp: 35, gmail: 20 };

        container.innerHTML = `
            <div class="stacked-bar-container">
                <div class="stacked-bar-segment slack" style="width: ${dist.slack}%;" data-tooltip="Slack: ${dist.slack}%">Slack</div>
                <div class="stacked-bar-segment whatsapp" style="width: ${dist.whatsapp}%;" data-tooltip="WhatsApp: ${dist.whatsapp}%">WA</div>
                <div class="stacked-bar-segment gmail" style="width: ${dist.gmail}%;" data-tooltip="Gmail: ${dist.gmail}%">Gmail</div>
            </div>
            <div class="distribution-legend" style="display: flex; justify-content: space-between; font-size: 0.75rem; color: var(--text-dim);">
                <span>Slack (${dist.slack}%)</span>
                <span>WhatsApp (${dist.whatsapp}%)</span>
                <span>Gmail (${dist.gmail}%)</span>
            </div>
        `;
    },

    /**
     * Waiting On 지표 렌더링
     */
    renderWaitingMetrics() {
        const meCount = document.getElementById('waitingOnMeCount');
        const othersCount = document.getElementById('waitingOnOthersCount');

        if (meCount) meCount.textContent = "4";
        if (othersCount) othersCount.textContent = "2";
    }
};
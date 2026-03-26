export const state = {
    userProfile: { email: "", picture: "", name: "", points: 0, streak: 0, streak_freezes: 0 },
    userAliases: [],
    currentLang: localStorage.getItem('mc_lang') || 'ko',
    currentTheme: localStorage.getItem('mc_theme') || 'dark',
    waConnected: false,
    gmailConnected: false,
    // Archive state
    archivePage: 1,
    archiveLimit: 20,
    archiveSearch: "",
    archiveSort: '',
    archiveOrder: 'DESC',
    archiveTotalCount: 0,
    archiveThresholdDays: 7 // Default
};

export const updateLang = (lang) => {
    state.currentLang = lang;
    localStorage.setItem('mc_lang', lang);
};

export const updateTheme = (theme) => {
    state.currentTheme = theme;
    localStorage.setItem('mc_theme', theme);
};

export const updateStats = (user) => {
    if (!user) return;
    if (user.archive_days) {
        state.archiveThresholdDays = user.archive_days;
    }
    state.userProfile = { ...state.userProfile, ...user };
};

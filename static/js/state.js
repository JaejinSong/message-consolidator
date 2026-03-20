export const state = {
    userProfile: { email: "", picture: "", name: "" },
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
    archiveTotalCount: 0
};

export const updateLang = (lang) => {
    state.currentLang = lang;
    localStorage.setItem('mc_lang', lang);
};

export const updateTheme = (theme) => {
    state.currentTheme = theme;
    localStorage.setItem('mc_theme', theme);
};

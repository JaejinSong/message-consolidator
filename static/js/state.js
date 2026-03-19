export const state = {
    userProfile: { email: "", picture: "", name: "" },
    userAliases: [],
    currentLang: localStorage.getItem('mc_lang') || 'ko',
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

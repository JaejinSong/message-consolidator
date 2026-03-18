export const state = {
    userProfile: { email: "", picture: "", name: "" },
    userAliases: [],
    currentLang: localStorage.getItem('mc_lang') || 'ko',
    waConnected: false,
    gmailConnected: false
};

export const updateLang = (lang) => {
    state.currentLang = lang;
    localStorage.setItem('mc_lang', lang);
};

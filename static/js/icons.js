export const ICONS = {
    viewOriginal: `
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="width: 16px; height: 16px;">
            <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
            <circle cx="12" cy="12" r="3"></circle>
        </svg>`,
    link: `
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="width: 16px; height: 16px;">
            <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"></path>
            <polyline points="15 3 21 3 21 9"></polyline>
            <line x1="10" y1="14" x2="21" y2="3"></line>
        </svg>`,
    done: `
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" style="width: 18px; height: 18px;">
            <polyline points="20 6 9 17 4 12"></polyline>
        </svg>`,
    delete: `
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" style="width: 16px; height: 16px;">
            <polyline points="3 6 5 6 21 6"></polyline>
            <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
        </svg>`,
    slack: `<svg viewBox="0 0 100 100" style="width: 20px; height: 20px;">
            <path fill="#E01E5A" d="M22.9,53.8V42.3c0-3.2-2.6-5.8-5.8-5.8s-5.8,2.6-5.8,5.8v11.5c0,3.2,2.6,5.8,5.8,5.8S22.9,57,22.9,53.8z"/><path fill="#E01E5A" d="M28.6,42.3c0-3.2,2.6-5.8,5.8-5.8h11.5c3.2,0,5.8,2.6,5.8,5.8s-2.6,5.8-5.8,5.8H34.4C31.2,48.1,28.6,45.5,28.6,42.3z"/><path fill="#36C5F0" d="M46.2,22.9h11.5c3.2,0,5.8-2.6,5.8-5.8s-2.6-5.8-5.8-5.8H46.2c-3.2,0-5.8,2.6-5.8,5.8S43,22.9,46.2,22.9z"/><path fill="#36C5F0" d="M57.7,28.6c3.2,0,5.8,2.6,5.8,5.8v11.5c0,3.2-2.6,5.8-5.8,5.8s-5.8-2.6-5.8-5.8V34.4C51.9,31.2,54.5,28.6,57.7,28.6z"/><path fill="#2EB67D" d="M77.1,46.2v11.5c0,3.2,2.6,5.8,5.8,5.8s5.8-2.6,5.8-5.8V46.2c0-3.2-2.6-5.8-5.8-5.8S77.1,43,77.1,46.2z"/><path fill="#2EB67D" d="M71.4,57.7c0,3.2-2.6,5.8-5.8,5.8H54.1c-3.2,0-5.8-2.6-5.8-5.8s2.6-5.8,5.8-5.8h11.5C68.8,51.9,71.4,54.5,71.4,57.7z"/><path fill="#ECB22E" d="M53.8,77.1H42.3c-3.2,0-5.8,2.6-5.8,5.8s2.6,5.8,5.8,5.8h11.5c3.2,0,5.8-2.6,5.8-5.8S57,77.1,53.8,77.1z"/><path fill="#ECB22E" d="M42.3,71.4c-3.2,0-5.8-2.6-5.8-5.8V54.1c0-3.2,2.6-5.8,5.8-5.8c3.2,0,5.8,2.6,5.8,5.8v11.5C48.1,68.8,45.5,71.4,42.3,71.4z"/>
        </svg>`,
    whatsapp: `<svg viewBox="0 0 448 512" style="width: 20px; height: 20px; fill: #25d366;">
            <path d="M380.9 97.1C339 55.1 283.2 32 223.9 32c-122.4 0-222 99.6-222 222 0 39.1 10.2 77.3 29.6 111L0 480l117.7-30.9c32.4 17.7 68.9 27 106.1 27h.1c122.3 0 224.1-99.6 224.1-222 0-59.3-25.2-115-67.1-157zm-157 341.6c-33.2 0-65.7-8.9-94-25.7l-6.7-4-69.8 18.3L72 359.2l-4.4-7c-18.5-29.4-28.2-63.3-28.2-98.2 0-101.7 82.8-184.5 184.6-184.5 49.3 0 95.6 19.2 130.4 54.1 34.8 34.9 56.2 81.2 56.1 130.5 0 101.8-84.9 184.6-186.6 184.6zm101.2-138.2c-5.5-2.8-32.8-16.2-37.9-18-5.1-1.9-8.8-2.8-12.5 2.8-3.7 5.6-14.3 18-17.6 21.8-3.2 3.7-6.5 4.2-12 1.4-5.5-2.8-23.2-8.5-44.2-27.1-16.4-14.6-27.4-32.7-30.6-38.2-3.2-5.6-.3-8.6 2.4-11.3 2.5-2.4 5.5-6.5 8.3-9.7 2.8-3.3 3.7-5.6 5.6-9.3 1.8-3.7.9-6.9-.5-9.7-1.4-2.8-12.5-30.1-17.1-41.2-4.5-10.8-9.1-9.3-12.5-9.5-3.2-.2-6.9-.2-10.6-.2-3.7 0-9.7 1.4-14.8 6.9-5.1 5.6-19.4 19-19.4 46.3 0 27.3 19.9 53.7 22.6 57.4 2.8 3.7 39.1 59.7 94.8 83.8 13.2 5.7 23.5 9.2 31.6 11.8 13.3 4.2 25.4 3.6 35 2.2 10.7-1.6 32.8-13.4 37.4-26.4 4.6-13 4.6-24.1 3.2-26.4-1.3-2.5-5-3.9-10.5-6.6z"/>
        </svg>`,
    gmail: `<svg viewBox="0 0 512 512" style="width: 20px; height: 20px;">
            <path fill="#EA4335" d="M48 64C21.5 64 0 85.5 0 112v288c0 26.5 21.5 48 48 48h416c26.5 0 48-21.5 48-48V112c0-26.5-21.5-48-48-48H48zM48 96h416c8.8 0 16 7.2 16 16v21.3L256 295.1 32 133.3V112c0-8.8 7.2-16 16-16zm-16 70.6l208 147.3L448 166.6V400c0 8.8-7.2 16-16 16H80c-8.8 0-16-7.2-16-16V166.6z"/>
        </svg>`,
    stale: `
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="width: 14px; height: 14px; vertical-align: middle; margin-right: 2px;">
            <circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/><path d="M12 2a10 10 0 0 1 10 10" opacity="0.3"/>
        </svg>`,
    abandoned: `
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="width: 14px; height: 14px; vertical-align: middle; margin-right: 2px;">
            <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>
        </svg>`
};
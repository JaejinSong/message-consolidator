/**
 * @file authService.ts
 * @description Google (Gmail) OAuth service for connection, status check, and disconnection.
 */

import { apiFetch, BASE_URL } from '../utils/http';

export const authService = {
    /**
     * Checks if Gmail is currently connected for the authenticated user.
     * @returns {Promise<{connected: boolean, email?: string}>}
     */
    async checkGmailStatus(): Promise<{ connected: boolean; email?: string }> {
        try {
            return await apiFetch('/gmail/status');
        } catch (error) {
            console.error('[AuthService] checkGmailStatus failed:', error);
            return { connected: false };
        }
    },

    /**
     * Disconnects the Gmail account for the authenticated user.
     * @returns {Promise<boolean>}
     */
    async disconnectGmail(): Promise<boolean> {
        try {
            await apiFetch('/gmail/disconnect', { method: 'POST' });
            return true;
        } catch (error) {
            console.error('[AuthService] disconnectGmail failed:', error);
            return false;
        }
    },

    /**
     * Initiates the Google OAuth flow by redirecting the browser.
     * Fetch API must NOT be used here.
     */
    connectGmail(): void {
        // Why: http.ts logic takes care of /auth routes mapping to root.
        // For window.location.href, we need an absolute URL.
        const url = BASE_URL.startsWith('http') ? new URL(BASE_URL) : { origin: window.location.origin };
        window.location.href = `${url.origin}/auth/gmail/connect`;
    }
};

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { state, updateLang, updateTheme, updateStats } from './state.ts';

describe('state', () => {
    beforeEach(() => {
        vi.stubGlobal('localStorage', {
            getItem: vi.fn(),
            setItem: vi.fn(),
        });
    });

    it('should update currentLang and reach localStorage', () => {
        updateLang('en');
        expect(state.currentLang).toBe('en');
        expect(localStorage.setItem).toHaveBeenCalledWith('mc_lang', 'en');
    });

    it('should update currentTheme and reach localStorage', () => {
        updateTheme('light');
        expect(state.currentTheme).toBe('light');
        expect(localStorage.setItem).toHaveBeenCalledWith('mc_theme', 'light');
    });

    it('should update stats and merge with existing profile', () => {
        const initialProfile = { ...state.userProfile };
        const newStats = { points: 100, streak: 5 };
        updateStats(newStats);

        expect(state.userProfile.points).toBe(100);
        expect(state.userProfile.streak).toBe(5);
        expect(state.userProfile.email).toBe(initialProfile.email);
    });

    it('should not update stats if user data is null', () => {
        const profileBefore = { ...state.userProfile };
        updateStats(null);
        expect(state.userProfile).toEqual(profileBefore);
    });
});

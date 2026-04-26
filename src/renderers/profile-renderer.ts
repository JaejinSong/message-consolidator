import { UserProfile } from '../types';

/**
 * @file profile-renderer.ts
 * @description UI renderer for user profile, stats, and gamification elements.
 */

/**
 * Updates user profile UI with latest data.
 */
export function updateUserProfile(profile: UserProfile | null): void {
    if (!profile) return;

    const userProfile = document.getElementById('userProfile');
    const gamificationStats = document.getElementById('gamificationStats');
    if (userProfile) userProfile.classList.remove('hidden');
    //Why: Hides the gamification container entirely as the feature is removed.
    if (gamificationStats) gamificationStats.classList.add('hidden');

    const userEmail = document.getElementById('userEmail');
    const userPic = document.getElementById('userPicture') as HTMLImageElement | null;

    if (userEmail) {
        userEmail.textContent = profile.email || '';
        userEmail.classList.remove('hidden');
    }

    if (userPic) {
        if (profile.picture) {
            userPic.src = profile.picture;
            userPic.classList.remove('hidden');
        } else {
            userPic.classList.add('hidden');
        }
    }

    toggleAdminVisibility(Boolean(profile.is_admin || profile.is_super_admin));
}

// Why: super admin or delegated admin sees the Admin tab; everyone else stays unaware.
// Toggling on the button alone is enough — clicking activates the panel via setupTabs.
function toggleAdminVisibility(showAdmin: boolean): void {
    const adminTabBtn = document.querySelector<HTMLElement>('[data-settings-tab="adminTab"]');
    if (!adminTabBtn) return;
    adminTabBtn.classList.toggle('hidden', !showAdmin);
}

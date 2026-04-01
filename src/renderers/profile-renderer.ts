import { UserProfile } from '../state.ts';

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
    if (gamificationStats) gamificationStats.classList.remove('hidden');

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

    const streakText = document.getElementById('userStreak');
    const xpText = document.getElementById('xpText');
    const xpBar = document.getElementById('xpBar') as HTMLElement | null;
    const pointsText = document.getElementById('userPoints');
    const levelText = document.getElementById('userLevel');

    if (streakText) streakText.textContent = `${profile.streak || 0}🔥`;
    
    const currentXp = (profile.xp || 0) % 100;
    if (xpText) xpText.textContent = `${currentXp} / 100 XP`;
    if (xpBar) {
        xpBar.style.width = `${currentXp}%`;
    }
    if (pointsText) pointsText.textContent = String(profile.points || 0);
    if (levelText) levelText.textContent = String(profile.level || 1);

    const freezeContainer = document.getElementById('streakFreezeContainer');
    if (freezeContainer) {
        const count = profile.streak_freezes || 0;
        let html = `<span class="freeze-badge" title="Streak Freeze">❄️ × ${count}</span>`;

        if (profile.points >= 50) {
            html += `<button class="buy-freeze-btn" id="buyFreezeBtn">+ ❄️ (50 SCORE)</button>`;
        }
        freezeContainer.innerHTML = html;
    }
}

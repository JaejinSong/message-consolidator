/**
 * Updates user profile UI with latest data.
 */
export function updateUserProfile(profile) {
    if (!profile) return;

    const userProfile = document.getElementById('userProfile');
    const gamificationStats = document.getElementById('gamificationStats');
    if (userProfile) userProfile.classList.remove('hidden');
    if (gamificationStats) gamificationStats.classList.remove('hidden');

    const userEmail = document.getElementById('userEmail');
    const userPic = document.getElementById('userPicture');

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
    const xpBar = document.getElementById('xpBar');
    const pointsText = document.getElementById('userPoints');
    const levelText = document.getElementById('userLevel');

    if (streakText) streakText.textContent = `${profile.streak || 0}🔥`;
    if (xpText) xpText.textContent = `${(profile.xp || 0) % 100} / 100 XP`;
    if (xpBar) {
        const progress = (profile.xp || 0) % 100;
        xpBar.style.width = `${progress}%`;
    }
    if (pointsText) pointsText.textContent = profile.points || 0;
    if (levelText) levelText.textContent = profile.level || 1;

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

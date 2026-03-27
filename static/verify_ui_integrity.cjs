const fs = require('fs');
const path = require('path');

const BASE_DIR = '/home/jinro/.gemini/message-consolidator/static';

function checkFile(filePath, requiredStrings) {
    console.log(`Checking ${filePath}...`);
    const content = fs.readFileSync(path.join(BASE_DIR, filePath), 'utf8');
    let missing = false;
    requiredStrings.forEach(str => {
        if (!content.includes(str)) {
            console.error(`  [ERROR] Missing: ${str}`);
            missing = true;
        } else {
            console.log(`  [OK] Found: ${str}`);
        }
    });
    return !missing;
}

const htmlChecks = [
    'id="userProfile"',
    'id="userEmail"',
    'id="userPicture"',
    'id="gamificationStats"',
    'data-i18n="levelLabel"',
    'data-i18n="streakLabel"',
    'data-i18n="pointsLabel"',
    'id="userLevel"',
    'id="userStreak"',
    'id="userPoints"'
];

const rendererChecks = [
	"userEmail.classList.remove('hidden')",
	"userProfile.classList.remove('hidden')",
	"gamificationStats.classList.remove('hidden')",
	"profile.streak || 0",
	"profile.points || 0"
];

const i18nChecks = [
	'.tab-btn[data-tab=',
	'.c-main-nav__item[data-tab=',
	'dashboardTitle',
	'archiveTitle',
	'insightsTitle'
];

let allOk = true;
allOk &= checkFile('index.html', htmlChecks);
allOk &= checkFile('js/renderer.js', rendererChecks);
allOk &= checkFile('js/i18n.js', i18nChecks);

if (allOk) {
    console.log('\n[SUCCESS] UI Integrity and I18n Mapping Verified!');
    process.exit(0);
} else {
    console.error('\n[FAILURE] Some verification checks failed.');
    process.exit(1);
}

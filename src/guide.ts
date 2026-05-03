import { GUIDE_SECTIONS, GUIDE_CONTENT, GuideSection } from './guide-content';
import { parseMarkdown } from './logic';

function renderSection(key: GuideSection): void {
	const guideContent = document.getElementById('guideContent');
	if (!guideContent) return;
	guideContent.innerHTML = parseMarkdown(GUIDE_CONTENT[key]);
	const panel = document.querySelector('.c-guide__panel') as HTMLElement | null;
	if (panel) panel.scrollTop = 0;
}

export const guide = {
	init(): void {
		const guideSection = document.getElementById('guideSection');
		if (!guideSection) return;

		const buttons = guideSection.querySelectorAll('.c-guide__sidebar-btn');
		buttons.forEach(btn => {
			btn.addEventListener('click', (e) => {
				const target = e.currentTarget as HTMLElement;
				const key = target.getAttribute('data-section') as GuideSection;
				if (!key) return;
				buttons.forEach(b => b.classList.remove('c-guide__sidebar-btn--active'));
				target.classList.add('c-guide__sidebar-btn--active');
				renderSection(key);
			});
		});
	},

	onShow(): void {
		const guideContent = document.getElementById('guideContent');
		if (guideContent && !guideContent.innerHTML.trim()) {
			renderSection('getting-started');
		}
	},
};

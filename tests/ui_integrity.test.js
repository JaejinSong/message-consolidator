import { describe, it, expect, beforeAll } from 'vitest';
import { Window } from 'happy-dom';
import fs from 'fs';
import path from 'path';

describe('Message Consolidator - Dashboard UI Integrity', () => {
    let document;
    let htmlPath = path.resolve(process.cwd(), 'static/index.html');
    let cssPath = path.resolve(process.cwd(), 'static/css/v2-components.css');
    let variablesPath = path.resolve(process.cwd(), 'static/css/variables.css');

    beforeAll(async () => {
        const html = fs.readFileSync(htmlPath, 'utf8');
        const window = new Window();
        document = window.document;
        document.write(html);
    });

    it('should have all main view buttons (Dashboard, Archive, Insights)', () => {
        const viewButtons = ['dashViewBtn', 'archiveViewBtn', 'insightsViewBtn', 'settingsBtn'];
        viewButtons.forEach(id => {
            const btn = document.getElementById(id);
            expect(btn, `Element with ID ${id} should exist`).to.not.be.null;
            const validClasses = ['c-main-nav__item', 'c-user-profile__action', 'c-utility__btn'];
            const hasValidClass = validClasses.some(cls => btn.classList.contains(cls));
            expect(hasValidClass, `Button ${id} should have one of ${validClasses.join(', ')}`).to.be.true;
        });
    });

    it('Archive view should use BEM class and contain standardized sub-tabs', () => {
        const archiveSection = document.getElementById('archiveSection');
        expect(archiveSection.classList.contains('glass-card')).to.be.true;

        const subTabs = archiveSection.querySelectorAll('.c-tabs__btn');
        expect(subTabs.length).to.be.at.least(3); // All, Done, Trash
    });

    it('Search box in archive should use .c-input--pill class and have no inline styles (except display)', () => {
        const searchInput = document.getElementById('archiveSearchInput');
        expect(searchInput.classList.contains('c-input')).to.be.true;
        expect(searchInput.classList.contains('c-input--pill')).to.be.true;
        
        const style = searchInput.getAttribute('style') || "";
        // Should not have background or padding hardcoded
        expect(style).to.not.contain('background:');
        expect(style).to.not.contain('padding:');
    });

    it('Table header components should be present in Archive', () => {
        const table = document.querySelector('.c-archive-table');
        expect(table).to.not.be.null;
        
        const headers = ['ahSource', 'ahRoom', 'ahTask', 'ahRequester', 'ahAssignee', 'ahTime'];
        headers.forEach(id => {
            expect(document.getElementById(id)).to.not.be.null;
        });
    });

    it('should not have widespread hardcoded styles in main dashboard containers', () => {
        const mainContainers = ['dashboardContent', 'insightsSection', 'archiveSection'];
        mainContainers.forEach(id => {
            const el = document.getElementById(id);
            if(el) {
                const style = el.getAttribute('style') || "";
                // Verify that we are not hardcoding border-radius or colors directly
                expect(style).to.not.contain('#');
                expect(style).to.not.contain('rgb(');
            }
        });
    });

    it('variables.css must contain RGBA semantic variables', () => {
        const content = fs.readFileSync(variablesPath, 'utf8');
        const requiredVars = [
            '--color-primary-rgb',
            '--color-success-rgb',
            '--color-warning-rgb',
            '--color-error-rgb'
        ];
        requiredVars.forEach(v => {
            expect(content).to.contain(v);
        });
    });

    it('v2-components.css must contain .c-badge and .u- utility classes', () => {
        const content = fs.readFileSync(cssPath, 'utf8');
        expect(content).to.contain('.c-badge');
        expect(content).to.contain('.u-mb-2');
        expect(content).to.contain('.u-text-dim');
    });
});

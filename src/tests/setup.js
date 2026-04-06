import { vi } from 'vitest';

// 1. Mock Markdown Parser (Marked)
const markedMock = {
    parse: vi.fn((text) => `<p>${text}</p>`),
    use: vi.fn().mockReturnThis(),
    setOptions: vi.fn().mockReturnThis(),
    default: {
        parse: vi.fn((text) => `<p>${text}</p>`),
        use: vi.fn().mockReturnThis(),
        setOptions: vi.fn().mockReturnThis()
    }
};

vi.stubGlobal('marked', markedMock);
vi.mock('marked', () => ({
    ...markedMock,
    marked: markedMock // Some imports might use named 'marked'
}));

// 2. Mock ECharts (Removed - Transitioned to Vanilla SVG)


// 3. Mock DOM getComputedStyle for CSS variable resolution testing
vi.stubGlobal('getComputedStyle', vi.fn(() => ({
    getPropertyValue: vi.fn((prop) => {
        if (prop === '--accent-color') return '#00f2ff';
        if (prop === '--color-primary') return '#3b82f6';
        if (prop === '--text-dim') return '#9ca3af';
        if (prop === '--text-main') return '#ffffff';
        return '';
    }),
    trim: vi.fn(() => '')
})));

// 4. Mock Global fetch API
vi.stubGlobal('fetch', vi.fn());

// 5. Provide fallback for import.meta.env (for non-vite environments if needed)
if (typeof process !== 'undefined') {
    process.env.VITE_API_BASE_URL = '/api';
}

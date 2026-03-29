import { vi } from 'vitest';

// 1. Mock Markdown Parser (Marked)
vi.stubGlobal('marked', {
    parse: vi.fn((text) => `<p>${text}</p>`)
});

// 2. Mock ECharts
const mockSetOption = vi.fn();
const mockOn = vi.fn();
const mockOff = vi.fn();
const mockGetZr = vi.fn(() => ({ off: vi.fn(), on: vi.fn() }));
const mockResize = vi.fn();

vi.stubGlobal('echarts', {
    getInstanceByDom: vi.fn(() => null),
    init: vi.fn(() => ({
        setOption: mockSetOption,
        on: mockOn,
        off: mockOff,
        getZr: mockGetZr,
        resize: mockResize
    }))
});

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

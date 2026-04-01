import { vi } from 'vitest';

// 1. Mock Markdown Parser (Marked)
const markedMock = {
    parse: vi.fn((text) => `<p>${text}</p>`),
    default: {
        parse: vi.fn((text) => `<p>${text}</p>`)
    }
};

vi.stubGlobal('marked', markedMock);
vi.mock('marked', () => ({
    ...markedMock,
    marked: markedMock // Some imports might use named 'marked'
}));

// 2. Mock ECharts
const mockSetOption = vi.fn();
const mockOn = vi.fn();
const mockOff = vi.fn();
const mockGetZr = vi.fn(() => ({ 
    off: vi.fn(), 
    on: vi.fn(),
    handler: { dispatch: vi.fn() }
}));
const mockResize = vi.fn();
const mockDispose = vi.fn();

const echartsMock = {
    getInstanceByDom: vi.fn(() => null),
    init: vi.fn(() => ({
        setOption: mockSetOption,
        on: mockOn,
        off: mockOff,
        getZr: mockGetZr,
        resize: mockResize,
        dispose: mockDispose,
        showLoading: vi.fn(),
        hideLoading: vi.fn(),
        setGroup: vi.fn()
    })),
    registerTheme: vi.fn(),
    registerMap: vi.fn(),
    graphic: {
        extendShape: vi.fn(),
        registerShape: vi.fn()
    }
};

vi.stubGlobal('echarts', echartsMock);
vi.mock('echarts', () => ({
    ...echartsMock,
    default: echartsMock
}));

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

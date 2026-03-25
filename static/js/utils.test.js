import { describe, it, expect, vi } from 'vitest';
import { escapeHTML, formatDisplayTime, safeAsync } from './utils.js';

describe('utils.js - escapeHTML', () => {
  it('should escape HTML special characters', () => {
    const input = '<script>alert("xss")</script> & "quote"';
    const expected = '&lt;script&gt;alert(&quot;xss&quot;)&lt;/script&gt; &amp; &quot;quote&quot;';
    expect(escapeHTML(input)).toBe(expected);
  });

  it('should return empty string for null/undefined', () => {
    expect(escapeHTML(null)).toBe('');
    expect(escapeHTML(undefined)).toBe('');
  });
});

describe('utils.js - formatDisplayTime', () => {
  it('should return "방금 전" for very recent time (ko)', () => {
    const now = new Date().toISOString();
    expect(formatDisplayTime(now, 'ko')).toBe('방금 전');
  });

  it('should return "1 min. ago" for 1 minute ago (en)', () => {
    const oneMinAgo = new Date(Date.now() - 60000).toISOString();
    expect(formatDisplayTime(oneMinAgo, 'en')).toBe('1 min. ago');
  });
});

describe('utils.js - safeAsync (DOM Interaction)', () => {
  it('should NOT show login overlay when AuthError occurs by default', async () => {
    // Setup Happy DOM environment
    document.body.innerHTML = '<div id="loginOverlay" class="hidden"></div>';
    
    const mockFn = vi.fn().mockRejectedValue({ isAuthError: true });
    const safeFn = safeAsync(mockFn);

    try {
      await safeFn();
    } catch (e) {
      // expected throw
    }

    const overlay = document.getElementById('loginOverlay');
    expect(overlay.classList.contains('hidden')).toBe(true);
  });

  it('should show login overlay when AuthError occurs and triggerAuthOverlay is true', async () => {
    // Setup Happy DOM environment
    document.body.innerHTML = '<div id="loginOverlay" class="hidden"></div>';
    
    const mockFn = vi.fn().mockRejectedValue({ isAuthError: true });
    const safeFn = safeAsync(mockFn, { triggerAuthOverlay: true });

    try {
      await safeFn();
    } catch (e) {
      // expected throw
    }

    const overlay = document.getElementById('loginOverlay');
    expect(overlay.classList.contains('hidden')).toBe(false);
  });
});

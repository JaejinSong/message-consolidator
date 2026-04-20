/**
 * @file apiClient.ts
 * @description Centralized API client for all network requests.
 * Inherits Base URL from VITE_API_BASE_URL environment variable.
 */

export const BASE_URL = ((import.meta as any).env.VITE_API_BASE_URL || '').replace(/\/$/, '');

export interface ApiRequestOptions extends RequestInit {
  params?: Record<string, string | number | boolean | undefined>;
  errorMessage?: string;
}

/**
 * Common fetch wrapper for consistent API communication.
 * @param endpoint The API endpoint (e.g., '/api/messages')
 * @param options Fetch options and query parameters
 */
export async function apiFetch<T = any>(
  endpoint: string,
  options: ApiRequestOptions = {}
): Promise<T> {
  const { params, headers, errorMessage, ...fetchOptions } = options;
  
  // Construct URL with query parameters if provided
  let urlPath = endpoint.startsWith('/') ? endpoint : `/${endpoint}`;

  // Why: Prevent redundant '/api/api' and ensure '/auth' routes stay at root origin.
  // Use URL constructor logic or regex to sanitize the final path.
  let finalUrl: string;
  if (BASE_URL.startsWith('http')) {
    // Absolute URL case (Development or specific override)
    const url = new URL(BASE_URL);
    const rootOrigin = url.origin;
    
    if (urlPath.startsWith('/auth/')) {
      finalUrl = `${rootOrigin}${urlPath}`;
    } else if (urlPath.startsWith('/api/') && url.pathname.endsWith('/api')) {
      finalUrl = `${rootOrigin}${urlPath}`;
    } else {
      finalUrl = `${BASE_URL}${urlPath}`;
    }
  } else {
    // Relative path case (Production: BASE_URL is usually '/api' or empty)
    if (urlPath.startsWith('/auth/')) {
      finalUrl = urlPath; // Use absolute path from root
    } else if (urlPath.startsWith('/api/') && BASE_URL === '/api') {
      finalUrl = urlPath; // Use absolute path from root to avoid /api/api
    } else {
      finalUrl = `${BASE_URL}${urlPath}`;
    }
  }
  
  const searchParams = new URLSearchParams();
  if (params) {
    Object.entries(params).forEach(([key, value]) => {
      if (value !== undefined && value !== null) {
        searchParams.append(key, String(value));
      }
    });
  }
  
  const queryString = searchParams.toString();
  if (queryString) {
    const separator = finalUrl.includes('?') ? '&' : '?';
    finalUrl = `${finalUrl}${separator}${queryString}`;
  }

  // Why: Detailed debug log during development for easier proxy/API issue verification.
  if (import.meta.env.DEV) {
    console.debug(`[API Debug] ${fetchOptions.method || 'GET'} -> ${finalUrl}`);
    console.debug(`[API Debug] Base URL: ${BASE_URL}, Endpoint: ${endpoint}`);
  }

  const defaultHeaders: HeadersInit = {
    'Content-Type': 'application/json',
    ...headers,
  };

  const resp = await fetch(finalUrl, {
    ...fetchOptions,
    headers: defaultHeaders,
    credentials: 'include',
  });

  // Handle common status codes
  if (resp.status === 401) {
    const err: any = new Error('Session Expired or Unauthorized');
    err.isAuthError = true;
    err.status = 401;
    throw err;
  }

  const contentType = resp.headers.get("content-type");

  // Check for unexpected HTML responses
  if (contentType && contentType.includes("text/html")) {
    if (import.meta.env.DEV) {
      console.warn(`[API Warning] Received HTML instead of JSON from ${finalUrl}`);
      console.warn(`[API Warning] Content-Type: ${contentType}`);
      console.warn(`[API Warning] Response Status: ${resp.status}`);
      // Why: Often a 302/redirect by Caddy/Nginx to a login page results in an HTML response.
      console.error('Possible reason: Local development session expired or proxy misconfigured.');
    }
    
    // Why: Prevent JSON.parse error by failing early with a meaningful message.
    const err: any = new Error('Authentication Required (Session Expired or Proxy Redirect)');
    err.isAuthError = true;
    err.status = 401;
    throw err;
  }

  if (!resp.ok) {
    const text = await resp.text();
    if (import.meta.env.DEV) {
      console.error(`[API Error] Status: ${resp.status}`, text);
    }
    const err: any = new Error(errorMessage || text || `Error ${resp.status}`);
    err.status = resp.status;
    throw err;
  }

  if (contentType && contentType.includes("application/json")) {
    return await resp.json() as T;
  }
  
  return resp as unknown as T;
}

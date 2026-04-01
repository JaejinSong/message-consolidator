/**
 * Fetches search result with retry and timeout logic.
 *
 * @param {string} url - The URL to fetch.
 * @param {RequestInit} options - The fetch options.
 * @param {number} [retries=3] - Number of times to retry on failure.
 * @param {number} [timeout=5000] - Timeout in milliseconds.
 * @returns {Promise<Response>}
 */
export async function fetchWithRetry(
    url: string,
    options: RequestInit = {},
    retries: number = 3,
    timeout: number = 5000
): Promise<Response> {
    let lastError: Error | null = null;

    for (let i = 0; i < retries; i++) {
        const controller = new AbortController();
        const id = setTimeout(() => controller.abort(), timeout);

        try {
            const response = await fetch(url, {
                ...options,
                signal: controller.signal
            });
            clearTimeout(id);

            if (response.ok) {
                return response;
            }

            // Only retry on server errors (5xx) or rate limits (429)
            if (response.status < 500 && response.status !== 429) {
                return response;
            }

            lastError = new Error(`HTTP Error: ${response.status}`);
        } catch (err) {
            clearTimeout(id);
            lastError = err as Error;
            // Retry on AbortError or standard Network error
        }

        // Exponential backoff could be added here if needed,
        // but for now we just do a simple immediate retry.
    }

    throw lastError || new Error(`Fetch failed after ${retries} retries`);
}

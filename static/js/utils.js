/**
 * 비동기 함수의 에러 핸들링을 공통화하는 고차 함수 (Higher-Order Function)
 * 중복되는 try-catch 블록을 제거하고, 호출부의 가독성을 높입니다.
 * @param {Function} fn - 실행할 비동기 함수
 * @param {Function} [onError] - 에러 발생 시 실행할 커스텀 롤백 함수 (선택 사항)
 */
export const safeAsync = (fn, onError) => async function (...args) {
    try {
        return await fn.apply(this, args); // 원래 함수의 this 컨텍스트 유지
    } catch (e) {
        console.error('[Async Error]', e);
        if (onError) onError(e);
    }
};
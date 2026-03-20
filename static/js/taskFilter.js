export const createTaskFilter = (state, localeData) => {
    // Keywords for local filtering
    const uniqueKeywords = [];
    if (state.userProfile.email && state.userProfile.email.trim()) {
        const lowEmail = state.userProfile.email.trim().toLowerCase();
        uniqueKeywords.push(lowEmail);
        if (lowEmail.includes('@')) {
            const prefix = lowEmail.split('@')[0];
            uniqueKeywords.push(prefix);
        }
    }
    if (state.userProfile.name && state.userProfile.name.trim()) {
        const lowName = state.userProfile.name.trim().toLowerCase();
        uniqueKeywords.push(lowName);
        const noSpaceName = lowName.replace(/\s+/g, '');
        if (noSpaceName !== lowName) {
            uniqueKeywords.push(noSpaceName);
        }
    }

    if (state.userAliases && Array.isArray(state.userAliases)) {
        state.userAliases.forEach(alias => {
            if (alias && typeof alias === 'string' && alias.trim()) {
                const lowAlias = alias.trim().toLowerCase();
                uniqueKeywords.push(lowAlias);
                const noSpaceAlias = lowAlias.replace(/\s+/g, '');
                if (noSpaceAlias !== lowAlias) {
                    uniqueKeywords.push(noSpaceAlias);
                }
            }
        });
    }

    // 일반 지칭 대명사 (완전 일치 검사용 - 오탐 방지)
    const genericKeywords = ['내업무', '사용자', '나', 'me', 'user', 'mytasks'];
    if (localeData && localeData.myTasks) {
        genericKeywords.push(localeData.myTasks.replace(/\s+/g, '').toLowerCase());
    }

    return (m) => {
        const rawAssignee = (m.assignee || "").trim();
        const normalizedAssignee = rawAssignee.replace(/\s+/g, '').toLowerCase();
        const normalizedRequester = (m.requester || "").replace(/\s+/g, '').toLowerCase();

        const isExplicitMine = normalizedAssignee === '내업무' || normalizedAssignee === 'mytasks' ||
            rawAssignee === (localeData && localeData.myTasks) ||
            uniqueKeywords.includes(normalizedAssignee);

        if (isExplicitMine) return true;
        if (normalizedAssignee === '기타업무' || normalizedAssignee === 'othertasks' || rawAssignee === (localeData && localeData.otherTasks)) return false;

        let isMyTask = uniqueKeywords.some(kw => kw && (
            (m.task && m.task.toLowerCase().includes(kw)) ||
            (rawAssignee && rawAssignee.toLowerCase().includes(kw)) ||
            (m.requester && m.requester.toLowerCase().includes(kw))
        ));
        return isMyTask || genericKeywords.includes(normalizedAssignee) || genericKeywords.includes(normalizedRequester);
    };
};
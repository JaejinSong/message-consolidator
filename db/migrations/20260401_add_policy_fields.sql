-- Why: Supports '운영 정책(Policy)' 및 '맥락 질문(Context Query)' 관리 기능 확장
-- Category 필드는 이미 존재하므로, 맥락 질문 여부(is_context_query)와 정책 제약 사항(constraints) 필드를 추가합니다.

ALTER TABLE messages ADD COLUMN is_context_query INTEGER DEFAULT 0;
ALTER TABLE messages ADD COLUMN constraints TEXT DEFAULT '[]';

-- Why: 프론트엔드 UI 컴포넌트(message-card.ts)에서 신규 필드를 즉시 사용할 수 있도록 v_messages 뷰를 갱신합니다.
DROP VIEW IF EXISTS v_messages;
CREATE VIEW v_messages AS
SELECT 
    m.id, 
    m.user_email, 
    m.source, 
    COALESCE(m.room, '') as room, 
    m.task, 
    COALESCE(cr_req.effective_display_name, m.requester) as requester, 
    COALESCE(cr_asg.effective_display_name, m.assignee, '') as assignee,
    m.assigned_at,
    m.link, 
    m.source_ts, 
    COALESCE(m.original_text, '') as original_text, 
    m.done, 
    m.is_deleted, 
    m.created_at, 
    m.completed_at, 
    COALESCE(m.category, 'todo') as category, 
    COALESCE(m.deadline, '') as deadline,
    COALESCE(m.thread_id, '') as thread_id,
    COALESCE(m.assignee_reason, '') as assignee_reason,
    COALESCE(m.replied_to_id, '') as replied_to_id,
    m.is_context_query, -- 신규 필드 (INTEGER 0/1)
    COALESCE(m.constraints, '[]') as constraints, -- 신규 필드 (JSON TEXT)
    COALESCE(cr_req.effective_canonical_id, m.requester) as requester_canonical,
    COALESCE(cr_asg.effective_canonical_id, m.assignee) as assignee_canonical
FROM messages m
LEFT JOIN v_contacts_resolved cr_req ON m.user_email = cr_req.tenant_email AND m.requester = cr_req.original_canonical_id
LEFT JOIN v_contacts_resolved cr_asg ON m.user_email = cr_asg.tenant_email AND m.assignee = cr_asg.original_canonical_id;

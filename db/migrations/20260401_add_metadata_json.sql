-- Why: 가변적 메타데이터(Context, Constraints 등)를 유연하게 관리하기 위해 JSON 형식의 metadata 필드를 추가합니다.
-- 기존 필드(is_context_query, constraints)는 점진적으로 폐쇄하며, 초기 데이터 정합성을 위해 기존 값을 metadata로 마이그레이션합니다.

ALTER TABLE messages ADD COLUMN metadata TEXT DEFAULT '{}';

-- 기존 데이터 마이그레이션 (is_context_query와 constraints를 JSON 객체로 통합)
UPDATE messages SET metadata = json_object(
    'is_context_query', is_context_query,
    'constraints', json(COALESCE(constraints, '[]'))
) WHERE (is_context_query != 0 OR constraints != '[]');

-- Why: 프론트엔드 v_messages 뷰에서도 metadata 필드를 즉시 사용할 수 있도록 갱신합니다.
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
    m.is_context_query,
    m.constraints,
    m.metadata, -- 신규 메타데이터 필드 추가
    COALESCE(cr_req.effective_canonical_id, m.requester) as requester_canonical,
    COALESCE(cr_asg.effective_canonical_id, m.assignee) as assignee_canonical
FROM messages m
LEFT JOIN v_contacts_resolved cr_req ON m.user_email = cr_req.tenant_email AND m.requester = cr_req.original_canonical_id
LEFT JOIN v_contacts_resolved cr_asg ON m.user_email = cr_asg.tenant_email AND m.assignee = cr_asg.original_canonical_id;

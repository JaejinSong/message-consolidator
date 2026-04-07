CREATE TEMP VIEW v_messages_opt AS
SELECT 
    m.id, 
    m.user_email, 
    m.source, 
    COALESCE(m.room, '') as room, 
    m.task, 
    COALESCE(COALESCE(m_req.display_name, c_req.display_name), m.requester) as requester, 
    COALESCE(COALESCE(m_asg.display_name, c_asg.display_name), m.assignee, '') as assignee
FROM messages m
LEFT JOIN contacts c_req ON m.user_email = c_req.tenant_email AND m.requester = c_req.canonical_id
LEFT JOIN contacts m_req ON c_req.master_contact_id = m_req.id AND c_req.tenant_email = m_req.tenant_email
LEFT JOIN contacts c_asg ON m.user_email = c_asg.tenant_email AND m.assignee = c_asg.canonical_id
LEFT JOIN contacts m_asg ON c_asg.master_contact_id = m_asg.id AND c_asg.tenant_email = m_asg.tenant_email;

EXPLAIN QUERY PLAN
SELECT * FROM v_messages_opt WHERE user_email = 'jjsong@whatap.io';

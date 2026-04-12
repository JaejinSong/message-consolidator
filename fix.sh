#!/bin/bash

# Replace exact strings in messages.sql to add COALESCE
sed -i 's/source, room, task, requester, assignee, assigned_at, link, source_ts, original_text, done, is_deleted, created_at, completed_at, category, deadline, thread_id, assignee_reason, replied_to_id, is_context_query, constraints, metadata, source_channels, consolidated_context, requester_canonical, assignee_canonical, requester_type, assignee_type/COALESCE(source, '\'''\'') as source, COALESCE(room, '\'''\'') as room, COALESCE(task, '\'''\'') as task, COALESCE(requester, '\'''\'') as requester, COALESCE(assignee, '\'''\'') as assignee, assigned_at, COALESCE(link, '\'''\'') as link, COALESCE(source_ts, '\'''\'') as source_ts, COALESCE(original_text, '\'''\'') as original_text, done, is_deleted, created_at, completed_at, COALESCE(category, '\'''\'') as category, COALESCE(deadline, '\'''\'') as deadline, COALESCE(thread_id, '\'''\'') as thread_id, COALESCE(assignee_reason, '\'''\'') as assignee_reason, COALESCE(replied_to_id, '\'''\'') as replied_to_id, is_context_query, COALESCE(constraints, '\'''\'') as constraints, COALESCE(metadata, '\'''\'') as metadata, COALESCE(source_channels, '\'''\'') as source_channels, COALESCE(consolidated_context, '\'''\'') as consolidated_context, COALESCE(requester_canonical, '\'''\'') as requester_canonical, COALESCE(assignee_canonical, '\'''\'') as assignee_canonical, COALESCE(requester_type, '\'''\'') as requester_type, COALESCE(assignee_type, '\'''\'') as assignee_type/g' store/queries/messages.sql

sed -i 's/id, user_email/id, COALESCE(user_email, '\'''\'') as user_email/g' store/queries/messages.sql

# Run sqlc
sqlc generate

go test -v ./store -run TestGetArchivedMessagesFiltered_Status/Status_Done

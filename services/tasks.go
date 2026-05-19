package services

import (
	"context"
	"fmt"
	"message-consolidator/store"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
	"google.golang.org/api/gmail/v1"
)

const (
	AssigneeShared    = "shared"
	CategoryPersonal  = "personal"
	CategoryShared    = "shared"
	CategoryRequested = "requested"
	CategoryOthers    = "others"
)

var (
	//Why: Defines keywords returned by the AI for unspecific or group tasks to support standardized unassignment logic.
	genericOtherAssignees = map[string]bool{"기타 업무": true, "기타업무": true, "other tasks": true, "미지정": true}
)

// TasksService handles task-related operations including formatting, completion, and batch translation.

type TaskAI interface {
	GenerateMergedTaskTitle(ctx context.Context, email string, tasksJSON string) (string, error)
}

// TaskEmbedder is satisfied by *EmbeddingService and is the seam tasks use to
// enqueue archive embeddings after MarkDone. nil-safe via the optional setter.
type TaskEmbedder interface {
	EnqueueForMessage(ctx context.Context, msgID store.MessageID)
}

type TasksService struct {
	translationSvc *TranslationService
	geminiClient   TaskAI
	embedder       TaskEmbedder
}

func NewTasksService(trans *TranslationService, gemini TaskAI) *TasksService {
	return &TasksService{
		translationSvc: trans,
		geminiClient:   gemini,
	}
}

// SetEmbedder wires a background embedding hook for archive transitions.
// Why: separated from the constructor so main.go can build it after Gemini
// init without reshuffling existing callers/tests that pass two args.
func (s *TasksService) SetEmbedder(e TaskEmbedder) { s.embedder = e }

// StripOriginalText removes the original text to reduce payload size.
func (s *TasksService) StripOriginalText(msgs []store.ConsolidatedMessage) {
	for i := range msgs {
		msgs[i].HasOriginal = msgs[i].OriginalText != ""
		msgs[i].OriginalText = ""
	}
}

func (s *TasksService) FormatMessagesForClient(ctx context.Context, email string, msgs []store.ConsolidatedMessage) {
	user, _ := store.GetOrCreateUser(ctx, email, "", "")

	// Pre-aggregation Phase: Extract all unique identifiers from message batch.
	// Why: Eliminates N+1 DB queries by resolving identities in a single bulk operation.
	identifiers := extractUniqueIdentifiers(msgs)
	aliasMap := store.BulkResolveAliases(ctx, email, identifiers)

	aliases, _ := store.GetUserAliasesByEmail(ctx, email)
	identities := GetEffectiveAliases(*user, aliases)

	for i := range msgs {
		if resolved := aliasMap[msgs[i].Requester]; resolved != "" {
			msgs[i].Requester = resolved
		}
		if resolved := aliasMap[msgs[i].Assignee]; resolved != "" {
			msgs[i].Assignee = resolved
		}
		s.applyAssigneeRules(user, identities, &msgs[i])
		s.assignCategory(user, identities, &msgs[i])
	}
}

// assignCategory implements the server-side categorization priority logic.
// Priority: 0. stored requested (when requester is not me), 1. personal, 2. shared, 3. requested, 4. others.
// Why: Decision is derived only from structural identity fields (Assignee/AssigneeCanonical/
// RequesterCanonical), never from Task body text. This keeps classification stable across
// languages and translation state, and avoids second-guessing AI's assignee extraction.
// Broadcast detection is the AI's responsibility at extraction time (Assignee == AssigneeShared).
// Exception: when completion service explicitly persists category=requested on a task where
// the requester is not the current user (delegation/redirect transition), that stored value
// is honored over the Assignee-based derivation so the row stays in "맡긴 업무" instead
// of snapping back to "받은 업무".
func (s *TasksService) assignCategory(user *store.User, identities []string, msg *store.ConsolidatedMessage) {
	isAssigneeMe := s.IsAssigneeMarkedAsMine(msg.Assignee, identities) || strings.EqualFold(msg.AssigneeCanonical, user.Email)
	isRequesterMe := s.IsAssigneeMarkedAsMine(msg.Requester, identities) || strings.EqualFold(msg.RequesterCanonical, user.Email) || msg.Requester == user.Email

	// Why: Honor explicitly stored category=requested when the requester is someone else.
	// This preserves delegation/redirect transitions written by completion service without
	// being overridden by the Assignee-based personal derivation on the next fetch.
	if msg.Category == CategoryRequested && !strings.EqualFold(msg.RequesterCanonical, user.Email) && msg.Requester != user.Email {
		return
	}
	if isAssigneeMe {
		msg.Category = CategoryPersonal
		return
	}
	if msg.Assignee == AssigneeShared {
		msg.Category = CategoryShared
		return
	}
	if isRequesterMe {
		msg.Category = CategoryRequested
		return
	}
	msg.Category = CategoryOthers
}

// hasGroupMention detects common team/group tags to identify non-individual tasks.
func hasGroupMention(text string) bool {
	content := strings.ToLower(text)
	groupWords := []string{"@everyone", "@channel", "@here", "team", "everyone"}
	for _, word := range groupWords {
		if strings.Contains(content, word) {
			return true
		}
	}
	return false
}

func extractUniqueIdentifiers(msgs []store.ConsolidatedMessage) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, m := range msgs {
		if !seen[m.Requester] && m.Requester != "" {
			ids = append(ids, m.Requester)
			seen[m.Requester] = true
		}
		if !seen[m.Assignee] && m.Assignee != "" {
			ids = append(ids, m.Assignee)
			seen[m.Assignee] = true
		}
	}
	return ids
}

func (s *TasksService) applyAssigneeRules(user *store.User, identities []string, msg *store.ConsolidatedMessage) {
	assignee := strings.TrimSpace(msg.Assignee)
	isUnknown := strings.EqualFold(assignee, "undefined") || strings.EqualFold(assignee, "unknown")
	if isUnknown || assignee == "" {
		msg.Assignee = ""
		return
	}

	if s.IsAssigneeMarkedAsMine(assignee, identities) || strings.EqualFold(msg.AssigneeCanonical, user.Email) {
		msg.Assignee = user.PreferredName()
	}

	if strings.EqualFold(strings.TrimSpace(msg.Requester), user.Email) ||
		strings.EqualFold(msg.RequesterCanonical, user.Email) {
		msg.RequesterCanonical = user.Email
		msg.Requester = user.PreferredName()
	} else if (msg.RequesterCanonical == "" || strings.EqualFold(msg.RequesterCanonical, msg.Requester)) &&
		s.IsAssigneeMarkedAsMine(msg.Requester, identities) {
		// Why: canonical == raw requester signals a view-fallback (contacts JOIN miss); treat as unresolved.
		msg.RequesterCanonical = user.Email
		msg.Requester = user.PreferredName()
	}
}

// ApplyTranslations fetches cached translations and triggers JIT for missing ones.
// Why: Returns English immediately for missing translations to prevent UI blocking.


// PrepareMessagesForClient unifies translations, stripping, and formatting.
// Why: Category must be derived from the untranslated Task so that hasGroupMention's
// English-only keyword list yields the same classification regardless of display lang.
func (s *TasksService) PrepareMessagesForClient(ctx context.Context, email string, msgs []store.ConsolidatedMessage, lang string) {
	s.FormatMessagesForClient(ctx, email, msgs)
	s.ApplyTranslations(ctx, email, lang, msgs)
	s.StripOriginalText(msgs)
}

// HandleTaskCompletion orchestrates the process of marking a task as done.
func (s *TasksService) HandleTaskCompletion(ctx context.Context, email string, taskID store.MessageID, done bool) error {
	if taskID <= 0 {
		return fmt.Errorf("invalid task id: %d", taskID)
	}
	msg, err := store.GetMessageByID(ctx, store.GetDB(), email, taskID)
	if err == nil && msg.Done && done {
		return nil
	}

	if err := store.MarkMessageDone(ctx, store.GetDB(), email, taskID, done); err != nil {
		return err
	}
	// Why: archive transition (done=true) is the only point we want to spend an
	// embedding API call. Background-detached so the user's MarkDone response
	// returns immediately even if Gemini is slow.
	if done && s.embedder != nil {
		s.embedder.EnqueueForMessage(ctx, taskID)
	}
	return nil
}

// ReclassifyUserTasks re-evaluates assignees for a user's tasks based on identities and content.
func (s *TasksService) ReclassifyUserTasks(ctx context.Context, email string, user *store.User, aliases []string, msgs []store.ConsolidatedMessage) int {
	allMyIdentities := GetEffectiveAliases(*user, aliases)
	fixedCount := 0

	for _, m := range msgs {
		if s.reclassifySingleTask(ctx, email, user, allMyIdentities, m) {
			fixedCount++
		}
	}
	return fixedCount
}

func (s *TasksService) reclassifySingleTask(ctx context.Context, email string, user *store.User, allMyIdentities []string, m store.ConsolidatedMessage) bool {
	// Guard: Clear generic "other" assignees for manual re-assignment.
	if shouldClearAssignee(m.Assignee) {
		_ = store.UpdateTaskAssignee(ctx, nil, email, m.ID, "")
		return true
	}

	isMarkedAsMine := s.IsAssigneeMarkedAsMine(m.Assignee, allMyIdentities)
	if !isMarkedAsMine {
		return false
	}

	isDirectGmail := s.IsDirectlyAddressedToMe(m, user.Email)

	// Guard: Automatically un-assign Gmail tasks wrongly assigned to "me" if only CC/BCC.
	if m.Source == "gmail" && !isDirectGmail && isAssigneeGeneric(m.Assignee) {
		_ = store.UpdateTaskAssignee(ctx, nil, email, m.ID, "")
		return true
	}

	matchedByAlias := IsTaskMatchedByAlias(m, allMyIdentities, isDirectGmail)
	newAssignee, changed := s.resolveNewAssignee(user, m.Assignee, matchedByAlias)
	if changed {
		_ = store.UpdateTaskAssignee(ctx, nil, email, m.ID, newAssignee)
		return true
	}

	return false
}

// RestoreGmailCCAssignment identifies Gmail tasks that were incorrectly assigned due to the user being CC'd.
// Why: [Performance] Uses errgroup with a worker limit of 20 to parallelize Gmail API resolution and returns a map for batch DB updates.
func (s *TasksService) RestoreGmailCCAssignment(ctx context.Context, email string, user *store.User, aliases []string, msgs []store.ConsolidatedMessage, svc *gmail.Service) (map[store.MessageID]string, int) {
	updates := make(map[store.MessageID]string)
	var mu sync.Mutex
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(20)

	for _, m := range msgs {
		m := m
		g.Go(func() error {
			id, actual, changed := s.checkRestoreGmailCC(ctx, email, user, aliases, m, svc)
			if changed {
				mu.Lock()
				updates[id] = actual
				mu.Unlock()
			}
			return nil
		})
	}

	_ = g.Wait()
	return updates, len(updates)
}

func (s *TasksService) checkRestoreGmailCC(ctx context.Context, email string, user *store.User, aliases []string, m store.ConsolidatedMessage, svc *gmail.Service) (store.MessageID, string, bool) {
	if m.Source != "gmail" {
		return 0, "", false
	}

	toHeader := extractToHeader(m.OriginalText)
	if isMeInToHeader(toHeader, user.Email) {
		return 0, "", false
	}

	if !s.IsAssigneeMarkedAsMine(m.Assignee, GetEffectiveAliases(*user, aliases)) {
		return 0, "", false
	}

	actualAssignee := resolveActualAssignee(ctx, m, toHeader, svc)
	if actualAssignee == "" || strings.TrimSpace(m.Assignee) == actualAssignee {
		return 0, "", false
	}

	return m.ID, actualAssignee, true
}


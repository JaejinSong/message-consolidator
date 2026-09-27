// Package scanner — optional ChannelAdapter capability interfaces.
// Why: these were previously declared inline in channel_adapter.go and satisfied
// implicitly by each adapter with no compile-time check, so a renamed method on an
// adapter silently dropped a capability instead of failing the build. Declaring
// them here alongside the `var _ <iface> = <adapter>{}` assertions below makes a
// dropped capability a build failure instead of a silent runtime behavior change.
package scanner

import (
	"context"

	"message-consolidator/store"
	"message-consolidator/types"
)

// driverCompletionOptOut — optional: an adapter whose drain phase already feeds
// the completion pipeline over ALL raw rows (pre-classification) implements this
// so the driver does not double-dispatch the classified subset (LINE).
type driverCompletionOptOut interface{ ownsCompletionDispatch() }

// saveThreadAnchor — optional: channels that anchor reply threads on the saved
// message's own ID (LINE) or parent thread ts (Slack) provide the thread_id
// persisted with the task; WhatsApp/Telegram leave it empty.
type saveThreadAnchor interface {
	SaveThreadID(m types.RawMessage) string
}

// saveLinker — optional: channels with permalinks (Slack) build the task's Link;
// Slack additionally registers the thread for sweep tracking inside this call.
type saveLinker interface {
	SaveLink(ctx context.Context, m types.RawMessage, email string) string
}

// scanAcker — optional: channels backed by a durable, replayable message log
// (WhatsApp) acknowledge each scanned group so a scan that errors out before
// finishing leaves its messages eligible for replay instead of silently
// advancing past them. ok=true means the group was handled to a terminal
// state (noise-filtered, AI success, or usable fallback); ok=false means the
// group should be retried on a future scan.
type scanAcker interface {
	AckScanned(ctx context.Context, email string, ids []string, ok bool)
}

// resolveTrustSource — optional: an adapter whose adapter.IsFromMe is pinned false
// for the category-override path (Slack) but still has real sender identity
// available provides this so the injection loop can feed isTrustedResolve an
// accurate IsFromMe without touching the category-override behavior.
type resolveTrustSource interface {
	IsOwnMessage(m types.RawMessage, user store.User) bool
}

// RoomRenamer is implemented by adapters whose stored room label may predate the name they can
// resolve today. LegacyRoomName reports the label such history was written under, or "" when
// the adapter has never had a weaker fallback.
type RoomRenamer interface {
	LegacyRoomName(roomKey string) string
}

// proposalThreadAnchor — optional: an adapter whose raw messages never populate
// ThreadID (Slack sets only ReplyToID on replies, leaving root messages with no
// anchor at all) provides the thread anchor the injection loop should use for
// candidates.ThreadID instead of raw.ThreadID. Why: 33 open Slack tasks were
// fuzzy-renamed by unrelated replies in other threads of the same channel
// because findMatch's cross-thread guard never fired with an empty ThreadID.
type proposalThreadAnchor interface {
	ProposalThreadID(m types.RawMessage) string
}

// Compile-time checks: every adapter must satisfy ChannelAdapter, and each optional
// capability assertion below must match the adapter's actual method set -- a rename
// or removal here now fails the build instead of silently dropping the capability.
var (
	_ ChannelAdapter = whatsAppAdapter{}
	_ ChannelAdapter = telegramAdapter{}
	_ ChannelAdapter = (*slackAdapter)(nil)
	_ ChannelAdapter = (*lineAdapter)(nil)

	_ scanAcker        = whatsAppAdapter{}
	_ RoomRenamer      = whatsAppAdapter{}
	_ saveThreadAnchor = whatsAppAdapter{}

	_ driverCompletionOptOut = (*lineAdapter)(nil)
	_ saveThreadAnchor       = (*lineAdapter)(nil)

	_ driverCompletionOptOut = (*slackAdapter)(nil)
	_ saveThreadAnchor       = (*slackAdapter)(nil)
	_ saveLinker             = (*slackAdapter)(nil)
	_ resolveTrustSource     = (*slackAdapter)(nil)
	_ proposalThreadAnchor   = (*slackAdapter)(nil)
)

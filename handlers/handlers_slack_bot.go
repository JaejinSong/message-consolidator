package handlers

import (
	"context"
	"encoding/json"
	"io"
	"message-consolidator/auth"
	"message-consolidator/internal/safego"
	"message-consolidator/logger"
	"message-consolidator/services"
	"message-consolidator/store"
	"net/http"
	"net/url"
	"strconv"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/whatap/go-api/trace"
)

// Why: Slack expects 200 within 3s on every webhook; reading the raw body up-front
// (then verifying signature, then dispatching async) keeps inbound handlers fast and
// makes signature verification possible (slack-go form parsers consume the body).
const slackBodyMaxBytes = 1 << 20 // 1MiB — far above any realistic Slack payload

// HandleSlackEvent receives Events API webhooks (message.im, app_mention, url_verification).
func (a *API) HandleSlackEvent(w http.ResponseWriter, r *http.Request) {
	body, ok := a.readAndVerifySlack(w, r)
	if !ok {
		return
	}

	// Why: url_verification arrives once at app registration; respond synchronously with
	// the challenge so Slack accepts the endpoint.
	var probe struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(body, &probe); err == nil && probe.Type == slackevents.URLVerification {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(probe.Challenge))
		return
	}

	event, err := slackevents.ParseEvent(body, slackevents.OptionNoVerifyToken())
	if err != nil {
		logger.Warnf("[SLACKBOT] parse event failed: %v", err)
		respondError(w, http.StatusBadRequest, "invalid event")
		return
	}

	w.WriteHeader(http.StatusOK)

	if event.Type != slackevents.CallbackEvent {
		return
	}
	go a.dispatchSlackEvent(event) //nolint:contextcheck // Slack 3s ACK rule: request ctx dies after w.WriteHeader. Background goroutine starts fresh trace context.
}

// HandleSlackInteractive receives Block Kit button clicks (block_actions).
func (a *API) HandleSlackInteractive(w http.ResponseWriter, r *http.Request) {
	body, ok := a.readAndVerifySlack(w, r)
	if !ok {
		return
	}

	form, err := url.ParseQuery(string(body))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid form")
		return
	}
	rawPayload := form.Get("payload")
	if rawPayload == "" {
		respondError(w, http.StatusBadRequest, "missing payload")
		return
	}

	var cb slack.InteractionCallback
	if err := json.Unmarshal([]byte(rawPayload), &cb); err != nil {
		logger.Warnf("[SLACKBOT] interaction unmarshal: %v", err)
		respondError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	w.WriteHeader(http.StatusOK)
	go a.dispatchSlackInteraction(cb) //nolint:contextcheck // see HandleSlackEvent: background goroutine owns its own trace ctx.
}

// HandleSlackCommand receives slash commands (`/tasks`).
func (a *API) HandleSlackCommand(w http.ResponseWriter, r *http.Request) {
	body, ok := a.readAndVerifySlack(w, r)
	if !ok {
		return
	}

	form, err := url.ParseQuery(string(body))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid form")
		return
	}
	slackUserID := form.Get("user_id")
	channelID := form.Get("channel_id")
	if slackUserID == "" || channelID == "" {
		respondError(w, http.StatusBadRequest, "missing identifiers")
		return
	}

	// Why: empty 200 ack means Slack will not echo a default text — bot replies via
	// chat.postMessage in the background goroutine instead, which lets us send the
	// full Block Kit list rather than a plain ack string.
	w.WriteHeader(http.StatusOK)
	go a.dispatchSlackCommand(slackUserID, channelID, form.Get("text")) //nolint:contextcheck // see HandleSlackEvent: background goroutine owns its own trace ctx.
}

func (a *API) readAndVerifySlack(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if a.Bot == nil || a.Config.SlackSigningSecret == "" {
		respondError(w, http.StatusServiceUnavailable, "slack bot disabled")
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, slackBodyMaxBytes))
	if err != nil {
		respondError(w, http.StatusBadRequest, "cannot read body")
		return nil, false
	}
	ts := r.Header.Get("X-Slack-Request-Timestamp")
	sig := r.Header.Get("X-Slack-Signature")
	if err := auth.VerifySlackRequest(a.Config.SlackSigningSecret, ts, sig, body); err != nil {
		logger.Warnf("[SLACKBOT] signature verify failed: %v", err)
		respondError(w, http.StatusUnauthorized, "invalid signature")
		return nil, false
	}
	return body, true
}

func (a *API) dispatchSlackEvent(event slackevents.EventsAPIEvent) {
	defer safego.Recover("slackbot-event")
	ctx, _ := trace.Start(context.Background(), "/Slack-Event")
	var err error
	defer func() { _ = trace.End(ctx, err) }()

	switch inner := event.InnerEvent.Data.(type) {
	case *slackevents.AppMentionEvent:
		err = a.Bot.HandleDMText(ctx, inner.User, inner.Channel, inner.Text)
	case *slackevents.MessageEvent:
		// Why: only react to user-authored IM (DM) messages. Filter bots, edits, and
		// non-DM channel_types so the bot does not respond to channel posts or its own
		// replies (which would trigger an infinite loop).
		if inner.BotID != "" || inner.SubType != "" || inner.ChannelType != "im" || inner.User == "" {
			return
		}
		err = a.Bot.HandleDMText(ctx, inner.User, inner.Channel, inner.Text)
	default:
		return
	}
	if err != nil {
		logger.Warnf("[SLACKBOT] dispatch event: %v", err)
	}
}

func (a *API) dispatchSlackInteraction(cb slack.InteractionCallback) {
	defer safego.Recover("slackbot-interaction")
	ctx, _ := trace.Start(context.Background(), "/Slack-Interaction")
	var err error
	defer func() { _ = trace.End(ctx, err) }()

	if len(cb.ActionCallback.BlockActions) == 0 {
		return
	}
	action := cb.ActionCallback.BlockActions[0]
	channel := cb.Container.ChannelID
	messageTS := cb.Container.MessageTs
	if channel == "" {
		channel = cb.Channel.ID
	}

	kind, arg, ok := services.ParseSlackActionID(action.ActionID)
	if !ok {
		return
	}
	switch kind {
	case services.SlackActionTaskDone:
		id, parseErr := strconv.ParseInt(arg, 10, 64)
		if parseErr != nil || id <= 0 {
			err = parseErr
			return
		}
		err = a.Bot.HandleDoneAction(ctx, cb.User.ID, channel, messageTS, store.MessageID(id), 0)
	case services.SlackActionTaskPage:
		page, parseErr := strconv.Atoi(arg)
		if parseErr != nil {
			err = parseErr
			return
		}
		err = a.Bot.HandlePageAction(ctx, cb.User.ID, channel, messageTS, page)
	}
	if err != nil {
		logger.Warnf("[SLACKBOT] dispatch interaction (%s): %v", action.ActionID, err)
	}
}

func (a *API) dispatchSlackCommand(slackUserID, channel, text string) {
	defer safego.Recover("slackbot-command")
	ctx, _ := trace.Start(context.Background(), "/Slack-Command")
	var err error
	defer func() { _ = trace.End(ctx, err) }()

	// Why: `/tasks` plus optional positional arg ("done 123") routes through the same
	// parser used for DM text so behavior stays consistent across surfaces.
	if text == "" {
		text = "tasks"
	}
	err = a.Bot.HandleDMText(ctx, slackUserID, channel, text)
	if err != nil {
		logger.Warnf("[SLACKBOT] dispatch command: %v", err)
	}
}

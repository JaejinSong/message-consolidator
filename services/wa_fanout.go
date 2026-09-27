package services

import "message-consolidator/types"

// FanOutWAReceivers returns a single OnMessage-shaped callback that invokes each
// receiver in order. Why: main.go wires multiple independent WhatsApp message
// sinks (durable DB log, Notion mirror) onto one WAManager.OnMessage hook.
func FanOutWAReceivers(receivers ...func(email, chatJID string, msg types.RawMessage)) func(email, chatJID string, msg types.RawMessage) {
	return func(email, chatJID string, msg types.RawMessage) {
		for _, receive := range receivers {
			receive(email, chatJID, msg)
		}
	}
}

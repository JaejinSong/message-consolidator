package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/slack-go/slack"
)

// check-slack-scope verifies that SLACK_TOKEN can post DMs (chat:write + im:write).
// Usage: go run ./cmd/check-slack-scope -to <slack_user_id>
//   ex: go run ./cmd/check-slack-scope -to U01ABCD1234
// If -to omitted, falls back to looking up the bot's own user (auth.test) — only
// proves chat:write to self, which Slack treats as a no-op for bot tokens.
func main() {
	_ = godotenv.Load(".env")

	to := flag.String("to", "", "target Slack user_id (e.g. U01ABCD1234) — omit to use bot self")
	dry := flag.Bool("dry", false, "skip PostMessage; only run auth.test")
	flag.Parse()

	token := strings.TrimSpace(os.Getenv("SLACK_TOKEN"))
	if token == "" {
		log.Fatal("SLACK_TOKEN not found in .env")
	}

	api := slack.New(token)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	authResp, err := api.AuthTestContext(ctx)
	if err != nil {
		log.Fatalf("auth.test failed: %v", err)
	}
	fmt.Printf("[auth.test] team=%q user=%q user_id=%s bot_id=%s\n",
		authResp.Team, authResp.User, authResp.UserID, authResp.BotID)

	scopes := fetchScopes(ctx, token)
	fmt.Printf("[scopes] %s\n", scopes)
	if !strings.Contains(scopes, "chat:write") {
		fmt.Println("[FAIL] chat:write missing — add it to the Slack app manifest then reinstall")
		os.Exit(1)
	}
	fmt.Println("[OK] chat:write present")

	if *dry {
		fmt.Println("[dry] skipping PostMessage")
		return
	}

	target := *to
	if target == "" {
		target = authResp.UserID
		fmt.Printf("[warn] -to not given, using bot self user_id=%s (likely no-op for bot tokens)\n", target)
	}

	channelID, ts, err := api.PostMessageContext(ctx, target,
		slack.MsgOptionText("[mc] scope check — please ignore", false),
		slack.MsgOptionAsUser(false),
	)
	if err != nil {
		fmt.Printf("[FAIL] PostMessage error: %v\n", err)
		fmt.Println()
		fmt.Println("Likely fixes:")
		fmt.Println("  - missing_scope: add chat:write (and im:write for opening DMs) to the Slack app manifest")
		fmt.Println("  - not_in_channel: bot needs im:write or user has not opened a DM with the bot yet")
		fmt.Println("  - not_allowed_token_type: token is not a Bot Token (xoxb-)")
		fmt.Println("  - channel_not_found: target user_id is wrong or the user is in a different workspace")
		os.Exit(1)
	}
	fmt.Printf("[OK] DM delivered: channel=%s ts=%s\n", channelID, ts)
}

// fetchScopes calls auth.test via raw HTTP so we can read X-OAuth-Scopes from headers.
// The slack-go SDK strips response headers, so we duplicate the call cheaply to surface them.
func fetchScopes(ctx context.Context, token string) string {
	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/auth.test", strings.NewReader(url.Values{}.Encode()))
	if err != nil {
		return "<request error>"
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "<http error: " + err.Error() + ">"
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if s := resp.Header.Get("X-OAuth-Scopes"); s != "" {
		return s
	}
	return "<no X-OAuth-Scopes header>"
}


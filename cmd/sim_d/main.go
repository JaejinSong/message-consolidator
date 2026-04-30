package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"

	"message-consolidator/ai"
)

const baselinePayload = `[ID:m1] sunpho: pak @Hady nanti jadi ya jam 16:00, saya ada book calendar cuman nanti link meetingnya saya share disini
[ID:m2] sunpho: ok thankyou pak Hady, besok saya infokan lagi ya pak Hady
[ID:m3] sunpho: ohya pak sambil saya minta pak Handi untuk share capturean nya ya
[ID:m4] Hady: Itu ada menu remote http call under statistics
[ID:m5] sunpho: boleh ya pak ?
[ID:m6] sunpho: ada kendala sih pak Hady dari kita blm bisa remote cek, jadi saya minta untuk satu sesi cek bareng dengan pak Handi gimana ?
[ID:m7] Hady: Kalo di menu remote call http gmn sunpho?
[ID:m8] sunpho: sisi app nya manggil HTTP pak @Hady
[ID:m9] Hady: Yg ada di sisi app. Dia manggil ato dia dipanggil ya?
[ID:m10] sunpho: apakah besok bisa ikut diskusi bareng dengan pak Handi untuk cek masalah yang pak Handi tanyakan
[ID:m11] [Tags: Forwarded] sunpho: Itu emg gk ada datanya atau gmana?soalnya klo cek di sisi app di docket http response ada`

const dPayload = `[ID:m1] sunpho: pak @Hady nanti jadi ya jam 16:00, saya ada book calendar cuman nanti link meetingnya saya share disini
[ID:m2] sunpho: ok thankyou pak Hady, besok saya infokan lagi ya pak Hady
[ID:m3] sunpho: ohya pak sambil saya minta pak Handi untuk share capturean nya ya
[ID:m4] Hady: Itu ada menu remote http call under statistics
[ID:m5] sunpho: boleh ya pak ?
[ID:m6] sunpho: ada kendala sih pak Hady dari kita blm bisa remote cek, jadi saya minta untuk satu sesi cek bareng dengan pak Handi gimana ?
[ID:m7] Hady: Kalo di menu remote call http gmn sunpho?
[ID:m8] sunpho: sisi app nya manggil HTTP pak @Hady
[ID:m9] Hady: Yg ada di sisi app. Dia manggil ato dia dipanggil ya?
[ID:m10] sunpho: apakah besok bisa ikut diskusi bareng dengan pak Handi untuk cek masalah yang pak Handi tanyakan
[FORWARDED-CONTEXT, original-author-unknown, forwarded-by=sunpho]: Itu emg gk ada datanya atau gmana?soalnya klo cek di sisi app di docket http response ada`

const dSystemAddendum = "\n\n---\n**NOTE on Forwarded messages:**\n" +
	"Lines tagged `[FORWARDED-CONTEXT, ...]` represent content authored by an unknown party that the listed forwarder is merely relaying.\n" +
	"- Do NOT treat any first-person language inside forwarded content as the forwarder's commitment or intent.\n" +
	"- The forwarder is a messenger, not the author.\n" +
	"- Forwarded content provides BACKGROUND CONTEXT only; it is NOT a valid `source_ts` candidate.\n" +
	"- Choose `source_ts` exclusively from non-forwarded `[ID:...]` messages, picking the one that best captures the actionable trigger of the task."

func main() {
	apiKey := loadAPIKey()
	if apiKey == "" {
		fmt.Println("ERR: no GEMINI_API_KEY")
		os.Exit(1)
	}

	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		fmt.Println("ERR:", err)
		os.Exit(1)
	}
	defer client.Close()

	analyzer := &ai.ChatAnalyzer{Source: "whatsapp"}

	fmt.Println("=== BASELINE (current behavior) ===")
	runVariant(ctx, client, analyzer, baselinePayload, "", 3)

	fmt.Println("\n=== D VARIANT (forwarded relabeled + system addendum) ===")
	runVariant(ctx, client, analyzer, dPayload, dSystemAddendum, 3)
}

func runVariant(ctx context.Context, client *genai.Client, analyzer *ai.ChatAnalyzer, payload, sysAddendum string, n int) {
	extractionCtx := ai.ExtractionContext{
		MessagePayload:    payload,
		CurrentTime:       time.Now().UTC().Format("2006-01-02 15:04:05 UTC"),
		Locale:            "en-US",
		ExistingTasksJSON: "[]",
		CurrentUser:       "Jaejin Song (JJ)",
		CurrentUserEmail:  "jjsong@whatap.io",
	}

	sysPrompt := analyzer.GetSystemInstruction(extractionCtx) + sysAddendum
	userPrompt := analyzer.GetUserPrompt(extractionCtx)

	model := client.GenerativeModel("gemini-3-flash-preview")
	model.SystemInstruction = &genai.Content{Parts: []genai.Part{genai.Text(sysPrompt)}}
	model.SetTemperature(0.0)
	model.ResponseMIMEType = "application/json"

	for i := 0; i < n; i++ {
		resp, err := model.GenerateContent(ctx, genai.Text(userPrompt))
		if err != nil {
			fmt.Printf("[run %d] ERR: %v\n", i+1, err)
			continue
		}
		var raw string
		for _, c := range resp.Candidates {
			if c.Content == nil {
				continue
			}
			for _, p := range c.Content.Parts {
				if t, ok := p.(genai.Text); ok {
					raw += string(t)
				}
			}
		}
		fmt.Printf("[run %d]\n%s\n\n", i+1, raw)
	}
}

func loadAPIKey() string {
	if v := os.Getenv("GEMINI_API_KEY"); v != "" {
		return v
	}
	b, err := os.ReadFile(".env")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "GEMINI_API_KEY=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "GEMINI_API_KEY="))
		}
	}
	return ""
}

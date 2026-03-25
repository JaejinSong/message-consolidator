package scanner

import (
	"context"
	"message-consolidator/channels"
	"message-consolidator/config"
	"message-consolidator/logger"
	"message-consolidator/services"
	"message-consolidator/store"
	"strings"
	"time"

	"message-consolidator/ai"

	"github.com/whatap/go-api/trace"
	"golang.org/x/sync/errgroup"
)

var (
	cfg           *config.Config
	completionSvc *services.CompletionService
)

func Init(c *config.Config) {
	cfg = c
	if cfg.GeminiAPIKey != "" {
		gClient, err := ai.NewGeminiClient(context.Background(), cfg.GeminiAPIKey, cfg.GeminiAnalysisModel, cfg.GeminiTranslationModel)
		if err != nil {
			logger.Errorf("[SCANNER] Failed to init GeminiClient for completion service: %v", err)
		} else {
			completionSvc = services.NewCompletionService(gClient, &services.DefaultTaskStore{})
		}
	}
}

func StartBackgroundScanner() {
	logger.Infof("Background scanner started (59s interval for anti-resonance)...")
	ticker := time.NewTicker(59 * time.Second)
	defer ticker.Stop()

	// Run initial scan
	RunAllScans()

	// Start Slow Sweeper as a separate background worker
	go startSlowSweeper()

	for range ticker.C {
		RunAllScans()
	}
}

func RunAllScans() {
	traceCtx, _ := trace.StartWithContext(context.Background(), "Background-RunAllScans")
	defer trace.End(traceCtx, nil)

	users, err := store.GetAllUsers()
	if err != nil {
		logger.Errorf("Scanner Error: Failed to get users: %v", err)
		return
	}

	var eg errgroup.Group
	// errgroup을 통해 세마포어(제한)와 에러 수집, 고루틴 대기를 동시에 우아하게 처리합니다.
	eg.SetLimit(5)

	for _, user := range users {
		// Get aliases for this user
		aliases, _ := store.GetUserAliases(user.ID)

		u, al := user, aliases
		eg.Go(func() error {
			scanAllSources(u, al)
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		logger.Errorf("Scanner Error: One or more user scans failed: %v", err)
	}

	// Slack API 호출 낭비를 막기 위해, 봇이 참여한 전체 채널을 1번만 순회하며 모든 유저의 메시지를 일괄 처리합니다.
	if cfg.SlackToken != "" {
		ctx, cancel := context.WithTimeout(traceCtx, 60*time.Second)
		scanSlack(ctx, users)
		cancel()

		// Slack 스캔 완료 후 각 유저의 메타데이터 영속성 보장
		for _, u := range users {
			store.PersistAllScanMetadata(u.Email)
		}
	}

	// 글로벌 유지보수 작업 (사용자 루프 밖에서 1번만 실행)
	if err := store.ArchiveOldTasks(); err != nil {
		logger.Errorf("Scanner Error: Failed to archive old tasks: %v", err)
	}

	// 게이미피케이션 데이터 주기적 반영 (Piggyback)
	if err := services.FlushGamificationData(); err != nil {
		logger.Errorf("Scanner Error: Failed to flush gamification data: %v", err)
	}

	// 주기적(1시간)으로 메모리에 쌓인 미반영 토큰 사용량을 DB에 플러시하여 NeonDB Sleep 보장
	store.FlushTokenUsageIfNeeded()

	// DB 커넥션 풀 상태 모니터링 로깅 (NeonDB Sleep 진입 전 커넥션 반환 확인용)
	store.LogDBStats()
}

// getEffectiveAliases는 사용자가 명시적으로 등록한 별칭 외에
// 본인의 이름과 이메일 아이디를 자동으로 포함시켜 스캔 시 감지 누락을 방지합니다.
func getEffectiveAliases(user store.User, aliases []string) []string {
	effective := append([]string{}, aliases...)
	if user.Name != "" {
		effective = append(effective, user.Name)
	}
	if prefix, _, found := strings.Cut(user.Email, "@"); found && prefix != "" {
		effective = append(effective, prefix)
	}
	return effective
}

func scanAllSources(user store.User, aliases []string) {
	logger.Debugf("[SCAN] Scanning for user: %s", user.Email)

	// 타임아웃을 위한 Context 생성 (사용자 1명당 전체 스캔 최대 45초 대기)
	// 에러 시 다른 고루틴을 취소하지 않기 위해 errgroup.WithContext는 사용하지 않습니다.
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var eg errgroup.Group
	effectiveAliases := getEffectiveAliases(user, aliases)

	// 1. Gmail 스캔 (병렬 처리)
	if store.HasGmailToken(user.Email) {
		eg.Go(func() error {
			logger.Debugf("[SCAN] Starting Gmail scan for %s", user.Email)
			onSent := func(msg store.ConsolidatedMessage) {
				if completionSvc != nil {
					completionSvc.ProcessPotentialCompletion(context.Background(), msg)
				}
			}
			channels.ScanGmail(ctx, user.Email, "Korean", cfg, onSent)
			return nil
		})
	}

	// 3. WhatsApp 스캔 (병렬 처리 및 누락된 호출 복구)
	eg.Go(func() error {
		logger.Debugf("[SCAN] Starting WhatsApp scan for %s", user.Email)
		scanWhatsApp(ctx, user, effectiveAliases, "Korean")
		return nil
	})

	eg.Wait() // 모든 채널의 스캔이 끝날 때까지 대기

	// Persistence of scan metadata
	store.PersistAllScanMetadata(user.Email)
}

func Scan(email string, lang string) {
	traceCtx, _ := trace.StartWithContext(context.Background(), "ManualScan")
	defer trace.End(traceCtx, nil)

	user, err := store.GetOrCreateUser(email, "", "")
	if err != nil {
		logger.Errorf("[SCAN] Failed to get user %s: %v", email, err)
		return
	}
	aliases, _ := store.GetUserAliases(user.ID)
	effectiveAliases := getEffectiveAliases(*user, aliases)

	// 수동 스캔도 무한 대기 방지를 위해 타임아웃 적용 (최대 60초)
	ctx, cancel := context.WithTimeout(traceCtx, 60*time.Second)
	defer cancel()

	// Gmail
	if store.HasGmailToken(email) {
		onSent := func(msg store.ConsolidatedMessage) {
			if completionSvc != nil {
				completionSvc.ProcessPotentialCompletion(context.Background(), msg)
			}
		}
		channels.ScanGmail(ctx, email, lang, cfg, onSent)
	}

	// Slack (수동 스캔 시 단일 유저라도 동일 인터페이스 사용)
	scanSlack(ctx, []store.User{*user})

	// WhatsApp
	scanWhatsApp(ctx, *user, effectiveAliases, lang)

	// 수동 스캔 후 메타데이터 영구 저장
	store.PersistAllScanMetadata(user.Email)

	// Piggyback: 수동 스캔 완료 시점에 게이미피케이션 데이터 플러시
	_ = services.FlushGamificationData()
}

// IsAliasMatched는 짧은 별칭('나', 'me')이나 일반 대명사로 인한 오탐을 방지하기 위한 안전한 매칭 함수입니다.
func IsAliasMatched(text, sender, alias string) bool {
	lowerAlias := strings.ToLower(strings.TrimSpace(alias))
	if lowerAlias == "" {
		return false
	}
	aliasLen := len([]rune(lowerAlias))

	// 1. 발신자 일치 검사
	if sender != "" {
		lowerSender := strings.ToLower(sender)
		if lowerSender == lowerAlias || (aliasLen > 1 && strings.Contains(lowerSender, lowerAlias)) {
			return true
		}
	}

	// 2. 본문 일치 검사
	if text != "" {
		lowerText := strings.ToLower(text)
		if aliasLen <= 2 {
			// 짧은 별칭은 띄어쓰기 단위로 분리하여 엄격하게 검사
			words := strings.Fields(lowerText)
			for _, w := range words {
				// 완전히 일치하거나, 한국어 조사 처리를 위해 접두어로 쓰인 경우 허용 ("나", "나는", "me")
				if w == lowerAlias || (strings.HasPrefix(w, lowerAlias) && len([]rune(w)) <= aliasLen+2) {
					return true
				}
			}
		} else {
			// 길이가 긴 고유 별칭은 기존처럼 유연하게 부분 일치 허용
			if strings.Contains(lowerText, lowerAlias) {
				return true
			}
		}
	}

	return false
}

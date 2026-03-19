package main

import (
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	SlackToken         string
	GeminiAPIKey       string
	GoogleClientID     string
	GoogleClientSecret string
	AuthSecret         string
	AuthDisabled       bool
	AppBaseURL         string
	NeonDBURL          string
	GeminiAnalysisModel    string
	GeminiTranslationModel string
	LogLevel               string
}

func LoadConfig() *Config {
	// .env 파일 로드 (파일이 없어도 환경 변수가 설정되어 있을 수 있으므로 에러 무시 가능)
	err := godotenv.Load()
	if err != nil {
		infof("No .env file found, using system environment variables")
	}

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "INFO"
	}

	geminiAnalysisModel := os.Getenv("GEMINI_ANALYSIS_MODEL")
	if geminiAnalysisModel == "" {
		geminiAnalysisModel = "gemini-3-flash-preview"
	}

	geminiTranslationModel := os.Getenv("GEMINI_TRANSLATION_MODEL")
	if geminiTranslationModel == "" {
		geminiTranslationModel = "gemini-3.1-flash-lite"
	}

	return &Config{
		SlackToken:             os.Getenv("SLACK_TOKEN"),
		GeminiAPIKey:           os.Getenv("GEMINI_API_KEY"),
		GoogleClientID:         os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret:     os.Getenv("GOOGLE_CLIENT_SECRET"),
		AuthSecret:             os.Getenv("AUTH_SECRET"),
		AuthDisabled:           os.Getenv("AUTH_DISABLED") == "true",
		AppBaseURL:             os.Getenv("APP_BASE_URL"),
		NeonDBURL:              os.Getenv("DATABASE_URL"),
		LogLevel:               logLevel,
		GeminiAnalysisModel:    geminiAnalysisModel,
		GeminiTranslationModel: geminiTranslationModel,
	}
}

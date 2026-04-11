package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/ulf/paperless-aissist/internal/types"
)

const envFile = ".env"

// Load reads the .env file and returns an AppConfig.
func Load() (*types.AppConfig, error) {
	_ = godotenv.Overload(envFile) // Overload (not Load) to apply saved changes immediately

	interval, _ := strconv.Atoi(getEnv("SCAN_INTERVAL", "0"))
	strict := strings.EqualFold(getEnv("STRICT_METADATA", "false"), "true")
	logPrompts := strings.EqualFold(getEnv("LOG_PROMPTS", "false"), "true")

	return &types.AppConfig{
		OllamaHost:     getEnv("OLLAMA_HOST", "http://localhost:11434"),
		OllamaModel:    getEnv("OLLAMA_MODEL", "gemma4:latest"),
		PromptFile:     getEnv("PROMPT_FILE", "prompt.md"),
		PaperlessURL:   getEnv("PAPERLESS_BASE_URL", ""),
		PaperlessToken: getEnv("PAPERLESS_API_TOKEN", ""),
		Port:           getEnv("PORT", "8080"),
		ScanTag:        getEnv("SCAN_TAG", ""),
		ScanInterval:   interval,
		AITag:          getEnv("AI_TAG", "ai-gen"),
		StrictMetadata: strict,
		EmbedModel:     getEnv("EMBED_MODEL", "bge-m3:latest"),
		IndexPath:      getEnv("INDEX_PATH", "./data"),
		LogPrompts:     logPrompts,
	}, nil
}

// Save writes the config back to the .env file.
func Save(cfg *types.AppConfig) error {
	lines := []string{
		"OLLAMA_HOST=" + cfg.OllamaHost,
		"OLLAMA_MODEL=" + cfg.OllamaModel,
		"PROMPT_FILE=" + cfg.PromptFile,
		"PAPERLESS_BASE_URL=" + cfg.PaperlessURL,
		"PAPERLESS_API_TOKEN=" + cfg.PaperlessToken,
		"PORT=" + cfg.Port,
		"SCAN_TAG=" + cfg.ScanTag,
		fmt.Sprintf("SCAN_INTERVAL=%d", cfg.ScanInterval),
		"AI_TAG=" + cfg.AITag,
		fmt.Sprintf("STRICT_METADATA=%v", cfg.StrictMetadata),
		"EMBED_MODEL=" + cfg.EmbedModel,
		"INDEX_PATH=" + cfg.IndexPath,
		fmt.Sprintf("LOG_PROMPTS=%v", cfg.LogPrompts),
	}
	return os.WriteFile(envFile, []byte(strings.Join(lines, "\n")+"\n"), 0600)
}

// LoadPrompt reads the prompt template file.
func LoadPrompt(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SavePrompt writes the prompt template file.
func SavePrompt(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

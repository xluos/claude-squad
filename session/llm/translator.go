// Package llm provides LLM integration for translating session names.
package llm

import (
	"bytes"
	"claude-squad/config"
	"claude-squad/log"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	// DefaultTimeout is the default timeout for LLM API requests.
	DefaultTimeout = 10 * time.Second
	// MaxASCII is the maximum value for ASCII characters.
	MaxASCII = 127
)

// APIRequest represents the request body for the LLM API
type APIRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	EnableThinking bool      `json:"enable_thinking"`
	Temperature float32   `json:"temperature,omitempty"`
}

// Message represents a chat message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// APIResponse represents the response from the LLM API
type APIResponse struct {
	Choices []Choice `json:"choices"`
	Error   *APIError `json:"error,omitempty"`
}

// Choice represents a choice in the API response
type Choice struct {
	Message Message `json:"message"`
}

// APIError represents an error from the API
type APIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// TranslateToEnglishID translates a Chinese name to a semantic English identifier
// Returns a kebab-case English identifier suitable for git branch names
// Falls back to timestamp-based identifier if translation fails or LLM is not configured
func TranslateToEnglishID(chineseName string) (string, error) {
	// Fallback identifier in case of any failure
	fallback := fmt.Sprintf("session-%d", time.Now().Unix())

	// Load configuration
	cfg := config.LoadConfig()

	// Check if LLM is enabled and configured
	if !cfg.LLM.Enabled || cfg.LLM.APIKey == "" || cfg.LLM.Model == "" || cfg.LLM.BaseURL == "" {
		return fallback, fmt.Errorf("LLM not configured, using fallback")
	}

	// Prepare the prompt
	prompt := fmt.Sprintf(`你是一个专业的技术翻译助手。请将以下中文会话名称翻译为简洁的英文标识符，遵循以下规则：

1. 使用 kebab-case 格式（小写字母，单词间用短横线连接）
2. 保持语义化，能够反映原始中文的含义
3. 尽量简洁，控制在 3-5 个单词以内
4. 只返回翻译后的标识符，不要有任何其他说明文字

中文名称：%s

英文标识符：`, chineseName)

	// Prepare request body
	requestBody := APIRequest{
		Model:  cfg.LLM.Model,
		Stream: cfg.LLM.Stream, // Use configured stream setting
		EnableThinking: cfg.LLM.EnableThinking,
		Messages: []Message{
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return fallback, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", cfg.LLM.BaseURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fallback, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", cfg.LLM.APIKey))

	// Determine timeout value: use configured value if > 0, otherwise use default
	timeout := DefaultTimeout
	if cfg.LLM.Timeout > 0 {
		timeout = time.Duration(cfg.LLM.Timeout) * time.Second
	}

	// Make the request with timeout
	client := &http.Client{
		Timeout: timeout,
	}

	// Log the request body for debugging (JSON)
	if log.InfoLog != nil {
		// try to pretty-print; if that fails, log compact
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, jsonData, "", "  "); err == nil {
			log.InfoLog.Printf("LLM request body:\n%s", pretty.String())
		} else {
			log.InfoLog.Printf("LLM request body: %s", string(jsonData))
		}
	}

	// Time the client.Do call
	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if log.InfoLog != nil {
		log.InfoLog.Printf("client.Do elapsed: %s", elapsed.String())
	}
	if err != nil {
		return fallback, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fallback, fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return fallback, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return fallback, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Check for API errors
	if apiResp.Error != nil {
		return fallback, fmt.Errorf("API error: %s", apiResp.Error.Message)
	}

	// Extract the translated identifier
	if len(apiResp.Choices) == 0 {
		return fallback, fmt.Errorf("no choices in response")
	}

	translatedID := strings.TrimSpace(apiResp.Choices[0].Message.Content)

	// Sanitize the result to ensure it's valid
	sanitized := sanitizeIdentifier(translatedID)
	if sanitized == "" {
		return fallback, fmt.Errorf("translation resulted in empty identifier")
	}

	return sanitized, nil
}

// sanitizeIdentifier ensures the identifier is safe for git branch names.
// - Converts to lowercase
// - Replaces spaces with hyphens
// - Removes any characters not in [a-z0-9-_]
// - Removes leading/trailing hyphens
func sanitizeIdentifier(s string) string {
	// Convert to lowercase
	s = strings.ToLower(s)

	// Replace spaces with hyphens
	s = strings.ReplaceAll(s, " ", "-")

	// Remove any characters not in the safe set
	re := regexp.MustCompile(`[^a-z0-9\-_]+`)
	s = re.ReplaceAllString(s, "")

	// Replace multiple hyphens with single hyphen
	reDash := regexp.MustCompile(`-+`)
	s = reDash.ReplaceAllString(s, "-")

	// Trim leading/trailing hyphens.
	s = strings.Trim(s, "-")

	return s
}

// HasNonASCII checks if a string contains non-ASCII characters.
func HasNonASCII(s string) bool {
	for _, r := range s {
		if r > MaxASCII {
			return true
		}
	}
	return false
}

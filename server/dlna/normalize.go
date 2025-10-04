package dlna

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"server/settings"
)

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatRequest struct {
	Model     string              `json:"model"`
	Messages  []openAIChatMessage `json:"messages"`
	MaxTokens int                 `json:"max_tokens"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message openAIChatMessage `json:"message"`
	} `json:"choices"`
}

func normalizeTitle(path string) string {
	apiKey, model := settings.GetOpenAIConfig()
	if apiKey == "" || model == "" {
		return path
	}
	prompt := fmt.Sprintf("Normalize the following file name into an Infuse-compatible title. For movies use 'Movie Title (Year)'. For TV episodes use 'Show Title SXXEYY'. Return only the normalized title without extension. File name: %s", path)
	reqBody := openAIChatRequest{
		Model: model,
		Messages: []openAIChatMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens: 50,
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		return path
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return path
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return path
	}
	defer resp.Body.Close()

	var respBody openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return path
	}
	if len(respBody.Choices) > 0 {
		title := strings.TrimSpace(respBody.Choices[0].Message.Content)
		if title != "" {
			return title
		}
	}
	return path
}

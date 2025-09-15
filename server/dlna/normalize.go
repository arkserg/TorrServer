package dlna

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"server/log"
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
	if settings.BTsets.EnableDebug {
		log.TLogln("normalizeTitle: input", path)
	}

	if cached := settings.GetDLNATitle(path); cached != "" {
		if settings.BTsets.EnableDebug {
			log.TLogln("normalizeTitle: cache hit", cached)
		}
		return cached
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	model := os.Getenv("OPENAI_MODEL")
	if apiKey == "" || model == "" {
		if settings.BTsets.EnableDebug {
			log.TLogln("normalizeTitle: missing API key or model, returning original")
		}
		return path
	}

	prompt := fmt.Sprintf("Normalize the following file name into an Infuse-compatible title. For movies use 'Movie Title (Year)'. For TV episodes use 'Show Title SXXEYY'. Return only the normalized title without extension. File name: %s", path)
	if settings.BTsets.EnableDebug {
		log.TLogln("normalizeTitle: prompt", prompt)
	}
	reqBody := openAIChatRequest{
		Model: model,
		Messages: []openAIChatMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens: 50,
	}
	buf, err := json.Marshal(reqBody)
	if err != nil {
		if settings.BTsets.EnableDebug {
			log.TLogln("normalizeTitle: marshal request failed", err)
		}
		return path
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(buf))
	if err != nil {
		if settings.BTsets.EnableDebug {
			log.TLogln("normalizeTitle: create request failed", err)
		}
		return path
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if settings.BTsets.EnableDebug {
			log.TLogln("normalizeTitle: request failed", err)
		}
		return path
	}
	defer resp.Body.Close()

	if settings.BTsets.EnableDebug {
		log.TLogln("normalizeTitle: response status", resp.Status)
	}

	var respBody openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		if settings.BTsets.EnableDebug {
			log.TLogln("normalizeTitle: decode response failed", err)
		}
		return path
	}
	if len(respBody.Choices) > 0 {
		title := strings.TrimSpace(respBody.Choices[0].Message.Content)
		if settings.BTsets.EnableDebug {
			log.TLogln("normalizeTitle: normalized title", title)
		}
		if title != "" {
			settings.SetDLNATitle(path, title)
			return title
		}
	} else if settings.BTsets.EnableDebug {
		log.TLogln("normalizeTitle: no choices in response")
	}

	if settings.BTsets.EnableDebug {
		log.TLogln("normalizeTitle: returning original path")
	}
	return path
}

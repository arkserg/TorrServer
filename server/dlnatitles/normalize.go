package dlnatitles

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
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

var ensureLocks sync.Map

// EnsureTorrent prepares DLNA titles for all provided torrent files in a single batch.
// It skips regeneration when a bucket for the torrent already exists and guards concurrent
// workers with a per-torrent mutex to avoid races.
func EnsureTorrent(hashHex string, paths []string) {
	if settings.BTsets.EnableDebug {
		log.TLogln("dlnatitles.EnsureTorrent: input", hashHex, len(paths))
	}

	hashHex = strings.ToLower(strings.TrimSpace(hashHex))
	if hashHex == "" || len(paths) == 0 {
		return
	}

	uniquePaths := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		uniquePaths = append(uniquePaths, path)
	}
	if len(uniquePaths) == 0 {
		return
	}

	unlock := lockTorrent(hashHex)
	defer unlock()

	if settings.HasDLNATitleBucket(hashHex) {
		if settings.BTsets.EnableDebug {
			log.TLogln("dlnatitles.EnsureTorrent: bucket already exists")
		}
		return
	}

	titles := make(map[string]string, len(uniquePaths))
	for _, path := range uniquePaths {
		title, err := generateNormalizedTitle(path)
		if err != nil && settings.BTsets.EnableDebug {
			log.TLogln("dlnatitles.EnsureTorrent: generation failed", err)
		}
		title = strings.TrimSpace(title)
		if title == "" {
			title = path
		}
		titles[path] = title
		if settings.BTsets.EnableDebug {
			log.TLogln("dlnatitles.EnsureTorrent: prepared title", path, "->", title)
		}
	}

	if len(titles) == 0 {
		return
	}

	if settings.HasDLNATitleBucket(hashHex) {
		if settings.BTsets.EnableDebug {
			log.TLogln("dlnatitles.EnsureTorrent: bucket appeared during generation, skipping store")
		}
		return
	}

	settings.StoreDLNATitles(hashHex, titles)
	if settings.BTsets.EnableDebug {
		log.TLogln("dlnatitles.EnsureTorrent: stored titles", len(titles))
	}
}

func lockTorrent(hashHex string) func() {
	if hashHex == "" {
		return func() {}
	}
	muIface, _ := ensureLocks.LoadOrStore(hashHex, &sync.Mutex{})
	mu := muIface.(*sync.Mutex)
	mu.Lock()
	return func() {
		mu.Unlock()
		ensureLocks.Delete(hashHex)
	}
}

// Lookup returns the cached DLNA title for the given torrent file or falls back to the original path.
func Lookup(hashHex, path string) string {
	if settings.BTsets.EnableDebug {
		log.TLogln("dlnatitles.Lookup: input", hashHex, path)
	}

	if cached := settings.GetDLNATitle(hashHex, path); cached != "" {
		if settings.BTsets.EnableDebug {
			log.TLogln("dlnatitles.Lookup: cache hit", cached)
		}
		return cached
	}

	if settings.BTsets.EnableDebug {
		log.TLogln("dlnatitles.Lookup: fallback to original path")
	}
	return path
}

func generateNormalizedTitle(path string) (string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	model := os.Getenv("OPENAI_MODEL")
	if apiKey == "" || model == "" {
		if settings.BTsets.EnableDebug {
			log.TLogln("dlnatitles.generate: missing API key or model")
		}
		return path, fmt.Errorf("openai configuration is not set")
	}

	prompt := fmt.Sprintf("Normalize the following file name into an Infuse-compatible title. For movies use 'Movie Title (Year)'. For TV episodes use 'Show Title SXXEYY'. Return only the normalized title without extension. File name: %s", path)
	if settings.BTsets.EnableDebug {
		log.TLogln("dlnatitles.generate: prompt", prompt)
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
			log.TLogln("dlnatitles.generate: marshal request failed", err)
		}
		return path, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(buf))
	if err != nil {
		if settings.BTsets.EnableDebug {
			log.TLogln("dlnatitles.generate: create request failed", err)
		}
		return path, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if settings.BTsets.EnableDebug {
			log.TLogln("dlnatitles.generate: request failed", err)
		}
		return path, err
	}
	defer resp.Body.Close()

	if settings.BTsets.EnableDebug {
		log.TLogln("dlnatitles.generate: response status", resp.Status)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return path, fmt.Errorf("openai returned status %s", resp.Status)
	}

	var respBody openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		if settings.BTsets.EnableDebug {
			log.TLogln("dlnatitles.generate: decode response failed", err)
		}
		return path, err
	}
	if len(respBody.Choices) > 0 {
		title := strings.TrimSpace(respBody.Choices[0].Message.Content)
		if settings.BTsets.EnableDebug {
			log.TLogln("dlnatitles.generate: normalized title", title)
		}
		if title != "" {
			return title, nil
		}
	} else if settings.BTsets.EnableDebug {
		log.TLogln("dlnatitles.generate: no choices in response")
	}

	return path, fmt.Errorf("openai returned empty title")
}

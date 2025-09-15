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
	enableDebug := settings.BTsets != nil && settings.BTsets.EnableDebug
	if enableDebug {
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
		if enableDebug {
			log.TLogln("dlnatitles.EnsureTorrent: bucket already exists")
		}
		return
	}

	workers := settings.DefaultDLNATitleWorkers
	if settings.BTsets != nil && settings.BTsets.DLNATitleWorkers > 0 {
		workers = settings.BTsets.DLNATitleWorkers
	}
	if workers <= 0 {
		workers = settings.DefaultDLNATitleWorkers
	}

	titles := make(map[string]string, len(uniquePaths))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, path := range uniquePaths {
		path := path
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			title, err := generateNormalizedTitle(path)
			if err != nil && enableDebug {
				log.TLogln("dlnatitles.EnsureTorrent: generation failed", err)
			}
			title = strings.TrimSpace(title)
			if title == "" {
				title = path
			}

			mu.Lock()
			titles[path] = title
			mu.Unlock()

			if enableDebug {
				log.TLogln("dlnatitles.EnsureTorrent: prepared title", path, "->", title)
			}
		}()
	}

	wg.Wait()

	if len(titles) == 0 {
		return
	}

	if settings.HasDLNATitleBucket(hashHex) {
		if enableDebug {
			log.TLogln("dlnatitles.EnsureTorrent: bucket appeared during generation, skipping store")
		}
		return
	}

	settings.StoreDLNATitles(hashHex, titles)
	if enableDebug {
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
	enableDebug := settings.BTsets != nil && settings.BTsets.EnableDebug
	if apiKey == "" || model == "" {
		if enableDebug {
			log.TLogln("dlnatitles.generate: missing API key or model")
		}
		return path, fmt.Errorf("openai configuration is not set")
	}

	prompt := fmt.Sprintf("Normalize the following file name into an Infuse-compatible title. For movies use 'Movie Title (Year)'. For TV episodes use 'Show Title SXXEYY'. Return only the normalized title without extension. File name: %s", path)
	if enableDebug {
		log.TLogln("dlnatitles.generate: prompt", prompt)
	}

	reqBody := openAIChatRequest{
		Model: model,
		Messages: []openAIChatMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens: 50,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		if enableDebug {
			log.TLogln("dlnatitles.generate: marshal request failed", err)
		}
		return path, err
	}

	first, err := requestNormalizedTitle(apiKey, payload, 1, enableDebug)
	if err != nil {
		return path, err
	}

	second, err := requestNormalizedTitle(apiKey, payload, 2, enableDebug)
	if err != nil {
		return path, err
	}
	if first == second {
		return first, nil
	}

	third, err := requestNormalizedTitle(apiKey, payload, 3, enableDebug)
	if err != nil {
		return path, err
	}
	if third == first {
		return first, nil
	}
	if third == second {
		return second, nil
	}

	log.TLogln("WARNING dlnatitles.generate: inconsistent normalization responses", path, first, second, third)
	return path, fmt.Errorf("openai returned inconsistent titles")
}

func requestNormalizedTitle(apiKey string, payload []byte, attempt int, enableDebug bool) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		if enableDebug {
			log.TLogln("dlnatitles.generate: create request failed", err)
		}
		return "", fmt.Errorf("attempt %d: create request failed: %w", attempt, err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if enableDebug {
			log.TLogln("dlnatitles.generate: request failed", err)
		}
		return "", fmt.Errorf("attempt %d: request failed: %w", attempt, err)
	}
	defer resp.Body.Close()

	if enableDebug {
		log.TLogln("dlnatitles.generate: attempt", attempt, "response status", resp.Status)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("attempt %d: openai returned status %s", attempt, resp.Status)
	}

	var respBody openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		if enableDebug {
			log.TLogln("dlnatitles.generate: decode response failed", err)
		}
		return "", fmt.Errorf("attempt %d: decode response failed: %w", attempt, err)
	}

	if len(respBody.Choices) == 0 {
		if enableDebug {
			log.TLogln("dlnatitles.generate: attempt", attempt, "no choices in response")
		}
		return "", fmt.Errorf("attempt %d: openai returned empty title", attempt)
	}

	title := strings.TrimSpace(respBody.Choices[0].Message.Content)
	if enableDebug {
		log.TLogln("dlnatitles.generate: attempt", attempt, "normalized title", title)
	}
	if title == "" {
		return "", fmt.Errorf("attempt %d: openai returned empty title", attempt)
	}

	return title, nil
}

package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	BufferBytes = 32 * 1024 // 32KB buffer
	UserAgent   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// Global HTTP client with connection pooling
var httpClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	},
}

// StreamProxy handles range request proxying to any target URL with custom headers
func StreamProxy(w http.ResponseWriter, r *http.Request, targetURL string, headers map[string]string) error {
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Hour) // Support long streams
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create proxy request: %w", err)
	}

	// Copy range headers from client
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	if ifRangeHeader := r.Header.Get("If-Range"); ifRangeHeader != "" {
		req.Header.Set("If-Range", ifRangeHeader)
	}

	// Apply custom headers (e.g. Auth tokens, User-Agent, Referer)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// Default user agent if not overridden
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", UserAgent)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute target request: %w", err)
	}
	defer resp.Body.Close()

	// If the upstream returns an error status, do not proxy it as a valid stream
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upstream returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Copy headers from target response back to our client
	copyHeader(w.Header(), resp.Header, "Content-Range")
	copyHeader(w.Header(), resp.Header, "Content-Length")
	copyHeader(w.Header(), resp.Header, "Content-Type")
	copyHeader(w.Header(), resp.Header, "Accept-Ranges")
	copyHeader(w.Header(), resp.Header, "Cache-Control")
	copyHeader(w.Header(), resp.Header, "ETag")

	// Set status code
	w.WriteHeader(resp.StatusCode)

	// Stream response body to client with fixed buffer
	buf := make([]byte, BufferBytes)
	_, err = io.CopyBuffer(w, resp.Body, buf)
	if err != nil {
		// Log or suppress client disconnect errors
		return fmt.Errorf("error during stream piping: %w", err)
	}

	return nil
}

func copyHeader(dst, src http.Header, key string) {
	if val := src.Get(key); val != "" {
		dst.Set(key, val)
	}
}

// ExtractTeraboxStreamURL extracts the direct video streaming link from a Terabox share link
func ExtractTeraboxStreamURL(shareURL string) (string, error) {
	parsedURL, err := url.Parse(shareURL)
	if err != nil {
		return "", fmt.Errorf("invalid terabox url: %w", err)
	}

	surl := parsedURL.Query().Get("surl")
	if surl == "" && strings.Contains(parsedURL.Path, "/s/") {
		parts := strings.Split(parsedURL.Path, "/s/")
		if len(parts) > 1 {
			surl = parts[1]
			// Trim starting '1' which is common in short URLs but might or might not be needed depending on API
			if strings.HasPrefix(surl, "1") && len(surl) > 1 {
				surl = surl[1:]
			}
		}
	}

	if surl == "" {
		return "", fmt.Errorf("could not extract surl parameter from Terabox link")
	}

	// We hit the Terabox share page
	targetPage := fmt.Sprintf("https://www.terabox.com/sharing/link?surl=%s", url.QueryEscape(surl))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", targetPage, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,xml;q=0.9,image/webp,*/*;q=0.8")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Terabox page: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read Terabox page: %w", err)
	}

	bodyStr := string(bodyBytes)

	// Search for dlink or download url in the page javascript
	reDlink := regexp.MustCompile(`"dlink"\s*:\s*"([^"]+)"`)
	matches := reDlink.FindStringSubmatch(bodyStr)
	if len(matches) > 1 {
		dlink := matches[1]
		dlink = strings.ReplaceAll(dlink, `\/`, `/`)
		dlink = strings.ReplaceAll(dlink, `\u0026`, `&`)
		return dlink, nil
	}

	reLocals := regexp.MustCompile(`window\.__initialData\s*=\s*(\{.*?\});`)
	localsMatches := reLocals.FindStringSubmatch(bodyStr)
	if len(localsMatches) > 1 {
		localsData := localsMatches[1]
		if idx := strings.Index(localsData, `"dlink"`); idx != -1 {
			subData := localsData[idx:]
			endIdx := strings.Index(subData, `,`)
			if endIdx != -1 {
				rawDlink := subData[:endIdx]
				rawDlink = strings.Trim(strings.SplitN(rawDlink, ":", 2)[1], ` "`)
				rawDlink = strings.ReplaceAll(rawDlink, `\/`, `/`)
				rawDlink = strings.ReplaceAll(rawDlink, `\u0026`, `&`)
				return rawDlink, nil
			}
		}
	}

	reFileList := regexp.MustCompile(`"file_list"\s*:\s*\[\s*\{\s*[^\]]*"dlink"\s*:\s*"([^"]+)"`)
	fileListMatches := reFileList.FindStringSubmatch(bodyStr)
	if len(fileListMatches) > 1 {
		dlink := fileListMatches[1]
		dlink = strings.ReplaceAll(dlink, `\/`, `/`)
		dlink = strings.ReplaceAll(dlink, `\u0026`, `&`)
		return dlink, nil
	}

	return "", fmt.Errorf("could not find streaming link in Terabox page HTML structure")
}

// SubProxy fetches a subtitle file (like SRT), converts it to VTT if necessary, and serves it
func SubProxy(w http.ResponseWriter, r *http.Request, targetURL string, headers map[string]string) error {
	ctx, cancel := context.WithTimeout(r.Context(), 1*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create proxy request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute target request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("upstream returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read subtitle body: %w", err)
	}

	// SRT to VTT on the fly conversion
	content := string(body)
	if !strings.HasPrefix(content, "WEBVTT") {
		// Replace commas with periods in timestamps
		re := regexp.MustCompile(`(\d{2}:\d{2}:\d{2}),(\d{3})`)
		content = re.ReplaceAllString(content, "$1.$2")
		content = "WEBVTT\n\n" + content
	}

	w.Header().Set("Content-Type", "text/vtt")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(content))
	return nil
}

// AudioRemuxProxy pipes the upstream stream through ffmpeg to select a specific audio track
func AudioRemuxProxy(w http.ResponseWriter, r *http.Request, targetURL string, headers map[string]string, audioTrack string) error {
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Hour)
	defer cancel()

	// Extract offset if it's a range request - FFMPEG handles seek
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		// FFMPEG seek for HTTP isn't byte-based easily if we pass URL to FFMPEG.
		// Actually, if we pass the URL directly to ffmpeg, it will fetch it. 
		// BUT Video.js expects byte ranges, FFMPEG outputs a live stream!
		// It's better to just output a continuous stream without Accept-Ranges for remuxes.
	}

	// FFMPEG args
	args := []string{
		"-v", "quiet",
	}

	// Add auth headers for input
	if authHeader, ok := headers["Authorization"]; ok {
		args = append(args, "-headers", "Authorization: "+authHeader+"\r\n")
	}

	args = append(args,
		"-i", targetURL,
		"-map", "0:v:0", // default video
		"-map", fmt.Sprintf("0:a:%s", audioTrack), // selected audio
		"-c", "copy",
		"-f", "matroska", // matroska is streamable and supports any codec
		"pipe:1",
	)

	cmd := exec.CommandContext(ctx, GetExecutablePath("ffmpeg"), args...)

	w.Header().Set("Content-Type", "video/x-matroska")
	w.Header().Set("Accept-Ranges", "none") // disable seeking for live FFMPEG remux for now
	w.WriteHeader(http.StatusOK)

	// Pipe FFMPEG stdout directly to HTTP ResponseWriter
	cmd.Stdout = w
	cmd.Stderr = nil // ignore stderr

	err := cmd.Run()
	if err != nil {
		fmt.Println("ffmpeg error:", err)
		return fmt.Errorf("ffmpeg stream failed: %w", err)
	}

	return nil
}

// GetExecutablePath safely resolves ffmpeg/ffprobe binary paths (local vs PATH)
func GetExecutablePath(base string) string {
	pwd, err := os.Getwd()
	if err == nil {
		path := filepath.Join(pwd, base)
		if _, err := os.Stat(path); err == nil {
			return path
		}
		if _, err := os.Stat(path + ".exe"); err == nil {
			return path + ".exe"
		}
	}
	return base // fallback to OS PATH
}

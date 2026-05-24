package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	BufferBytes = 32 * 1024 // 32KB buffer
	UserAgent   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

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

	client := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: false,
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute target request: %w", err)
	}
	defer resp.Body.Close()

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
	// Terabox link format: https://www.terabox.com/sharing/link?surl=XYZ OR https://terabox.com/s/1XYZ
	// We extract the surl key
	surl := ""
	if strings.Contains(shareURL, "surl=") {
		parts := strings.Split(shareURL, "surl=")
		if len(parts) > 1 {
			surl = strings.Split(parts[1], "&")[0]
		}
	} else if strings.Contains(shareURL, "/s/") {
		parts := strings.Split(shareURL, "/s/")
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
	targetPage := fmt.Sprintf("https://www.terabox.com/sharing/link?surl=%s", surl)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", targetPage, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,xml;q=0.9,image/webp,*/*;q=0.8")

	client := &http.Client{}
	resp, err := client.Do(req)
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
	// Example pattern: "dlink":"https:\/\/..." or "dlink":"https:..."
	reDlink := regexp.MustCompile(`"dlink"\s*:\s*"([^"]+)"`)
	matches := reDlink.FindStringSubmatch(bodyStr)
	if len(matches) > 1 {
		dlink := matches[1]
		// Unescape JSON slashes (\/)
		dlink = strings.ReplaceAll(dlink, `\/`, `/`)
		// Replace unicodes if any
		dlink = strings.ReplaceAll(dlink, `\u0026`, `&`)
		return dlink, nil
	}

	// Fallback to checking the share list api (this matches Terabox internal patterns)
	// Some pages structure using window.locals or __initialData
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

	// Try searching for file_list containing dlinks
	reFileList := regexp.MustCompile(`"file_list"\s*:\s*\[\s*\{\s*[^\]]*"dlink"\s*:\s*"([^"]+)"`)
	fileListMatches := reFileList.FindStringSubmatch(bodyStr)
	if len(fileListMatches) > 1 {
		dlink := fileListMatches[1]
		dlink = strings.ReplaceAll(dlink, `\/`, `/`)
		dlink = strings.ReplaceAll(dlink, `\u0026`, `&`)
		return dlink, nil
	}

	// If scraping failed, check if the direct download URL pattern is accessible or return error
	return "", fmt.Errorf("could not find streaming link in Terabox page HTML structure")
}

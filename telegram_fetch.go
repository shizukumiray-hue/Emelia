package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// fetchTelegramProxies fetches proxies from Telegram channel via web preview
func fetchTelegramProxies(channelUsername string, outputFile string) error {
	// First, check if channel has a linked proxy URL
	proxyURL := ""
	
	// Try to find proxy URL from channel page
	channelURL := fmt.Sprintf("https://t.me/s/%s", channelUsername)
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	
	req, err := http.NewRequest("GET", channelURL, nil)
	if err == nil {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == 200 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB cap
			resp.Body.Close()
			
			// Look for common proxy list patterns
			htmlContent := string(body)
			if strings.Contains(htmlContent, "zip.cm.edu.kg/all.txt") {
				proxyURL = "https://zip.cm.edu.kg/all.txt"
			} else if strings.Contains(htmlContent, "zip.cm.edu.kg/all.json") {
				proxyURL = "https://zip.cm.edu.kg/all.json"
			}
		}
	}
	
	var proxies []string
	
	// If we found a direct proxy URL, fetch from there
	if proxyURL != "" {
		fmt.Printf("📥 Found proxy list URL: %s\n", proxyURL)
		proxies, err = fetchFromURL(proxyURL, client)
		if err != nil {
			fmt.Printf("⚠️  Failed to fetch from URL: %v\n", err)
		}
	}
	
	// Fallback: parse HTML content
	if len(proxies) == 0 {
		fmt.Println("🔍 Parsing HTML content...")
		req, _ := http.NewRequest("GET", channelURL, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to fetch channel: %v", err)
		}
		defer resp.Body.Close()
		
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB cap
		proxies = parseProxiesFromHTML(string(body))
	}
	
	if len(proxies) == 0 {
		return fmt.Errorf("no proxies found in channel")
	}
	
	// Write to file
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer file.Close()
	
	writer := bufio.NewWriter(file)
	for _, proxy := range proxies {
		writer.WriteString(proxy + "\n")
	}
	writer.Flush()
	
	fmt.Printf("✅ Fetched %d proxies from Telegram channel\n", len(proxies))
	return nil
}

// fetchFromURL fetches proxies from a direct URL
func fetchFromURL(url string, client *http.Client) ([]string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status code: %d", resp.StatusCode)
	}
	
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10MB cap
	if err != nil {
		return nil, err
	}
	
	content := string(body)
	var proxies []string
	seen := make(map[string]bool)
	
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		
		// Parse various formats
		proxy := parseProxyLine(line)
		if proxy == "" {
			continue
		}
		
		// Deduplicate by IP:PORT
		parts := strings.Split(proxy, ",")
		if len(parts) >= 2 {
			key := parts[0] + ":" + parts[1]
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		
		proxies = append(proxies, proxy)
	}
	
	return proxies, nil
}

// parseProxyLine parses a single proxy line into CSV format
func parseProxyLine(line string) string {
	// Format 1: IP:PORT#CC
	if matches := regexp.MustCompile(`^(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}):(\d{2,5})#([A-Z]{2})$`).FindStringSubmatch(line); matches != nil {
		if !isValidIPFormat(matches[1]) {
			return ""
		}
		portNum, err := strconv.Atoi(matches[2])
		if err != nil || portNum < 1 || portNum > 65535 {
			return ""
		}
		return fmt.Sprintf("%s,%s,%s,", matches[1], matches[2], matches[3])
	}
	
	// Format 2: IP,PORT,CC,ORG
	if matches := regexp.MustCompile(`^(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}),(\d{2,5}),([A-Z]{2}),(.+)$`).FindStringSubmatch(line); matches != nil {
		if !isValidIPFormat(matches[1]) {
			return ""
		}
		portNum, err := strconv.Atoi(matches[2])
		if err != nil || portNum < 1 || portNum > 65535 {
			return ""
		}
		return fmt.Sprintf("%s,%s,%s,%s", matches[1], matches[2], matches[3], strings.TrimSpace(matches[4]))
	}
	
	// Format 3: IP,PORT,CC
	if matches := regexp.MustCompile(`^(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}),(\d{2,5}),([A-Z]{2})$`).FindStringSubmatch(line); matches != nil {
		if !isValidIPFormat(matches[1]) {
			return ""
		}
		portNum, err := strconv.Atoi(matches[2])
		if err != nil || portNum < 1 || portNum > 65535 {
			return ""
		}
		return fmt.Sprintf("%s,%s,%s,", matches[1], matches[2], matches[3])
	}
	
	// Format 4: IP,PORT
	if matches := regexp.MustCompile(`^(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}),(\d{2,5})$`).FindStringSubmatch(line); matches != nil {
		if !isValidIPFormat(matches[1]) {
			return ""
		}
		portNum, err := strconv.Atoi(matches[2])
		if err != nil || portNum < 1 || portNum > 65535 {
			return ""
		}
		return fmt.Sprintf("%s,%s,,", matches[1], matches[2])
	}
	
	// Format 5: IP:PORT
	if matches := regexp.MustCompile(`^(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}):(\d{2,5})$`).FindStringSubmatch(line); matches != nil {
		if !isValidIPFormat(matches[1]) {
			return ""
		}
		portNum, err := strconv.Atoi(matches[2])
		if err != nil || portNum < 1 || portNum > 65535 {
			return ""
		}
		return fmt.Sprintf("%s,%s,,", matches[1], matches[2])
	}
	
	return ""
}

// parseProxiesFromHTML extracts proxy entries from HTML content
func parseProxiesFromHTML(html string) []string {
	var proxies []string
	seen := make(map[string]bool)
	
	// Patterns for proxy formats
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}):(\d{2,5})#([A-Z]{2})\b`),           // IP:PORT#CC
		regexp.MustCompile(`\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}),(\d{2,5}),([A-Z]{2}),([^<\n]+)\b`), // IP,PORT,CC,ORG
		regexp.MustCompile(`\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}),(\d{2,5}),([A-Z]{2})\b`),           // IP,PORT,CC
		regexp.MustCompile(`\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}),(\d{2,5})\b`),                      // IP,PORT
	}
	
	lines := strings.Split(html, "\n")
	for _, line := range lines {
		// Clean HTML entities
		line = strings.ReplaceAll(line, "&nbsp;", " ")
		line = strings.ReplaceAll(line, "&quot;", "\"")
		
		for _, pattern := range patterns {
			matches := pattern.FindAllStringSubmatch(line, -1)
			for _, match := range matches {
				if len(match) < 3 {
					continue
				}
				
				ip := match[1]
				port := match[2]
				
				// Validate IP format
				if !isValidIPFormat(ip) {
					continue
				}
				
				// Validate port range
				portNum, err := strconv.Atoi(port)
				if err != nil || portNum < 1 || portNum > 65535 {
					continue
				}
				
				key := ip + ":" + port
				if seen[key] {
					continue
				}
				seen[key] = true
				
				// Format as CSV
				var proxyLine string
				if len(match) >= 5 && match[4] != "" {
					// IP,PORT,CC,ORG
					proxyLine = fmt.Sprintf("%s,%s,%s,%s", ip, port, match[3], strings.TrimSpace(match[4]))
				} else if len(match) >= 4 && match[3] != "" {
					// IP,PORT,CC
					proxyLine = fmt.Sprintf("%s,%s,%s,", ip, port, match[3])
				} else {
					// IP,PORT
					proxyLine = fmt.Sprintf("%s,%s,,", ip, port)
				}
				
				proxies = append(proxies, proxyLine)
			}
		}
	}
	
	return proxies
}

// isValidIPFormat checks basic IP format
func isValidIPFormat(ip string) bool {
	return net.ParseIP(ip) != nil
}

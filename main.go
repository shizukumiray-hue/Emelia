package main

import (
	"bufio"
	"crypto/tls"
	"emilia/useragent"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// === KONFIGURASI ===
const (
	Debug         = false
	TimeoutSec    = 5
	MaxConcurrent = 200
)

// Global variable, akan diisi otomatis dari Env/Secret
var workerURLs []string

// Statistik kegagalan per layer (untuk diagnosa)
var (
	statDialErr   int32
	statTLSErr    int32
	statHTTPErr   int32
	statNon200Err int32
)

const (
	TraceURL  = "https://1.1.1.1/cdn-cgi/trace"
	AwsURL    = "https://checkip.amazonaws.com"
	FileInput = "Data/proxy-rabu-16-agustus.txt"
	FileAlive = "Data/alive.txt"
)

var regexOrg = regexp.MustCompile(`[^a-zA-Z0-9\s]`)

// === STRUKTUR DATA ===
type WorkerResponse struct {
	IP      string `json:"ip"`
	Org     string `json:"as_organization"`
	Country string `json:"country"`
	City    string `json:"city"`
}

type ProxyInput struct {
	IP       string
	Port     string
	Country  string
	OrgInput string
}

type ValidProxy struct {
	IP      string
	Port    string
	Country string
	Org     string
	City    string
	Source  string
}

type CheckResult struct {
	Valid bool
	Data  *ValidProxy
}

type Stats struct {
	Total   int32
	Live    int32
	Checked int32
}

// === FUNGSI UTAMA ===
func main() {
	limit := flag.Int("limit", 0, "batasi jumlah API check (0 = semua, >0 = mode tes tanpa tulis file)")
	inputFile := flag.String("input", FileInput, "file input proxy untuk di-scan")
	fetchChannel := flag.String("fetch", "", "fetch proxies from Telegram channel (e.g., zip_cm_edu_kg)")
	outputFetch := flag.String("output", "Data/telegram_fetch.txt", "output file for fetched proxies")
	flag.Parse()
	
	// Mode: Fetch from Telegram
	if *fetchChannel != "" {
		fmt.Println("==========================================")
		fmt.Println("   TELEGRAM PROXY FETCHER")
		fmt.Println("==========================================")
		fmt.Printf("📡 Fetching from channel: @%s\n", *fetchChannel)
		
		if err := fetchTelegramProxies(*fetchChannel, *outputFetch); err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			os.Exit(1)
		}
		
		fmt.Printf("✅ Saved to: %s\n", *outputFetch)
		return
	}

	// Buat folder Data dengan permission yang aman
	if err := os.MkdirAll("Data", 0750); err != nil { // ✅ 0750 bukan 0777
		fmt.Printf("❌ Gagal membuat folder Data: %v\n", err)
		return
	}

	fmt.Println("==========================================")
	fmt.Println("   GOLANG SOCKET SCANNER (SECURE EDITION)")
	fmt.Printf("   Debug Mode: %v\n", Debug)
	fmt.Println("==========================================")

	// 0. LOAD CONFIG (SECURE)
	if !loadConfig() {
		return
	}

	// 1. DAPATKAN IP ASLI
	fmt.Print("🔍 Mendapatkan IP Asli... ")
	realIP, err := getPublicIPDirect()
	if err != nil {
		fmt.Printf("\n⚠️  Warning: %v (lanjut dengan validasi IP asli saja)\n", err)
		realIP = ""
	}
	if realIP != "" {
		fmt.Printf("%s\n\n", realIP)
	} else {
		fmt.Println("N/A (skip validation)")
	}

	// 2. BACA FILE INPUT
	proxies, err := readInputFile(*inputFile)
	if err != nil {
		fmt.Printf("❌ Error membaca file input: %v\n", err)
		return
	}
	fmt.Printf("📂 Total Proxy Loaded: %d\n", len(proxies))
	if len(proxies) == 0 {
		fmt.Println("❌ Tidak ada proxy untuk di-scan.")
		return
	}
	if *limit > 0 && len(proxies) > *limit {
		proxies = proxies[:*limit]
		fmt.Printf("🧪 Mode tes: hanya %d proxy pertama (file tidak ditulis)\n", *limit)
	}
	fmt.Println("🚀 Memulai scan socket parallel, Mohon tunggu.")

	// 3. SCANNING
	stats := &Stats{Total: int32(len(proxies))}
	resultsChan := make(chan CheckResult, len(proxies))

	var wg sync.WaitGroup
	sem := make(chan struct{}, MaxConcurrent)

	// Progress monitor
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	done := make(chan bool)
	go progressMonitor(ticker, done, stats)

	for _, p := range proxies {
		wg.Add(1)
		sem <- struct{}{}

		go func(proxy ProxyInput) {
			defer wg.Done()
			defer func() { <-sem }()

			res := checkProxyManualSocket(proxy, realIP)
			atomic.AddInt32(&stats.Checked, 1)

			if res.Valid {
				atomic.AddInt32(&stats.Live, 1)
				if Debug {
					locInfo := res.Data.Country
					if res.Data.City != "" {
						locInfo = fmt.Sprintf("%s-%s", res.Data.Country, res.Data.City)
					}
					fmt.Printf("\n✅ LIVE: %s:%s | %s | %s | %s",
						res.Data.IP, res.Data.Port, locInfo, res.Data.Org, res.Data.Source)
				}
			}

			resultsChan <- res
		}(p)
	}

	wg.Wait()
	close(done)
	close(resultsChan)

	// 4. SORTING & SAVING
	fmt.Println("\n\n🏁 Scanning selesai. Menyimpan hasil.")
	fmt.Printf("📊 Diagnosa: dial gagal=%d | TLS gagal=%d | HTTP gagal=%d | non-200=%d\n",
		atomic.LoadInt32(&statDialErr), atomic.LoadInt32(&statTLSErr),
		atomic.LoadInt32(&statHTTPErr), atomic.LoadInt32(&statNon200Err))

	var validProxies []ValidProxy
	for res := range resultsChan {
		if res.Valid && res.Data != nil {
			validProxies = append(validProxies, *res.Data)
		}
	}

	saveResults(validProxies, *limit)
}

// === FUNGSI SECURITY & CONFIG ===
func loadConfig() bool {
	// Coba baca dari file .env lokal terlebih dahulu
	file, err := os.Open(".env")
	if err == nil {
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				value = strings.Trim(value, `"'`)
				os.Setenv(key, value)
			}
		}
	}

	// Ambil URL dari Environment Variable
	envURLs := os.Getenv("WORKER_URLS")
	if envURLs == "" {
		// Fallback: baca dari keys.json (hardcode)
		envURLs = readKeysFromJSON()
	}
	if envURLs == "" {
		fmt.Println("❌ ERROR: WORKER_URLS tidak ditemukan!")
		fmt.Println("\n📝 Cara setup:")
		fmt.Println("   1. Lokal: Buat file .env dengan isi:")
		fmt.Println("      WORKER_URLS=https://url1.com,https://url2.com")
		fmt.Println("      atau hardcode di keys.json")
		return false
	}

	// Parse URLs
	parts := strings.Split(envURLs, ",")
	for _, u := range parts {
		trimmed := strings.TrimSpace(u)
		if trimmed != "" && isValidURL(trimmed) {
			workerURLs = append(workerURLs, trimmed)
		}
	}

	if len(workerURLs) == 0 {
		fmt.Println("❌ Tidak ada worker URL yang valid!")
		return false
	}

	fmt.Printf("🔒 Security: %d Worker URLs berhasil dimuat.\n", len(workerURLs))
	return true
}

func isValidURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// readKeysFromJSON: baca WORKER_URLS dari keys.json
func readKeysFromJSON() string {
	data, err := os.ReadFile("keys.json")
	if err != nil {
		return ""
	}
	var cfg map[string]string
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	return strings.TrimSpace(cfg["WORKER_URLS"])
}

// === FUNGSI BANTU UTAMA ===
func checkProxyManualSocket(input ProxyInput, realIP string) CheckResult {

	for i, target := range workerURLs {
		body, code := rawSocketRequest(target, input.IP, input.Port)
		if code == 200 {
			var resp WorkerResponse
			if err := json.Unmarshal(body, &resp); err == nil {
				// Validasi IP dengan lebih ketat
				if isValidIP(resp.IP) && (realIP == "" || resp.IP != realIP) {
					finalOrg := cleanOrgName(input.OrgInput)
					if resp.Org != "" {
						finalOrg = cleanOrgName(resp.Org)
					}

					finalCountry := normalizeCountry(input.Country)
					if resp.Country != "" {
						finalCountry = normalizeCountry(resp.Country)
					}

					return CheckResult{
						Valid: true,
						Data: &ValidProxy{
							IP:      input.IP,
							Port:    input.Port,
							Country: finalCountry,
							Org:     finalOrg,
							City:    resp.City,
							Source:  fmt.Sprintf("Worker-%d", i+1),
						},
					}
				}
			}
		}
	}

	// LAYER 2: Cloudflare Trace
	body, code := rawSocketRequest(TraceURL, input.IP, input.Port)
	if code == 200 {
		ip, loc := parseTraceDetails(string(body))

		if isValidIP(ip) && (realIP == "" || ip != realIP) {
			finalCountry := normalizeCountry(input.Country)
			if loc != "" {
				finalCountry = normalizeCountry(loc)
			}

			return CheckResult{
				Valid: true,
				Data: &ValidProxy{
					IP:      input.IP,
					Port:    input.Port,
					Country: finalCountry,
					Org:     cleanOrgName(input.OrgInput),
					Source:  "CF Trace",
				},
			}
		}
	}

	// LAYER 3: AWS CheckIP
	body, code = rawSocketRequest(AwsURL, input.IP, input.Port)
	if code == 200 {
		ip := strings.TrimSpace(string(body))
		if isValidIP(ip) && (realIP == "" || ip != realIP) {
			return CheckResult{
				Valid: true,
				Data: &ValidProxy{
					IP:      input.IP,
					Port:    input.Port,
					Country: normalizeCountry(input.Country),
					Org:     cleanOrgName(input.OrgInput),
					Source:  "AWS",
				},
			}
		}
	}

	return CheckResult{Valid: false}
}

func normalizeCountry(country string) string {
	country = strings.ToUpper(strings.TrimSpace(country))
	// Jika lebih dari 2 karakter, ambil 2 karakter pertama
	if len(country) > 2 {
		return country[:2]
	}
	return country
}

func rawSocketRequest(targetURL, proxyIP, proxyPort string) ([]byte, int) {
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return nil, 0
	}

	host := parsedURL.Hostname()
	if host == "" {
		return nil, 0
	}

	path := parsedURL.Path
	if path == "" {
		path = "/"
	}

	if proxyPort == "" {
		return nil, 0
	}

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(proxyIP, proxyPort),
		time.Duration(TimeoutSec)*time.Second)
	if err != nil {
		atomic.AddInt32(&statDialErr, 1)
		return nil, 0
	}
	defer conn.Close()

	tlsConfig := &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	}

	tlsConn := tls.Client(conn, tlsConfig)
	if tlsConn == nil {
		atomic.AddInt32(&statTLSErr, 1)
		return nil, 0
	}

	deadline := time.Now().Add(time.Duration(TimeoutSec) * time.Second)
	tlsConn.SetDeadline(deadline)

	if err := tlsConn.Handshake(); err != nil {
		atomic.AddInt32(&statTLSErr, 1)
		return nil, 0
	}
	defer tlsConn.Close()

	rawRequest := fmt.Sprintf(
		"GET %s HTTP/1.1\r\n"+
			"Host: %s\r\n"+
			"User-Agent: %s\r\n"+
			"Accept: */*\r\n"+
			"Connection: close\r\n"+
			"\r\n",
		path, host, useragent.GetRandom(),
	)

	if _, err := tlsConn.Write([]byte(rawRequest)); err != nil {
		atomic.AddInt32(&statHTTPErr, 1)
		return nil, 0
	}

	reader := bufio.NewReader(tlsConn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		atomic.AddInt32(&statHTTPErr, 1)
		return nil, 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		atomic.AddInt32(&statNon200Err, 1)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		atomic.AddInt32(&statHTTPErr, 1)
		return nil, resp.StatusCode
	}

	return body, resp.StatusCode
}

// === FUNGSI UTILITAS ===
func getPublicIPDirect() (string, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		},
	}

	for _, u := range workerURLs {
		resp, err := client.Get(u)
		if err == nil && resp.StatusCode == 200 {
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err == nil {
				var w WorkerResponse
				if json.Unmarshal(body, &w) == nil && isValidIP(w.IP) {
					return w.IP, nil
				}
			}
		}
	}

	// Fallback ke AWS
	resp, err := client.Get(AwsURL)
	if err == nil && resp.StatusCode == 200 {
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err == nil {
			ip := strings.TrimSpace(string(body))
			if isValidIP(ip) {
				return ip, nil
			}
		}
	}

	return "", fmt.Errorf("tidak bisa mendapatkan IP publik")
}

func parseTraceDetails(text string) (string, string) {
	var ip, loc string
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "ip=") {
			ip = strings.TrimPrefix(line, "ip=")
		} else if strings.HasPrefix(line, "loc=") {
			loc = strings.TrimPrefix(line, "loc=")
		}
	}
	return ip, loc
}

func cleanOrgName(org string) string {
	if org == "" {
		return ""
	}
	cleaned := regexOrg.ReplaceAllString(org, "")
	return strings.TrimSpace(cleaned)
}

func readInputFile(path string) ([]ProxyInput, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("gagal membuka file: %v", err)
	}
	defer file.Close()

	var proxies []ProxyInput
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		parts := strings.Split(line, ",")
		if len(parts) >= 4 {
			ip := strings.TrimSpace(parts[0])
			port := strings.TrimSpace(parts[1])
			country := strings.TrimSpace(parts[2])
			org := strings.TrimSpace(parts[3])

			if ip != "" && port != "" && isValidIP(ip) {
				proxies = append(proxies, ProxyInput{
					IP:       ip,
					Port:     port,
					Country:  country,
					OrgInput: org,
				})
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return proxies, fmt.Errorf("error membaca file line %d: %v", lineNum, err)
	}

	if len(proxies) == 0 {
		return proxies, fmt.Errorf("tidak ada proxy valid dalam file")
	}

	return proxies, nil
}

func isValidIP(ip string) bool {
	if ip == "" {
		return false
	}
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}
	if parsedIP.IsPrivate() || parsedIP.IsLoopback() || parsedIP.IsUnspecified() {
		return false
	}
	return true
}

func progressMonitor(ticker *time.Ticker, done chan bool, stats *Stats) {
	for {
		select {
		case <-done:
			fmt.Printf("\r⏳ Progress: %d/%d | ✅ Live: %d    \n",
				atomic.LoadInt32(&stats.Checked),
				stats.Total,
				atomic.LoadInt32(&stats.Live))
			return
		case <-ticker.C:
			current := atomic.LoadInt32(&stats.Checked)
			live := atomic.LoadInt32(&stats.Live)
			fmt.Printf("\r⏳ Progress: %d/%d | ✅ Live: %d    ",
				current, stats.Total, live)
		}
	}
}

func saveResults(proxies []ValidProxy, limit int) {
	if len(proxies) == 0 {
		fmt.Println("❌ Tidak ada proxy yang valid untuk disimpan.")
		return
	}
	if limit > 0 {
		fmt.Printf("🧪 Mode tes (limit=%d): file tidak ditulis.\n", limit)
		for _, p := range proxies[:min(5, len(proxies))] {
			fmt.Printf("   %s,%s,%s,%s\n", p.IP, p.Port, p.Country, cleanOrgName(p.Org))
		}
		return
	}

	// 1. SAVE ALIVE
	sort.Slice(proxies, func(i, j int) bool {
		if proxies[i].Country == proxies[j].Country {
			return proxies[i].IP < proxies[j].IP
		}
		return proxies[i].Country < proxies[j].Country
	})

	if err := writeToFile(FileAlive, proxies); err != nil {
		fmt.Printf("❌ Gagal menyimpan %s: %v\n", FileAlive, err)
		return
	}

	fmt.Printf("\n\n📁 Output Report:\n")
	fmt.Printf("   ✓ Alive.txt    : %d proxies (Urut A-Z)\n", len(proxies))
}

func writeToFile(filename string, proxies []ValidProxy) error {
	tmpFile := filename + ".tmp"
	
	file, err := os.Create(tmpFile)
	if err != nil {
		return err
	}
	
	writer := bufio.NewWriter(file)
	for _, p := range proxies {
		line := fmt.Sprintf("%s,%s,%s,%s\n", p.IP, p.Port, p.Country, p.Org)
		if _, err := writer.WriteString(line); err != nil {
			file.Close()
			os.Remove(tmpFile)
			return err
		}
	}
	
	if err := writer.Flush(); err != nil {
		file.Close()
		os.Remove(tmpFile)
		return err
	}
	file.Close()
	
	// Atomic rename (POSIX guarantee)
	return os.Rename(tmpFile, filename)
}

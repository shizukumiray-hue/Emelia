package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// MergeFile: file input proxy (tidak pernah ditimpa), format IP,PORT,CC,ORG
const MergeFile = "Data/ALL-MERGED.txt"

// parseFullLine: baris format standar "IP,PORT,CC,ORG"
func parseFullLine(line string) (ProxyInput, bool) {
	parts := strings.Split(line, ",")
	if len(parts) < 3 {
		return ProxyInput{}, false
	}
	ip := strings.TrimSpace(parts[0])
	port := strings.TrimSpace(parts[1])
	cc := strings.TrimSpace(parts[2])
	org := ""
	if len(parts) >= 4 {
		org = strings.TrimSpace(parts[3])
	}
	if !isValidIP(ip) || port == "" {
		return ProxyInput{}, false
	}
	return ProxyInput{IP: ip, Port: port, Country: normalizeCountry(cc), OrgInput: org}, true
}

// runMerge: baca Data/ALL-MERGED.txt (input), check semua via API,
// tulis Data/alive.txt (output) berisi hanya proxy LIVE + org, sortir by country.
// limit > 0: hanya check N proxy pertama (mode tes, tidak menulis file).
func runMerge(limit int) {
	fmt.Println("🔀 Mode MERGE: read Data/ALL-MERGED.txt → check → write Data/alive.txt (live + org, sort by country)")

	f, err := os.Open(MergeFile)
	if err != nil {
		fmt.Printf("❌ Gagal buka %s: %v\n", MergeFile, err)
		return
	}
	defer f.Close()

	merged := make(map[string]ProxyInput) // key: ip:port, prefer yang punya org
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		p, ok := parseFullLine(sc.Text())
		if !ok {
			continue
		}
		key := p.IP + ":" + p.Port
		existing, seen := merged[key]
		if !seen {
			merged[key] = p
		} else if existing.OrgInput == "" && p.OrgInput != "" {
			merged[key] = p
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Printf("❌ Error baca %s: %v\n", MergeFile, err)
		return
	}
	fmt.Printf("📥 Total unik di %s: %d\n", MergeFile, len(merged))
	if len(merged) == 0 {
		fmt.Println("❌ Tidak ada proxy valid.")
		return
	}

	// 1. DEDUPE + pilah yang perlu check (semua harus check — hasilnya live only)
	list := make([]ProxyInput, 0, len(merged))
	for _, p := range merged {
		list = append(list, p)
	}
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}

	// 2. API CHECK semua
	fmt.Printf("🚀 API check %d proxy (max %d concurrent)...\n", len(list), MaxConcurrent)
	stats := &Stats{Total: int32(len(list))}
	resultsChan := make(chan CheckResult, len(list))
	var wg sync.WaitGroup
	sem := make(chan struct{}, MaxConcurrent)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	done := make(chan bool)
	go progressMonitor(ticker, done, stats)

	for _, p := range list {
		wg.Add(1)
		sem <- struct{}{}
		go func(proxy ProxyInput) {
			defer wg.Done()
			defer func() { <-sem }()
			res := checkProxyManualSocket(proxy, "")
			atomic.AddInt32(&stats.Checked, 1)
			resultsChan <- res
		}(p)
	}
	wg.Wait()
	close(done)
	close(resultsChan)

	// 3. Hanya simpan yang LIVE, org dari API (fallback org input)
	var alive []ValidProxy
	for res := range resultsChan {
		if res.Valid && res.Data != nil {
			alive = append(alive, *res.Data)
		}
	}
	fmt.Printf("\n✅ Selesai: %d live dari %d check\n", len(alive), len(list))

	if len(alive) == 0 {
		fmt.Println("❌ Tidak ada proxy live, file tidak ditulis.")
		return
	}

	// 4. TULIS BALIK (hanya live, org terisi, format IP,PORT,CC,ORG)
	//    limit > 0 = mode tes: cukup lapor, JANGAN timpa file asli
	if limit > 0 {
		fmt.Printf("🧪 Mode tes (limit=%d): file tidak ditulis.\n", limit)
		for _, p := range alive[:min(5, len(alive))] {
			fmt.Printf("   %s,%s,%s,%s\n", p.IP, p.Port, p.Country, cleanOrgName(p.Org))
		}
		return
	}

	sort.Slice(alive, func(i, j int) bool {
		if alive[i].Country == alive[j].Country {
			return alive[i].IP < alive[j].IP
		}
		return alive[i].Country < alive[j].Country
	})

	out, err := os.Create(FileAlive)
	if err != nil {
		fmt.Printf("❌ Gagal tulis %s: %v\n", FileAlive, err)
		return
	}
	defer out.Close()
	w := bufio.NewWriter(out)
	emptyOrg := 0
	for _, p := range alive {
		org := cleanOrgName(p.Org)
		if org == "" {
			emptyOrg++
		}
		fmt.Fprintf(w, "%s,%s,%s,%s\n", p.IP, p.Port, p.Country, org)
	}
	if err := w.Flush(); err != nil {
		fmt.Printf("❌ Gagal flush: %v\n", err)
		return
	}
	fmt.Printf("\n📁 Output: %s (%d proxy live, sort by country)\n", FileAlive, len(alive))
	if emptyOrg > 0 {
		fmt.Printf("⚠️  %d entri tanpa org (API tidak mengembalikan isp)\n", emptyOrg)
	} else {
		fmt.Println("✅ Semua entri punya org/isp.")
	}
}
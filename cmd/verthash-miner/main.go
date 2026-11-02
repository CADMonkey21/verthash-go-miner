package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CADMonkey21/verthash-go-miner/pkg/cuda"
	"github.com/CADMonkey21/verthash-go-miner/pkg/stratum"
	"github.com/CADMonkey21/verthash-go-miner/pkg/verthash"
)

func resolveVerthashDat(customPath string) string {
	if customPath != "" {
		return customPath
	}
	if _, err := os.Stat("verthash.dat"); err == nil {
		return "verthash.dat"
	}
	if appData := os.Getenv("APPDATA"); appData != "" {
		ocmPath := filepath.Join(appData, "vertcoin-ocm", "verthash.dat")
		if _, err := os.Stat(ocmPath); err == nil {
			return ocmPath
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		dlPath := filepath.Join(home, "Downloads", "verthash.dat")
		if _, err := os.Stat(dlPath); err == nil {
			return dlPath
		}
	}
	return "verthash.dat"
}

func filterDevices(all []cuda.DeviceInfo, filterStr string) []cuda.DeviceInfo {
	filterStr = strings.TrimSpace(filterStr)
	if filterStr == "" {
		return all
	}
	allowed := make(map[int32]bool)
	for _, part := range strings.Split(filterStr, ",") {
		if idx, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
			allowed[int32(idx)] = true
		}
	}
	var out []cuda.DeviceInfo
	for _, d := range all {
		if allowed[d.Index] {
			out = append(out, d)
		}
	}
	return out
}

type gpuWorkerHandle struct {
	info       cuda.DeviceInfo
	nvmlHandle uintptr
	engine     *cuda.Engine
	jobCh      chan *stratum.Job
	hashCount  atomic.Uint64
	throttled  atomic.Bool
}

func main() {
	poolURL := flag.String("o", "", "Stratum server URL (e.g. stratum+tcp://127.0.0.1:9172)")
	user := flag.String("u", "", "Vertcoin payout address (supports +diff hint)")
	pass := flag.String("p", "x", "Stratum password")
	datFlag := flag.String("d", "", "Path to verthash.dat (auto-detected if omitted)")
	genDat := flag.Bool("gen", false, "Generate verthash.dat if missing")
	benchMode := flag.Bool("bench", false, "Run 5-second consensus self-test & multi-GPU benchmark")
	tempLimit := flag.Uint("temp-limit", 82, "GPU temperature limit in C (0 to disable)")
	devicesFlag := flag.String("devices", "", "Comma-separated GPU indices to use (e.g. 0,1)")
	flag.Parse()

	fmt.Println("=== verthash-go-miner (Zero-CGO Edition) ===")

	driverVer, allDevices, err := cuda.InitDriver()
	if err != nil {
		log.Fatalf("CUDA Init Error: %v", err)
	}
	nvmlHandles := cuda.InitNVML(allDevices)
	devices := filterDevices(allDevices, *devicesFlag)
	if len(devices) == 0 {
		log.Fatalf("No matching CUDA devices found for -devices=%s", *devicesFlag)
	}
	fmt.Printf("CUDA Driver Version: %d.%d | Active GPU(s): %d | Temp Limit: %dC\n",
		driverVer/1000, (driverVer%100)/10, len(devices), *tempLimit)

	datPath := resolveVerthashDat(*datFlag)
	if *genDat {
		fmt.Printf("Ensuring %s exists and is verified...\n", datPath)
		if err := verthash.EnsureVerthashDatafile(datPath); err != nil {
			log.Fatalf("Failed to generate verthash.dat: %v", err)
		}
	}

	fmt.Printf("Loading %s via zero-copy mmap...\n", datPath)
	vh, err := verthash.NewVerthash(datPath, true)
	if err != nil {
		log.Fatalf("Failed to load verthash.dat (%s): %v", datPath, err)
	}
	defer vh.Close()
	fmt.Printf("Loaded verthash.dat (%d MB)\n\n", vh.DataSize()/(1024*1024))

	var testHeader [80]byte
	for i := range testHeader {
		testHeader[i] = byte(i * 3)
	}
	cuda.PutNonce(&testHeader, 0x12345678)
	cpuHash, err := vh.SumVerthash(testHeader[:])
	if err != nil {
		log.Fatalf("CPU SumVerthash failed: %v", err)
	}

	if *benchMode || *poolURL == "" {
		fmt.Printf("CPU Reference Hash: %s\n", hex.EncodeToString(cpuHash[:]))
		var wg sync.WaitGroup
		for _, dev := range devices {
			wg.Add(1)
			go func(d cuda.DeviceInfo) {
				defer wg.Done()
				eng, err := cuda.NewEngine(d.Index, vh.Data())
				if err != nil {
					log.Printf("[GPU %d] Init failed: %v", d.Index, err)
					return
				}
				gpuHash, err := eng.HashOne(testHeader)
				if err != nil || gpuHash != cpuHash {
					log.Printf("[GPU %d] Self-test FAILED!", d.Index)
					return
				}
				fmt.Printf("[GPU %d] %s -> Consensus Self-Test PASSED (100%% bit-exact match)\n", d.Index, d.Name)

				_ = eng.SetHeader(testHeader)
				gridBlocks := uint32(512)
				if d.TotalMB > 6000 {
					gridBlocks = 1024
				}
				const threads = uint32(256)
				batchSize := gridBlocks * threads

				start := time.Now()
				var totalHashes uint64
				var nonce uint32
				for time.Since(start) < 5*time.Second {
					_, err := eng.SearchBatch(nonce, gridBlocks, threads, 0x000000FFFFFFFFFF)
					if err != nil {
						log.Printf("[GPU %d] Search error: %v", d.Index, err)
						return
					}
					totalHashes += uint64(batchSize)
					nonce += batchSize
				}
				elapsed := time.Since(start).Seconds()
				khps := (float64(totalHashes) / elapsed) / 1000.0
				tel := cuda.QueryTelemetry(nvmlHandles[d.Index])
				fmt.Printf("[GPU %d] %s -> %.2f kH/s%s (%d hashes in %.2fs)\n",
					d.Index, d.Name, khps, tel.FormatCompact(), totalHashes, elapsed)
			}(dev)
		}
		wg.Wait()
		if *poolURL == "" {
			fmt.Println("\nTip: To mine live to p2pool-go-VTC, run:")
			fmt.Println("  ./verthash-miner -o stratum+tcp://127.0.0.1:9172 -u <YOUR_VTC_ADDRESS>+0.5")
		}
		return
	}

	if *user == "" {
		log.Fatalf("Please specify your Vertcoin payout address with -u <VTC_ADDRESS>")
	}

	workers := make([]*gpuWorkerHandle, len(devices))
	var client *stratum.Client

	for i, dev := range devices {
		w := &gpuWorkerHandle{
			info:       dev,
			nvmlHandle: nvmlHandles[dev.Index],
			jobCh:      make(chan *stratum.Job, 1),
		}
		workers[i] = w
	}

	client = stratum.NewClient(*poolURL, *user, *pass, func(job *stratum.Job) {
		for _, w := range workers {
			if w.engine != nil {
				w.engine.Abort()
			}
			select {
			case <-w.jobCh:
			default:
			}
			w.jobCh <- job
		}
	})

	for i, w := range workers {
		go func(idx int, wh *gpuWorkerHandle) {
			eng, err := cuda.NewEngine(wh.info.Index, vh.Data())
			if err != nil {
				log.Fatalf("[GPU %d] Init failed: %v", wh.info.Index, err)
			}
			gpuHash, err := eng.HashOne(testHeader)
			if err != nil || gpuHash != cpuHash {
				log.Fatalf("[GPU %d] Consensus self-test failed!", wh.info.Index)
			}
			wh.engine = eng
			tel := cuda.QueryTelemetry(wh.nvmlHandle)
			fmt.Printf("[GPU %d] %s (%d MB)%s -> Ready & Verified\n",
				wh.info.Index, wh.info.Name, wh.info.TotalMB, tel.FormatCompact())

			gridBlocks := uint32(256)
			if wh.info.TotalMB > 6000 {
				gridBlocks = 512
			}
			const threads = uint32(256)
			batchSize := gridBlocks * threads

			var curJob *stratum.Job
			var en2Hex string
			var hdr80 [80]byte
			var nonce uint32
			var batchCounter uint32

			for {
				// Periodic thermal protection check every 16 batches (~1 second)
				if *tempLimit > 0 && batchCounter&0xF == 0 {
					t := cuda.QueryTelemetry(wh.nvmlHandle)
					if t.HasTemp && t.TempC >= uint32(*tempLimit) {
						wh.throttled.Store(true)
						resumeTemp := uint32(*tempLimit) - 4
						fmt.Printf("[GPU %d] Thermal limit reached (%dC >= %dC) — pausing until %dC...\n",
							wh.info.Index, t.TempC, *tempLimit, resumeTemp)
						for {
							time.Sleep(2 * time.Second)
							t = cuda.QueryTelemetry(wh.nvmlHandle)
							if !t.HasTemp || t.TempC <= resumeTemp {
								fmt.Printf("[GPU %d] Cooled to %dC — resuming mining\n", wh.info.Index, t.TempC)
								wh.throttled.Store(false)
								break
							}
						}
					}
				}
				batchCounter++

				select {
				case newJob := <-wh.jobCh:
					curJob = newJob
					en2Bytes, hexStr := client.NextExtraNonce2(curJob.ExtraNonce2Len)
					en2Hex = hexStr
					hdr80 = curJob.BuildHeader(en2Bytes)
					_ = eng.SetHeader(hdr80)
					nonce = uint32(idx) * 0x40000000
				default:
					if curJob == nil {
						newJob := <-wh.jobCh
						curJob = newJob
						en2Bytes, hexStr := client.NextExtraNonce2(curJob.ExtraNonce2Len)
						en2Hex = hexStr
						hdr80 = curJob.BuildHeader(en2Bytes)
						_ = eng.SetHeader(hdr80)
						nonce = uint32(idx) * 0x40000000
					}
				}

				candidates, err := eng.SearchBatch(nonce, gridBlocks, threads, curJob.TargetU64)
				if err != nil {
					log.Printf("[GPU %d] Kernel error: %v", wh.info.Index, err)
					time.Sleep(time.Second)
					continue
				}
				wh.hashCount.Add(uint64(batchSize))

				for _, candNonce := range candidates {
					candHdr := hdr80
					cuda.PutNonce(&candHdr, candNonce)
					fullHashLE, err := vh.SumVerthash(candHdr[:])
					if err != nil {
						continue
					}
					powInt, shareDiff := stratum.TargetToDiff(fullHashLE)
					if powInt.Cmp(curJob.Target256) <= 0 {
						fmt.Printf("[GPU %d] Found Share! Nonce: %08x | Share Diff: %.5f (Target: %.5f)\n",
							wh.info.Index, candNonce, shareDiff, curJob.Difficulty)
						_ = client.SubmitShare(curJob.JobID, en2Hex, curJob.NTimeHex, candNonce)
					}
				}

				nonce += batchSize
				if nonce+batchSize < nonce {
					en2Bytes, hexStr := client.NextExtraNonce2(curJob.ExtraNonce2Len)
					en2Hex = hexStr
					hdr80 = curJob.BuildHeader(en2Bytes)
					_ = eng.SetHeader(hdr80)
				}
			}
		}(i, w)
	}

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		lastCounts := make([]uint64, len(workers))
		lastTime := time.Now()

		for range ticker.C {
			now := time.Now()
			dt := now.Sub(lastTime).Seconds()
			lastTime = now

			var totalKHs, totalWatts float64
			var parts []string
			for i, w := range workers {
				cur := w.hashCount.Load()
				delta := cur - lastCounts[i]
				lastCounts[i] = cur
				khs := (float64(delta) / dt) / 1000.0
				totalKHs += khs

				tel := cuda.QueryTelemetry(w.nvmlHandle)
				if tel.HasPower {
					totalWatts += tel.PowerW
				}
				status := ""
				if w.throttled.Load() {
					status = " [THROTTLED]"
				}
				parts = append(parts, fmt.Sprintf("GPU%d: %.1f kH/s%s%s", w.info.Index, khs, tel.FormatCompact(), status))
			}
			acc := client.AcceptedShares.Load()
			rej := client.RejectedShares.Load()

			powerSummary := ""
			if totalWatts > 0 {
				powerSummary = fmt.Sprintf(" | Power: %.0fW (%.2f kH/W)", totalWatts, totalKHs/totalWatts)
			}
			fmt.Printf("[Stats] Total: %.2f kH/s [%s]%s | Shares: %d Acc / %d Rej\n",
				totalKHs, strings.Join(parts, " | "), powerSummary, acc, rej)
		}
	}()

	for {
		fmt.Printf("[Stratum] Connecting to %s...\n", *poolURL)
		if err := client.ConnectAndRun(); err != nil {
			fmt.Printf("[Stratum] Disconnected (%v) — reconnecting in 3s...\n", err)
			time.Sleep(3 * time.Second)
		}
	}
}

# verthash-go-miner

A fast, 100% pure-Go (`CGO_ENABLED=0`) NVIDIA CUDA GPU miner for Vertcoin (Verthash), built for ultra-low-latency mining on [p2pool-go-VTC](https://github.com/CADMonkey21/p2pool-go-VTC) and standard Verthash Stratum pools.

## Key Features
- **Ready-to-Run Binaries (Zero CGO):** Dynamically binds directly to your existing NVIDIA driver (`nvcuda.dll` / `libcuda.so.1`) and NVML (`nvml.dll` / `libnvidia-ml.so.1`)—no CUDA Toolkit, C++ redistributables, or compilers required.
- **Modern GPU Support:** Runs natively on NVIDIA GTX 16-series, RTX 20-series, RTX 30-series, and RTX 40-series GPUs (4 GB+ VRAM) without falling back to OpenCL.
- **SHA3-512 Midstate Precomputation:** Precomputes the first 72-byte sponge block across all 8 `p0` iterations per job while keeping state arrays locked in GPU registers.
- **Sub-5ms P2Pool Work Preemption:** Immediately aborts stale GPU batches when a new sharechain tip arrives, even when `clean_jobs=false`, with lock-free vardiff updates and asynchronous CPU share verification.
- **Live NVML Telemetry & Thermal Protection:** Displays per-GPU temperature, fan speed, wattage, and `kH/W` efficiency with automatic thermal throttling (`-temp-limit 82`).

---

## Step-by-Step Installation & Setup Guide

### Prerequisites
- An **NVIDIA GPU** with at least **4 GB VRAM** (GTX 1650 or newer).
- Up-to-date **NVIDIA Game Ready / Studio Drivers** (Windows) or **NVIDIA Proprietary Drivers** (Linux).
- A **Vertcoin (VTC) wallet address** (from Vertcoin Core, OCM, or a supported wallet).

---

### Step 1: Download the Miner
You do **not** need to install Go or compile any code. Download the pre-compiled binary for your operating system from the [**Releases Page**](https://github.com/CADMonkey21/verthash-go-miner/releases/latest):

- **Windows (64-bit):** Download `verthash-miner-windows-amd64.exe` and place it in a new folder (for example, `C:\verthash-miner\`).
- **Linux (64-bit):** Download `verthash-miner-linux-amd64` and make it executable:
  ```bash
  mkdir -p ~/verthash-miner && cd ~/verthash-miner
  wget [https://github.com/CADMonkey21/verthash-go-miner/releases/latest/download/verthash-miner-linux-amd64](https://github.com/CADMonkey21/verthash-go-miner/releases/latest/download/verthash-miner-linux-amd64)
  chmod +x verthash-miner-linux-amd64
  ```

---

### Step 2: Set Up `verthash.dat` (1.2 GB Data File)
Verthash mining requires a standard 1.2 GB `verthash.dat` file:
1. **Automatic Detection:** If you already have `verthash.dat` in the same folder as the miner, in your `Downloads` folder, or installed via **Vertcoin One-Click Miner (OCM)** (`%APPDATA%\vertcoin-ocm\verthash.dat`), the miner will find and load it automatically.
2. **First-Time Generation (`-gen`):** If you do not have `verthash.dat` on your computer yet, simply add the `-gen` flag to your startup command. The miner will automatically generate and verify `verthash.dat` in your current folder before starting (takes ~1–2 minutes on the very first run).
3. **Custom Location (`-d`):** If you store `verthash.dat` in another folder, point to it using `-d /path/to/verthash.dat`.

---

### Step 3: Create a Startup Script & Start Mining

#### Option A: Windows (`mine.bat`)
1. Open **Notepad** and paste the following lines (replace `<YOUR_VTC_ADDRESS>` with your Vertcoin wallet address, and replace `127.0.0.1:9172` with your P2Pool node or Stratum pool address):
   ```bat
   @echo off
   verthash-miner-windows-amd64.exe -gen -o stratum+tcp://127.0.0.1:9172 -u <YOUR_VTC_ADDRESS>+0.5 -temp-limit 82
   pause
   ```
2. Click **File -> Save As**, change **Save as type** to **All Files (`*.*`)**, and save it as **`mine.bat`** in the same folder as `verthash-miner-windows-amd64.exe`.
3. Double-click **`mine.bat`** to start mining!

#### Option B: Linux (`mine.sh`)
1. Create a `mine.sh` script in the same directory as the binary (replace `<YOUR_VTC_ADDRESS>` and the pool IP/port with your own):
   ```bash
   cat << 'SCRIPT' > mine.sh
   #!/usr/bin/env bash
   ./verthash-miner-linux-amd64 -gen -o stratum+tcp://127.0.0.1:9172 -u <YOUR_VTC_ADDRESS>+0.5 -temp-limit 82
   SCRIPT
   chmod +x mine.sh
   ```
2. Start the miner:
   ```bash
   ./mine.sh
   ```

*(Tip: Appending `+0.5` to your wallet address sets a friendly starting share difficulty on `p2pool-go-VTC` before automatic vardiff takes over. You can omit `+0.5` on standard pools.)*

---

## Command-Line Flags Reference

| Flag | Default | Description |
| :--- | :--- | :--- |
| `-o` | *(none)* | Stratum pool URL (e.g., `stratum+tcp://127.0.0.1:9172`) |
| `-u` | *(none)* | Your Vertcoin payout address (supports optional `+diff` hint for P2Pool) |
| `-p` | `x` | Stratum pool password |
| `-gen` | `false` | Automatically generate and verify `verthash.dat` if missing |
| `-d` | *(auto)* | Custom path to `verthash.dat` (auto-detects current dir, OCM dir, or `Downloads`) |
| `-devices` | *(all)* | Comma-separated GPU indices to mine on (e.g., `-devices 0` or `-devices 0,1`) |
| `-temp-limit` | `82` | GPU temperature limit in °C; pauses the GPU until it cools by 4°C (`0` to disable) |
| `-bench` | `false` | Run a 5-second consensus self-test and GPU speed benchmark without a pool |

### Testing Your GPUs Without a Pool (Benchmark Mode)
To verify your GPU drivers and test your hashrate before connecting to a pool, run:
- **Windows:** `.\verthash-miner-windows-amd64.exe -gen -bench`
- **Linux:** `./verthash-miner-linux-amd64 -gen -bench`

---

## Optional: Power & Efficiency Tuning (`nvidia-smi -lgc`)

Because Verthash is memory-bandwidth bound and `verthash-go-miner` offloads the initial SHA3-512 midstate to the CPU, locking your GPU's maximum core boost clock forces the card to the bottom of its voltage curve—**cutting power draw by 34%–42% and dropping temperatures by 10°C–19°C** with virtually zero hashrate loss:

**Windows (Run in Administrator PowerShell or Command Prompt):**
```powershell
nvidia-smi -i 0 -lgc 210,1320   # RTX 3070 (or 210,1200 for max efficiency)
nvidia-smi -i 1 -lgc 300,1350   # GTX 1650
```

**Linux:**
```bash
sudo nvidia-smi -pm 1
sudo nvidia-smi -i 0 -lgc 210,1350   # RTX 4060 Ti
sudo nvidia-smi -i 1 -lgc 210,1200   # RTX 3060 / RTX 3070
```

*(To reset to factory boost clocks at any time, run `nvidia-smi -rgc` as Administrator/root.)*

---

## Benchmarks (Live Sustained Mining)

### Power-Optimized (`nvidia-smi -lgc` Core Clock Cap)
| GPU | Core Lock | Sustained Hashrate | Power | Efficiency |
| :--- | :--- | :--- | :--- | :--- |
| NVIDIA GeForce RTX 3070 (8 GB) | 1200 MHz | ~983.1 kH/s | 94 W | 10.46 kH/W |
| NVIDIA GeForce RTX 3070 (8 GB) | 1320 MHz | ~1,009.3 kH/s | 99 W | 10.19 kH/W |
| NVIDIA GeForce RTX 4060 Ti (8 GB) | 1350 MHz | ~524.3 kH/s | 53 W | 9.89 kH/W |
| NVIDIA GeForce RTX 3060 (12 GB) | 1200 MHz | ~720.9 kH/s | 83 W | 8.69 kH/W |
| NVIDIA GeForce GTX 1650 (4 GB) | 1350 MHz | ~183.5 kH/s | 28 W | 6.55 kH/W |

### Stock Clocks (Unrestricted Boost)
| GPU | Memory Bus | Sustained Hashrate | Peak Hashrate | Stock Power |
| :--- | :--- | :--- | :--- | :--- |
| NVIDIA GeForce RTX 3070 (8 GB) | 256-bit GDDR6 | ~1,009 - 1,035 kH/s | 1,061.7 kH/s | ~161 W |
| NVIDIA GeForce RTX 3060 (12 GB) | 192-bit GDDR6 | ~708 - 721 kH/s | 734.0 kH/s | ~124 W |
| NVIDIA GeForce RTX 4060 Ti (8 GB) | 128-bit GDDR6 | ~524 - 537 kH/s | 550.4 kH/s | ~84 W |
| NVIDIA GeForce GTX 1650 (4 GB) | 128-bit GDDR5 | ~183 - 190 kH/s | 196.6 kH/s | ~44 W |

---

## Building from Source (For Developers)

Requires Go 1.22+. Because `verthash-go-miner` uses `purego` instead of `cgo`, no C/C++ compiler or CUDA SDK is needed:

```bash
git clone [https://github.com/CADMonkey21/verthash-go-miner.git](https://github.com/CADMonkey21/verthash-go-miner.git)
cd verthash-go-miner
CGO_ENABLED=0 go build -ldflags="-s -w" -o verthash-miner ./cmd/verthash-miner
```

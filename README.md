# verthash-go-miner

A 100% pure Go (`CGO_ENABLED=0`), zero-CGO NVIDIA CUDA GPU miner for Vertcoin (Verthash), built for ultra-low-latency integration with [p2pool-go-VTC](https://github.com/CADMonkey21/p2pool-go-VTC).

## Key Features
- **Zero CGO (`CGO_ENABLED=0`):** Dynamically binds the OS CUDA Driver API (`nvcuda.dll` / `libcuda.so.1`) and NVML (`nvml.dll` / `libnvidia-ml.so.1`) via `purego`—no C/C++ compiler, Visual Studio, or `nvcc` required to build or run.
- **SHA3-512 Midstate Precomputation:** Precomputes the first 72-byte sponge block across all 8 `p0` iterations per job, cutting Keccak-f[1600] permutations nearly in half while keeping `p0[128]` and `st[25]` locked in GPU registers.
- **Sub-5ms P2Pool Work Preemption:** Mapped host-to-device abort flags immediately abandon stale batches when `p2pool-go-VTC` broadcasts a new sharechain tip (even on `clean_jobs = false`).
- **Startup Consensus Self-Test:** Verifies 100% bit-exact GPU consensus against the CPU Verthash reference implementation before submitting shares.
- **Zero-CGO NVML Telemetry & Thermal Protection:** Reports live per-GPU core temperature, fan speed percentage, power draw (Watts), and rig `kH/W` efficiency, with automatic thermal throttling (`-temp-limit`).

## Benchmarks (Live Sustained Mining)

### Power-Optimized (`nvidia-smi -lgc` Core Clock Cap)
| GPU | Core Lock | Sustained Hashrate | Power | Efficiency |
| :--- | :--- | :--- | :--- | :--- |
| NVIDIA GeForce RTX 3070 (8 GB) | 1200 MHz | ~983.1 kH/s | 94 W | 10.46 kH/W |
| NVIDIA GeForce RTX 3060 (12 GB) | 1200 MHz | ~720.9 kH/s | 83 W | 8.69 kH/W |
| NVIDIA GeForce RTX 4060 Ti (8 GB) | 1350 MHz | ~524.3 kH/s | 53 W | 9.89 kH/W |
| NVIDIA GeForce GTX 1650 (4 GB) | 1350 MHz | ~183.5 kH/s | 28 W | 6.55 kH/W |

### Stock Clocks (Unrestricted Boost)
| GPU | Memory Bus | Sustained Hashrate | Peak Hashrate | Stock Power |
| :--- | :--- | :--- | :--- | :--- |
| NVIDIA GeForce RTX 3070 (8 GB) | 256-bit GDDR6 | ~1,009 - 1,035 kH/s | 1,061.7 kH/s | ~161 W |
| NVIDIA GeForce RTX 3060 (12 GB) | 192-bit GDDR6 | ~708 - 721 kH/s | 734.0 kH/s | ~124 W |
| NVIDIA GeForce RTX 4060 Ti (8 GB) | 128-bit GDDR6 | ~524 - 537 kH/s | 550.4 kH/s | ~84 W |
| NVIDIA GeForce GTX 1650 (4 GB) | 128-bit GDDR5 | ~183 - 190 kH/s | 196.6 kH/s | ~44 W |

## Efficiency Tuning (`nvidia-smi -lgc`)

Because Verthash is strictly memory-bandwidth bound and `verthash-go-miner` offloads the initial SHA3-512 midstate to the CPU, locking the GPU maximum boost clock forces the core to the bottom of its voltage curve—cutting power draw by 34%–42% and dropping temperatures by 10°C–19°C with virtually zero hashrate loss:

**Linux:**

    sudo nvidia-smi -pm 1
    sudo nvidia-smi -i 0 -lgc 210,1350   # RTX 4060 Ti
    sudo nvidia-smi -i 1 -lgc 210,1200   # RTX 3060 / RTX 3070

**Windows (Administrator PowerShell / CMD):**

    nvidia-smi -i 0 -lgc 210,1200        # RTX 3070 / RTX 3060
    nvidia-smi -i 1 -lgc 300,1350        # GTX 1650

*(Reset to factory clocks anytime with `nvidia-smi -rgc`.)*

## Build Instructions (Windows & Linux)

    CGO_ENABLED=0 go build -ldflags="-s -w" -o verthash-miner ./cmd/verthash-miner

## Usage

Run the 5-second consensus self-test and multi-GPU benchmark:

    ./verthash-miner -bench

Mine live to a `p2pool-go-VTC` node (with optional `+diff` starting difficulty hint):

    ./verthash-miner -o stratum+tcp://127.0.0.1:9172 -u <YOUR_VTC_ADDRESS>+0.5

### CLI Flags
- `-o`: Stratum server URL (e.g., `stratum+tcp://127.0.0.1:9172`)
- `-u`: Vertcoin payout address (append `+0.5` to set an initial P2Pool vardiff hint)
- `-p`: Stratum password (default `"x"`)
- `-d`: Custom path to `verthash.dat` (auto-detected in current directory, `%APPDATA%\vertcoin-ocm`, or `~/Downloads` if omitted)
- `-temp-limit`: GPU temperature cutoff in Celsius before pausing a worker (default `82`, set `0` to disable)
- `-devices`: Comma-separated GPU indices to mine on (e.g., `-devices 0,1`; defaults to all CUDA GPUs)
- `-gen`: Generate and verify `verthash.dat` automatically if missing
- `-bench`: Run startup consensus verification and 5-second benchmark

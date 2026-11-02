# verthash-go-miner

A 100% pure Go (`CGO_ENABLED=0`), zero-CGO NVIDIA CUDA GPU miner for Vertcoin (Verthash), built for ultra-low-latency integration with [p2pool-go-VTC](https://github.com/CADMonkey21/p2pool-go-VTC).

## Key Features
- **Zero CGO (`CGO_ENABLED=0`):** Dynamically binds the OS CUDA Driver API (`nvcuda.dll` / `libcuda.so.1`) and NVML (`nvml.dll` / `libnvidia-ml.so.1`) via `purego`—no C/C++ compiler, Visual Studio, or `nvcc` required to build or run.
- **SHA3-512 Midstate Precomputation:** Precomputes the first 72-byte sponge block across all 8 `p0` iterations per job, cutting Keccak-f[1600] permutations nearly in half while keeping `p0[128]` and `st[25]` locked in GPU registers.
- **Sub-5ms P2Pool Work Preemption:** Mapped host-to-device abort flags immediately abandon stale batches when `p2pool-go-VTC` broadcasts a new sharechain tip (even on `clean_jobs = false`).
- **Startup Consensus Self-Test:** Verifies 100% bit-exact GPU consensus against the CPU Verthash reference implementation before submitting shares.
- **Zero-CGO NVML Telemetry:** Reports live per-GPU core temperature, fan speed percentage, power draw (Watts), and rig `kH/W` efficiency.

## Benchmarks
| GPU | Hashrate |
| :--- | :--- |
| NVIDIA GeForce RTX 3070 | ~1,035 - 1,061 kH/s |
| NVIDIA GeForce GTX 1650 | ~183 - 190 kH/s |
| **Combined Rig Total** | **~1.22 - 1.25 MH/s** |

## Build Instructions (Windows & Linux)

    CGO_ENABLED=0 go build -o verthash-miner ./cmd/verthash-miner

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
- `-gen`: Generate and verify `verthash.dat` automatically if missing
- `-bench`: Run startup consensus verification and 5-second benchmark

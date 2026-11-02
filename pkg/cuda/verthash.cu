typedef unsigned int uint32_t;
typedef unsigned long long uint64_t;

#define ROTL64(x, y) (((x) << (y)) | ((x) >> (64 - (y))))
#define ROTL32(x, y) (((x) << (y)) | ((x) >> (32 - (y))))

__device__ __forceinline__ uint32_t fnv1a(uint32_t a, uint32_t b) {
    return (a ^ b) * 0x1000193U;
}

__constant__ uint64_t keccakf_rndc[24] = {
    0x0000000000000001ULL, 0x0000000000008082ULL, 0x800000000000808aULL, 0x8000000080008000ULL,
    0x000000000000808bULL, 0x0000000080000001ULL, 0x8000000080008081ULL, 0x8000000000008009ULL,
    0x000000000000008aULL, 0x0000000000000088ULL, 0x0000000080008009ULL, 0x000000008000000aULL,
    0x000000008000808bULL, 0x800000000000008bULL, 0x8000000000008089ULL, 0x8000000000008003ULL,
    0x8000000000008002ULL, 0x8000000000000080ULL, 0x000000000000800aULL, 0x800000008000000aULL,
    0x8000000080008081ULL, 0x8000000000008080ULL, 0x0000000080000001ULL, 0x8000000080008008ULL
};

__device__ __forceinline__ void keccakf1600(uint64_t st[25]) {
    const int piln[24] = {
        10, 7, 11, 17, 18, 3, 5, 16, 8, 21, 24, 4,
        15, 23, 19, 13, 12, 2, 20, 14, 22, 9, 6, 1
    };
    const int rotc[24] = {
        1, 3, 6, 10, 15, 21, 28, 36, 45, 55, 2, 14,
        27, 41, 56, 8, 25, 43, 62, 18, 39, 61, 20, 44
    };

    #pragma unroll 1
    for (int r = 0; r < 24; r++) {
        uint64_t bc[5];
        #pragma unroll
        for (int i = 0; i < 5; i++)
            bc[i] = st[i] ^ st[i + 5] ^ st[i + 10] ^ st[i + 15] ^ st[i + 20];

        #pragma unroll
        for (int i = 0; i < 5; i++) {
            uint64_t t = bc[(i + 4) % 5] ^ ROTL64(bc[(i + 1) % 5], 1);
            #pragma unroll
            for (int j = 0; j < 25; j += 5)
                st[j + i] ^= t;
        }

        uint64_t t = st[1];
        #pragma unroll
        for (int i = 0; i < 24; i++) {
            int j = piln[i];
            bc[0] = st[j];
            st[j] = ROTL64(t, rotc[i]);
            t = bc[0];
        }

        #pragma unroll
        for (int j = 0; j < 25; j += 5) {
            #pragma unroll
            for (int i = 0; i < 5; i++)
                bc[i] = st[j + i];
            #pragma unroll
            for (int i = 0; i < 5; i++)
                st[j + i] ^= (~bc[(i + 1) % 5]) & bc[(i + 2) % 5];
        }

        st[0] ^= keccakf_rndc[r];
    }
}

// Precomputes the 8 SHA3-512 midstates for bytes 0..71 of the header (runs once per job with 8 threads)
extern "C" __global__ void verthash_precompute_midstates(
    const uint32_t* __restrict__ d_header,
    uint64_t* __restrict__ d_midstates
) {
    uint32_t iter = threadIdx.x;
    if (iter >= 8) return;

    uint32_t hdr0 = (d_header[0] & 0xFFFFFF00U) | ((d_header[0] + iter + 1U) & 0xFFU);

    uint64_t st[25];
    st[0] = ((uint64_t)d_header[1] << 32) | hdr0;
    #pragma unroll
    for (int k = 1; k < 9; k++) {
        st[k] = ((uint64_t)d_header[2 * k + 1] << 32) | d_header[2 * k];
    }
    #pragma unroll
    for (int k = 9; k < 25; k++) st[k] = 0ULL;

    keccakf1600(st);

    #pragma unroll
    for (int k = 0; k < 25; k++) {
        d_midstates[iter * 25 + k] = st[k];
    }
}

__device__ void compute_verthash_midstate(
    const uint4* __restrict__ dat_u4,
    uint32_t mdiv,
    const uint32_t hdr[20],
    const uint64_t* __restrict__ d_midstates,
    uint32_t p1[8],
    volatile const uint32_t* abort_flag
) {
    uint64_t st[25];

    // 1. p1 = SHA3-256(hdr[0..19])
    #pragma unroll
    for (int k = 0; k < 10; k++) {
        st[k] = ((uint64_t)hdr[2 * k + 1] << 32) | hdr[2 * k];
    }
    st[10] = 0x0000000000000006ULL;
    #pragma unroll
    for (int k = 11; k < 16; k++) st[k] = 0ULL;
    st[16] = 0x8000000000000000ULL;
    #pragma unroll
    for (int k = 17; k < 25; k++) st[k] = 0ULL;

    keccakf1600(st);

    #pragma unroll
    for (int k = 0; k < 4; k++) {
        p1[2 * k]     = (uint32_t)st[k];
        p1[2 * k + 1] = (uint32_t)(st[k] >> 32);
    }

    // 2. p0 = 8 x SHA3-512 final block using precomputed midstate (fully unrolled so p0 stays in registers)
    uint32_t p0[128];
    uint64_t tail_word = ((uint64_t)hdr[19] << 32) | hdr[18];

    #pragma unroll
    for (int iter = 0; iter < 8; iter++) {
        const uint64_t* mid = &d_midstates[iter * 25];
        #pragma unroll
        for (int k = 0; k < 25; k++) {
            st[k] = __ldg(&mid[k]);
        }

        st[0] ^= tail_word;
        st[1] ^= 0x0000000000000006ULL;
        st[8] ^= 0x8000000000000000ULL;

        keccakf1600(st);

        #pragma unroll
        for (int k = 0; k < 8; k++) {
            p0[iter * 16 + 2 * k]     = (uint32_t)st[k];
            p0[iter * 16 + 2 * k + 1] = (uint32_t)(st[k] >> 32);
        }
    }

    // 3. 4,096 random 32-byte lookups across verthash.dat in VRAM
    // Outer loop x is unrolled 1x (compact PTX), inner loop y is fully unrolled so p0[0..127] stays in registers!
    uint32_t acc = 0x811c9dc5U;

    #pragma unroll 1
    for (int x = 0; x < 32; x++) {
        if (abort_flag != 0 && (x & 7) == 0 && *abort_flag != 0) {
            return;
        }
        #pragma unroll
        for (int y = 0; y < 128; y++) {
            uint32_t w = p0[y];
            p0[y] = ROTL32(w, 1);

            uint32_t idx16 = fnv1a(w, acc) % mdiv;
            uint4 v0 = __ldg(&dat_u4[idx16]);
            uint4 v1 = __ldg(&dat_u4[idx16 + 1]);

            p1[0] = fnv1a(p1[0], v0.x); acc = fnv1a(acc, v0.x);
            p1[1] = fnv1a(p1[1], v0.y); acc = fnv1a(acc, v0.y);
            p1[2] = fnv1a(p1[2], v0.z); acc = fnv1a(acc, v0.z);
            p1[3] = fnv1a(p1[3], v0.w); acc = fnv1a(acc, v0.w);
            p1[4] = fnv1a(p1[4], v1.x); acc = fnv1a(acc, v1.x);
            p1[5] = fnv1a(p1[5], v1.y); acc = fnv1a(acc, v1.y);
            p1[6] = fnv1a(p1[6], v1.z); acc = fnv1a(acc, v1.z);
            p1[7] = fnv1a(p1[7], v1.w); acc = fnv1a(acc, v1.w);
        }
    }
}

extern "C" __global__ void verthash_hash_one(
    const uint4* __restrict__ dat_u4,
    uint32_t mdiv,
    const uint32_t* __restrict__ d_header,
    const uint64_t* __restrict__ d_midstates,
    uint32_t* __restrict__ d_out_hash
) {
    if (threadIdx.x != 0 || blockIdx.x != 0) return;
    uint32_t hdr[20];
    for (int i = 0; i < 20; i++) hdr[i] = d_header[i];

    uint32_t p1[8];
    compute_verthash_midstate(dat_u4, mdiv, hdr, d_midstates, p1, 0);

    for (int i = 0; i < 8; i++) d_out_hash[i] = p1[i];
}

extern "C" __global__ void verthash_search(
    const uint4* __restrict__ dat_u4,
    uint32_t mdiv,
    const uint32_t* __restrict__ d_header,
    const uint64_t* __restrict__ d_midstates,
    uint32_t start_nonce,
    uint64_t target_u64,
    uint32_t* __restrict__ d_results,
    volatile const uint32_t* abort_flag
) {
    if (*abort_flag != 0) return;

    uint32_t tid = blockIdx.x * blockDim.x + threadIdx.x;
    uint32_t nonce = start_nonce + tid;

    uint32_t hdr[20];
    #pragma unroll
    for (int i = 0; i < 19; i++) hdr[i] = __ldg(&d_header[i]);
    hdr[19] = nonce;

    uint32_t p1[8];
    compute_verthash_midstate(dat_u4, mdiv, hdr, d_midstates, p1, abort_flag);

    if (*abort_flag != 0) return;

    uint64_t high64 = ((uint64_t)p1[7] << 32) | p1[6];
    if (high64 <= target_u64) {
        uint32_t slot = atomicAdd(&d_results[0], 1U);
        if (slot < 15U) {
            d_results[slot + 1U] = nonce;
        }
    }
}

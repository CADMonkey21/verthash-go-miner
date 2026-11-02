package stratum

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type TargetState struct {
	Difficulty float64
	Target256  *big.Int
	TargetU64  uint64
}

type Job struct {
	JobID          string
	PrevHashLE     [32]byte
	Coinb1         []byte
	Coinb2         []byte
	MerkleBranches [][]byte
	Version        uint32
	Bits           uint32
	NTimeHex       string
	NTime          uint32
	CleanJobs      bool
	SameTemplate   bool
	ExtraNonce1    []byte
	ExtraNonce2Len int
	Difficulty     float64
	Target256      *big.Int
	TargetU64      uint64
}

func SameWork(a, b *Job) bool {
	if a == nil || b == nil {
		return false
	}
	if a.PrevHashLE != b.PrevHashLE || a.Version != b.Version || a.Bits != b.Bits || a.NTime != b.NTime {
		return false
	}
	if !bytes.Equal(a.Coinb1, b.Coinb1) || !bytes.Equal(a.Coinb2, b.Coinb2) || !bytes.Equal(a.ExtraNonce1, b.ExtraNonce1) {
		return false
	}
	if len(a.MerkleBranches) != len(b.MerkleBranches) {
		return false
	}
	for i := range a.MerkleBranches {
		if !bytes.Equal(a.MerkleBranches[i], b.MerkleBranches[i]) {
			return false
		}
	}
	return true
}

type rpcMessage struct {
	ID     *uint64          `json:"id,omitempty"`
	Method string           `json:"method,omitempty"`
	Params *json.RawMessage `json:"params,omitempty"`
	Result *json.RawMessage `json:"result,omitempty"`
	Error  interface{}      `json:"error,omitempty"`
}

type rpcRequest struct {
	ID     uint64        `json:"id"`
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

type Client struct {
	Addr   string
	User   string
	Pass   string
	conn   net.Conn
	mu     sync.Mutex
	reqID  atomic.Uint64
	en2Seq atomic.Uint64

	extraNonce1    []byte
	extraNonce2Len int
	difficulty     float64
	target256      *big.Int
	targetU64      uint64
	targetState    atomic.Pointer[TargetState]
	lastJob        *Job

	OnNewJob       func(job *Job)
	AcceptedShares atomic.Uint64
	RejectedShares atomic.Uint64
}

func NewClient(addr, user, pass string, onNewJob func(job *Job)) *Client {
	addr = strings.TrimPrefix(addr, "stratum+tcp://")
	addr = strings.TrimPrefix(addr, "tcp://")
	c := &Client{
		Addr:     addr,
		User:     user,
		Pass:     pass,
		OnNewJob: onNewJob,
	}
	c.setDifficulty(0.01)
	return c
}

func (c *Client) CurrentTarget() *TargetState {
	return c.targetState.Load()
}

func doubleSHA256(b []byte) [32]byte {
	h1 := sha256.Sum256(b)
	return sha256.Sum256(h1[:])
}

// DiffToTarget matches p2pool-go-VTC: Diff 1 = 0x000000FFFF000000...
func DiffToTarget(diff float64) (*big.Int, uint64) {
	if diff <= 0 {
		diff = 0.01
	}
	maxTarget, _ := new(big.Int).SetString("000000FFFF000000000000000000000000000000000000000000000000000000", 16)
	fMax := new(big.Float).SetPrec(256).SetInt(maxTarget)
	fDiff := new(big.Float).SetPrec(256).SetFloat64(diff)
	fRes := new(big.Float).SetPrec(256).Quo(fMax, fDiff)
	target, _ := fRes.Int(nil)

	var buf [32]byte
	tBytes := target.Bytes()
	if len(tBytes) > 32 {
		for i := range buf {
			buf[i] = 0xFF
		}
	} else {
		copy(buf[32-len(tBytes):], tBytes)
	}
	top64 := binary.BigEndian.Uint64(buf[0:8])
	if top64 < ^uint64(0) {
		top64++ // +1 margin so GPU pre-filter never drops a boundary match
	}
	return target, top64
}

func TargetToDiff(powHashLE [32]byte) (*big.Int, float64) {
	var powBE [32]byte
	for i := 0; i < 32; i++ {
		powBE[i] = powHashLE[31-i]
	}
	powInt := new(big.Int).SetBytes(powBE[:])
	if powInt.Sign() <= 0 {
		return powInt, 0
	}
	maxTarget, _ := new(big.Int).SetString("000000FFFF000000000000000000000000000000000000000000000000000000", 16)
	q := new(big.Float).SetPrec(256).Quo(
		new(big.Float).SetPrec(256).SetInt(maxTarget),
		new(big.Float).SetPrec(256).SetInt(powInt),
	)
	d, _ := q.Float64()
	return powInt, d
}

func (c *Client) setDifficulty(diff float64) {
	t256, t64 := DiffToTarget(diff)
	c.difficulty = diff
	c.target256 = t256
	c.targetU64 = t64
	c.targetState.Store(&TargetState{
		Difficulty: diff,
		Target256:  t256,
		TargetU64:  t64,
	})
}

func (c *Client) ConnectAndRun() error {
	dialer := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	conn, err := dialer.Dial("tcp", c.Addr)
	if err != nil {
		return err
	}
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true) // Disable Nagle's algorithm for instant share submission
	}
	c.mu.Lock()
	c.conn = conn
	c.lastJob = nil
	c.mu.Unlock()
	defer conn.Close()

	if err := c.sendRequest(1, "mining.subscribe", []interface{}{"verthash-go-miner/1.0"}); err != nil {
		return err
	}
	if err := c.sendRequest(2, "mining.authorize", []interface{}{c.User, c.Pass}); err != nil {
		return err
	}
	c.reqID.Store(10)

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		c.handleMessage(&msg)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return fmt.Errorf("stratum connection closed by remote host")
}

func (c *Client) sendRequest(id uint64, method string, params []interface{}) error {
	req := rpcRequest{ID: id, Method: method, Params: params}
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err = c.conn.Write(b)
	return err
}

func (c *Client) handleMessage(msg *rpcMessage) {
	if msg.Method != "" {
		switch msg.Method {
		case "mining.set_difficulty":
			if msg.Params == nil {
				return
			}
			var p []float64
			if err := json.Unmarshal(*msg.Params, &p); err == nil && len(p) > 0 {
				c.mu.Lock()
				oldDiff := c.difficulty
				c.setDifficulty(p[0])
				if c.lastJob != nil {
					updated := *c.lastJob
					updated.Difficulty = c.difficulty
					updated.Target256 = c.target256
					updated.TargetU64 = c.targetU64
					c.lastJob = &updated
				}
				c.mu.Unlock()
				if oldDiff != p[0] {
					fmt.Printf("[Stratum] Vardiff updated -> %.5f\n", p[0])
				}
			}
		case "mining.notify":
			if msg.Params == nil {
				return
			}
			var raw []json.RawMessage
			if err := json.Unmarshal(*msg.Params, &raw); err != nil || len(raw) < 9 {
				return
			}
			var jobID, prevHashHex, coinb1Hex, coinb2Hex, versionHex, bitsHex, ntimeHex string
			var branchesHex []string
			var clean bool

			_ = json.Unmarshal(raw[0], &jobID)
			_ = json.Unmarshal(raw[1], &prevHashHex)
			_ = json.Unmarshal(raw[2], &coinb1Hex)
			_ = json.Unmarshal(raw[3], &coinb2Hex)
			_ = json.Unmarshal(raw[4], &branchesHex)
			_ = json.Unmarshal(raw[5], &versionHex)
			_ = json.Unmarshal(raw[6], &bitsHex)
			_ = json.Unmarshal(raw[7], &ntimeHex)
			_ = json.Unmarshal(raw[8], &clean)

			prevBytes, err := hex.DecodeString(prevHashHex)
			if err != nil || len(prevBytes) != 32 {
				return
			}
			var prevLE [32]byte
			for i := 0; i < 32; i += 4 {
				prevLE[i+0] = prevBytes[i+3]
				prevLE[i+1] = prevBytes[i+2]
				prevLE[i+2] = prevBytes[i+1]
				prevLE[i+3] = prevBytes[i+0]
			}

			coinb1, _ := hex.DecodeString(coinb1Hex)
			coinb2, _ := hex.DecodeString(coinb2Hex)
			branches := make([][]byte, len(branchesHex))
			for i, bh := range branchesHex {
				branches[i], _ = hex.DecodeString(bh)
			}

			verU64, _ := strconv.ParseUint(versionHex, 16, 32)
			bitsU64, _ := strconv.ParseUint(bitsHex, 16, 32)
			ntimeU64, _ := strconv.ParseUint(ntimeHex, 16, 32)

			c.mu.Lock()
			job := &Job{
				JobID:          jobID,
				PrevHashLE:     prevLE,
				Coinb1:         coinb1,
				Coinb2:         coinb2,
				MerkleBranches: branches,
				Version:        uint32(verU64),
				Bits:           uint32(bitsU64),
				NTimeHex:       ntimeHex,
				NTime:          uint32(ntimeU64),
				CleanJobs:      clean,
				ExtraNonce1:    append([]byte(nil), c.extraNonce1...),
				ExtraNonce2Len: c.extraNonce2Len,
				Difficulty:     c.difficulty,
				Target256:      c.target256,
				TargetU64:      c.targetU64,
			}
			job.SameTemplate = SameWork(c.lastJob, job) && !clean
			c.lastJob = job
			c.mu.Unlock()

			if c.OnNewJob != nil {
				c.OnNewJob(job)
			}
		}
		return
	}

	if msg.ID == nil {
		return
	}
	id := *msg.ID

	switch id {
	case 1: // mining.subscribe response
		if msg.Result == nil {
			return
		}
		var res []json.RawMessage
		if err := json.Unmarshal(*msg.Result, &res); err == nil && len(res) >= 3 {
			var en1Hex string
			var en2Len int
			_ = json.Unmarshal(res[1], &en1Hex)
			_ = json.Unmarshal(res[2], &en2Len)
			en1, _ := hex.DecodeString(en1Hex)
			if en2Len <= 0 {
				en2Len = 4
			}
			c.mu.Lock()
			c.extraNonce1 = en1
			c.extraNonce2Len = en2Len
			c.mu.Unlock()
			fmt.Printf("[Stratum] Subscribed (ExtraNonce1: %s, ExtraNonce2Size: %d)\n", en1Hex, en2Len)
		}
	case 2: // mining.authorize response
		var ok bool
		if msg.Result != nil {
			_ = json.Unmarshal(*msg.Result, &ok)
		}
		if ok {
			fmt.Printf("[Stratum] Authorized worker: %s\n", c.User)
		} else {
			fmt.Printf("[Stratum] Authorization FAILED for %s (error: %v)\n", c.User, msg.Error)
		}
	default: // mining.submit response
		var ok bool
		if msg.Result != nil {
			_ = json.Unmarshal(*msg.Result, &ok)
		}
		if ok && msg.Error == nil {
			acc := c.AcceptedShares.Add(1)
			rej := c.RejectedShares.Load()
			eff := (float64(acc) / float64(acc+rej)) * 100.0
			fmt.Printf("[Stratum] SHARE ACCEPTED! (Total: %d accepted, %d rejected | %.1f%% eff)\n", acc, rej, eff)
		} else {
			acc := c.AcceptedShares.Load()
			rej := c.RejectedShares.Add(1)
			fmt.Printf("[Stratum] SHARE REJECTED (Total: %d accepted, %d rejected | err: %v)\n", acc, rej, msg.Error)
		}
	}
}

func (c *Client) NextExtraNonce2(size int) ([]byte, string) {
	if size <= 0 {
		size = 4
	}
	v := c.en2Seq.Add(1)
	buf := make([]byte, size)
	if size >= 4 {
		binary.LittleEndian.PutUint32(buf[0:4], uint32(v))
	} else {
		for i := 0; i < size; i++ {
			buf[i] = byte(v >> (8 * i))
		}
	}
	return buf, hex.EncodeToString(buf)
}

func (j *Job) BuildHeader(extraNonce2 []byte) [80]byte {
	cbLen := len(j.Coinb1) + len(j.ExtraNonce1) + len(extraNonce2) + len(j.Coinb2)
	cb := make([]byte, 0, cbLen)
	cb = append(cb, j.Coinb1...)
	cb = append(cb, j.ExtraNonce1...)
	cb = append(cb, extraNonce2...)
	cb = append(cb, j.Coinb2...)

	merkleRoot := doubleSHA256(cb)
	var branchBuf [64]byte
	for _, br := range j.MerkleBranches {
		copy(branchBuf[0:32], merkleRoot[:])
		copy(branchBuf[32:64], br)
		merkleRoot = doubleSHA256(branchBuf[:])
	}

	var hdr [80]byte
	binary.LittleEndian.PutUint32(hdr[0:4], j.Version)
	copy(hdr[4:36], j.PrevHashLE[:])
	copy(hdr[36:68], merkleRoot[:])
	binary.LittleEndian.PutUint32(hdr[68:72], j.NTime)
	binary.LittleEndian.PutUint32(hdr[72:76], j.Bits)
	return hdr
}

func (c *Client) SubmitShare(jobID, extraNonce2Hex, nTimeHex string, nonce uint32) error {
	id := c.reqID.Add(1)
	nonceHex := fmt.Sprintf("%08x", nonce)
	return c.sendRequest(id, "mining.submit", []interface{}{
		c.User,
		jobID,
		extraNonce2Hex,
		nTimeHex,
		nonceHex,
	})
}

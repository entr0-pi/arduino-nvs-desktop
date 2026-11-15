package nvs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go.bug.st/serial"
	"io"
	"math"
	"strings"
	"sync"
	"time"
)

const (
	stx       byte = 0x02
	etx       byte = 0x03
	sep       byte = '#'
	crcHexLen      = 4

	maxFrame     = 512          // STX..ETX inclusive, same limit as the firmware
	maxBody      = maxFrame - 2 // bytes between STX and ETX
	maxKeyLen    = 15           // NVS key and namespace limit
	maxStringLen = 256          // longest string value, in bytes

	baudRate = 115200 // ignored by the ESP32-C3 native USB-CDC, harmless

	// Serial reads block for at most this long. Reply deadlines are
	// therefore accurate to about this granularity.
	pollInterval = 50 * time.Millisecond
)

// The only ops the firmware accepts (arduino.cpp executeCommand); any other
// op is answered with invalid_request.
const (
	opHello = "hello"
	opSet   = "set"
	opGet   = "get"
	opErase = "erase"

	statusOK = "ok"
)

// Error codes sent by the firmware in reply.message.
const (
	codeNotSynced      = "not_synced"
	codeInvalidRequest = "invalid_request"
	codeReadOnly       = "read_only" // namespace fixed by a FORCE_* build flag
	codeForbidden      = "forbidden" // runtime-state key (lock_*), never accessible
	codeNotFound       = "not_found"
	codeNvsWriteFailed = "nvs_write_failed"
	codeUnsupported    = "unsupported_value"
)

// Config holds the timing parameters (graph.md "Timing").
type Config struct {
	SkipHello    bool                                 // testing: leave firmware unsynchronized
	OnDiscard    func(reason string)                  // bad_crc, bad_json, stale_seq, oversize, interrupted
	OnFrame      func(direction string, frame []byte) // exact wire bytes; callback must not call Client
	OnNoise      func(data []byte)                    // bytes outside frames, including boot output
	BootDelay    time.Duration                        // wait after opening the port, in case it resets the board [D1]
	ReplyTimeout time.Duration                        // per-attempt reply deadline [C4]
	Attempts     int                                  // sends per request, the first one included [C9]
}

// DefaultConfig returns the values recommended in graph.md. On the
// ESP32-C3 native USB, opening the port may not reset the board: measure it,
// and lower BootDelay to about 200 ms if it doesn't (WAILS_GO.md §7).
func DefaultConfig() Config {
	return Config{BootDelay: 2 * time.Second, ReplyTimeout: time.Second, Attempts: 3}
}

// ValueType is the type of an NVS value as carried in the "type" field.
// The GNSS firmware stores only these four (I32, U32, U8, STR).
type ValueType string

const (
	TypeInt    ValueType = "int"    // int32  (NVS I32)
	TypeUint   ValueType = "uint"   // uint32 (NVS U32)
	TypeBool   ValueType = "bool"   // NVS U8, 0 or 1
	TypeString ValueType = "string" // at most maxStringLen bytes (NVS STR)
)

// Value is a typed value read from the device. Value holds an int32,
// uint32, bool or string according to Type.
type Value struct {
	Type  ValueType `json:"type"`
	Value any       `json:"value"`
}

// request is the JSON payload sent to the device.
type request struct {
	Op    string    `json:"op"`
	Seq   uint32    `json:"seq"`
	NS    string    `json:"ns,omitempty"` // set/get/erase
	Key   string    `json:"key,omitempty"`
	Type  ValueType `json:"type,omitempty"`
	Value any       `json:"value,omitempty"` // omitted only when nil
}

// Reply is the JSON payload received from the device. Seq is nil only for
// frame-level errors, where the device could not read our seq.
type Reply struct {
	Frame   []byte          `json:"frame"` // exact STX..ETX bytes (JSON encodes as base64)
	Seq     *uint32         `json:"seq"`
	Op      string          `json:"op"`
	NS      string          `json:"ns"`
	Key     string          `json:"key"`
	Status  string          `json:"status"`
	Message string          `json:"message"` // machine-readable error code
	Detail  string          `json:"detail"`  // optional human-readable reason
	Type    ValueType       `json:"type"`
	Value   json.RawMessage `json:"value"`
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

var (
	// ErrNotConnected is returned when no device session is open.
	ErrNotConnected = errors.New("nvs: not connected")

	// ErrNoReply means every attempt timed out or was rejected at frame
	// level. The device may or may not have executed the command [C9X].
	ErrNoReply = errors.New("nvs: no reply from device after all attempts; command outcome unknown")

	// ErrFrameTooLarge means the encoded request exceeds maxFrame.
	ErrFrameTooLarge = errors.New("nvs: request exceeds the maximum frame size")

	errTimeout = errors.New("nvs: reply deadline exceeded")
)

// ValidationError reports input rejected before anything is sent [C1X]:
// a bad field value, or a bad schema file (Field "file").
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return "nvs: invalid " + e.Field + ": " + e.Reason
}

// DeviceError is a well-formed error reply to our request: invalid_request,
// read_only, forbidden, not_found, nvs_write_failed... [S3]
type DeviceError struct {
	Op, NS, Key string
	Code        string // reply.message
	Detail      string // reply.detail, may be empty
}

func (e *DeviceError) Error() string {
	target := e.Op
	switch {
	case e.Key != "":
		target += " " + e.NS + "/" + e.Key
	case e.NS != "":
		target += " " + e.NS
	}
	msg := fmt.Sprintf("nvs: device rejected %s: %s", target, e.Code)
	if e.Detail != "" {
		msg += " (" + e.Detail + ")"
	}
	return msg
}

// frameRejectedError is a reply without seq: the device could not read our
// frame (crc_mismatch, bad_json...). The frame is resent [C7].
type frameRejectedError struct{ code string }

func (e frameRejectedError) Error() string { return "nvs: device rejected frame: " + e.code }

// ---------------------------------------------------------------------------
// Framing
// ---------------------------------------------------------------------------

// crc16 is CRC16-CCITT: poly 0x1021, init 0xFFFF, no reflection, no final
// XOR. It is bitwise on purpose, to mirror the firmware exactly. Frames are
// at most 512 bytes, so a lookup table would gain nothing measurable.
func crc16(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

const hexDigits = "0123456789ABCDEF"

// encodeFrame wraps payload as STX + payload + '#' + CRC hex + ETX.
func encodeFrame(payload []byte) ([]byte, error) {
	n := 1 + len(payload) + 1 + crcHexLen + 1
	if n > maxFrame {
		return nil, ErrFrameTooLarge
	}
	crc := crc16(payload)
	f := make([]byte, 0, n)
	f = append(f, stx)
	f = append(f, payload...)
	f = append(f, sep,
		hexDigits[crc>>12&0xF], hexDigits[crc>>8&0xF],
		hexDigits[crc>>4&0xF], hexDigits[crc&0xF],
		etx)
	return f, nil
}

// decodeFrame splits a frame body (between STX and ETX) on its last '#'
// and verifies the CRC. It returns the JSON payload and whether it is
// intact [C6].
func decodeFrame(body []byte) ([]byte, bool) {
	i := bytes.LastIndexByte(body, sep)
	if i < 0 || len(body)-i-1 != crcHexLen {
		return nil, false
	}
	var got uint16
	for _, h := range body[i+1:] {
		v, ok := hexValue(h)
		if !ok {
			return nil, false
		}
		got = got<<4 | uint16(v)
	}
	payload := body[:i]
	return payload, got == crc16(payload)
}

func hexValue(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

// Client is one session with the device over a serial port. It is safe for
// concurrent use: requests are serialized so only one is in flight [C2].
// It knows nothing about the schema: callers pass resolved KeySpecs.
type Client struct {
	mu      sync.Mutex
	port    serial.Port // nil once closed
	cfg     Config
	nextSeq uint64 // uint64 detects uint32 exhaustion without silently replaying

	// Receive state, guarded by mu.
	readBuf [256]byte
	pending []byte // bytes of readBuf not yet parsed
	frame   []byte // body of the frame being assembled (reused)
	inFrame bool
}

// Open opens the port, waits for the board, flushes stale input and
// performs the hello handshake [D0-H4].
func Open(portName string, cfg Config) (*Client, error) {
	if cfg.Attempts < 1 {
		cfg.Attempts = 1
	}
	port, err := serial.Open(portName, &serial.Mode{
		BaudRate: baudRate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	})
	if err != nil {
		return nil, fmt.Errorf("nvs: open %s: %w", portName, err)
	}
	return NewClient(port, cfg)
}

// NewClient runs the session start on an already open port. Split from
// Open so tests can pass a fake serial.Port.
func NewClient(port serial.Port, cfg Config) (*Client, error) {
	if cfg.Attempts < 1 {
		cfg.Attempts = 1
	}
	// With a read timeout set, Read returns (0, nil) when it expires.
	// readFrame turns that into a deadline check.
	if err := port.SetReadTimeout(pollInterval); err != nil {
		port.Close()
		return nil, fmt.Errorf("nvs: set read timeout: %w", err)
	}

	// [D1] Opening the port may reset the board. Wait for the firmware,
	// then drop the boot log and any log lines buffered while closed.
	// Observe output during the boot delay before flushing stale input.
	if cfg.OnNoise != nil {
		until := time.Now().Add(cfg.BootDelay)
		buf := make([]byte, 256)
		for time.Now().Before(until) {
			n, err := port.Read(buf)
			if err != nil {
				port.Close()
				return nil, fmt.Errorf("nvs: boot read: %w", err)
			}
			if n > 0 {
				cfg.OnNoise(append([]byte(nil), buf[:n]...))
			}
		}
	} else {
		time.Sleep(cfg.BootDelay)
	}
	if err := port.ResetInputBuffer(); err != nil {
		port.Close()
		return nil, fmt.Errorf("nvs: flush input: %w", err)
	}

	c := &Client{port: port, cfg: cfg, frame: make([]byte, 0, maxBody)}
	c.mu.Lock()
	var err error
	if !cfg.SkipHello {
		err = c.helloLocked()
	}
	c.mu.Unlock()
	if err != nil {
		port.Close()
		return nil, fmt.Errorf("nvs: handshake: %w", err)
	}
	return c, nil
}

// Close ends the session. It waits for a request in flight to finish.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.port == nil {
		return nil
	}
	err := c.port.Close()
	c.port = nil
	return err
}

// Resync repeats the hello handshake. Normally it is not needed: a device
// reboot is detected through not_synced and handled automatically.
func (c *Client) Resync() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.port == nil {
		return ErrNotConnected
	}
	return c.helloLocked()
}

// Set stores value under spec with the schema's type. The value is
// validated and normalized first, so no invalid request reaches the device.
func (c *Client) Set(spec KeySpec, value any) error {
	v, err := NormalizeValue(spec.Type, value)
	if err != nil {
		return err
	}
	_, err = c.do(request{Op: opSet, NS: spec.NS, Key: spec.Key, Type: spec.Type, Value: v})
	return err
}

// Get reads spec from the device and checks the type against the schema.
func (c *Client) Get(spec KeySpec) (Value, error) {
	r, err := c.do(request{Op: opGet, NS: spec.NS, Key: spec.Key})
	if err != nil {
		return Value{}, err
	}
	return decodeValue(spec, r)
}

// Erase removes spec. The firmware's erase is idempotent: removing a key
// that is not stored still succeeds.
func (c *Client) Erase(spec KeySpec) error {
	_, err := c.do(request{Op: opErase, NS: spec.NS, Key: spec.Key})
	return err
}

// do runs one command under the client lock [C2] and maps the reply status
// [S0]. On not_synced (the device rebooted) it says hello again and retries
// once with a new seq. This is safe because the device did not execute the
// refused command [S2].
func (c *Client) do(req request) (*Reply, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.port == nil {
		return nil, ErrNotConnected
	}

	r, err := c.roundTrip(req)
	if err == nil && r.Status != statusOK && r.Message == codeNotSynced {
		if err = c.helloLocked(); err == nil {
			r, err = c.roundTrip(req)
		}
	}
	if err != nil {
		return nil, err
	}
	if r.Status != statusOK {
		return nil, &DeviceError{Op: req.Op, NS: req.NS, Key: req.Key, Code: r.Message, Detail: r.Detail}
	}
	return r, nil
}

// helloLocked performs the handshake [H0-H2]. It uses the next seq like
// any request, and the device then expects seq+1. Callers hold c.mu.
func (c *Client) helloLocked() error {
	r, err := c.roundTrip(request{Op: opHello})
	if err != nil {
		return err
	}
	if r.Status != statusOK {
		return &DeviceError{Op: opHello, Code: r.Message, Detail: r.Detail}
	}
	return nil
}

// roundTrip reserves a seq, encodes the frame ONCE [C3] and sends it until a
// reply carrying that seq arrives or the attempts run out [C4-C9]. Every
// attempt sends identical bytes, so the device recognizes a retry and
// answers from its cache instead of executing again. Callers hold c.mu.
func (c *Client) roundTrip(req request) (*Reply, error) {
	if c.nextSeq > math.MaxUint32 {
		return nil, &ValidationError{Field: "seq", Reason: "sequence exhausted; reopen the session"}
	}
	req.Seq = uint32(c.nextSeq)
	c.nextSeq++

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("nvs: encode request: %w", err)
	}
	frame, err := encodeFrame(payload)
	if err != nil {
		return nil, err
	}

	return c.exchangeFrame(req.Seq, frame)
}

// Exchange sends arbitrary fields without mapping device refusals or resyncing.
// The caller's map is never changed. Retries reuse exactly the same wire frame.
func (c *Client) Exchange(req map[string]any) (*Reply, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.port == nil {
		return nil, ErrNotConnected
	}
	fields := make(map[string]any, len(req)+1)
	for k, v := range req {
		fields[k] = v
	}
	seq := c.nextSeq
	if v, ok := fields["seq"]; ok {
		b, err := json.Marshal(v)
		var n uint32
		if err != nil || json.Unmarshal(b, &n) != nil || string(b) == "null" {
			return nil, &ValidationError{Field: "seq", Reason: "must be uint32"}
		}
		seq = uint64(n)
	}
	if seq > math.MaxUint32 {
		return nil, &ValidationError{Field: "seq", Reason: "sequence exhausted; reopen the session"}
	}
	fields["seq"] = uint32(seq)
	payload, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("nvs: encode request: %w", err)
	}
	frame, err := encodeFrame(payload)
	if err != nil {
		return nil, err
	}
	if seq >= c.nextSeq {
		c.nextSeq = seq + 1
	}
	return c.exchangeFrame(uint32(seq), frame)
}

func (c *Client) exchangeFrame(seq uint32, frame []byte) (*Reply, error) {
	for attempt := 0; attempt < c.cfg.Attempts; attempt++ {
		if c.cfg.OnFrame != nil {
			c.cfg.OnFrame("tx", append([]byte(nil), frame...))
		}
		if err := c.writeAll(frame); err != nil {
			return nil, err
		}
		r, err := c.awaitReply(seq, time.Now().Add(c.cfg.ReplyTimeout))
		if err == nil {
			return r, nil
		}
		var rejected frameRejectedError
		if errors.Is(err, errTimeout) || errors.As(err, &rejected) {
			continue // [C9] resend the same frame
		}
		return nil, err // I/O error: the port is unusable
	}
	return nil, ErrNoReply
}

// awaitReply reads frames until one answers seq or the deadline passes.
// Corrupt frames (including replies broken by a firmware log line) and
// replies to other seqs (late replies to earlier attempts or requests) are
// discarded [C5-C8].
func (c *Client) awaitReply(seq uint32, deadline time.Time) (*Reply, error) {
	for {
		body, err := c.readFrame(deadline)
		if err != nil {
			return nil, err
		}
		payload, ok := decodeFrame(body)
		frame := append([]byte{stx}, body...)
		frame = append(frame, etx)
		if c.cfg.OnFrame != nil {
			c.cfg.OnFrame("rx", append([]byte(nil), frame...))
		}
		if !ok {
			c.discard("bad_crc")
			continue // [C6] corrupt: keep waiting
		}
		var r Reply
		if err := json.Unmarshal(payload, &r); err != nil {
			c.discard("bad_json")
			continue // CRC ok but unreadable: treat as corrupt
		}
		if r.Seq == nil {
			return nil, frameRejectedError{code: r.Message} // [C7]
		}
		if *r.Seq != seq {
			c.discard("stale_seq")
			continue // [C8] stale
		}
		r.Frame = frame
		return &r, nil
	}
}

// readFrame returns the body of the next complete frame. Bytes outside a
// frame (firmware log lines) are dropped, a new STX restarts the frame, and
// oversize frames are discarded. The returned slice is only valid until the
// next call. Callers hold c.mu.
func (c *Client) readFrame(deadline time.Time) ([]byte, error) {
	for {
		for len(c.pending) > 0 {
			b := c.pending[0]
			c.pending = c.pending[1:]
			switch {
			case b == stx:
				if c.inFrame {
					c.discard("interrupted")
				}
				c.frame = c.frame[:0]
				c.inFrame = true
			case !c.inFrame:
				// noise outside a frame
				if c.cfg.OnNoise != nil {
					c.cfg.OnNoise([]byte{b})
				}
			case b == etx:
				c.inFrame = false
				return c.frame, nil
			case len(c.frame) >= maxBody:
				c.discard("oversize")
				c.inFrame = false // oversize: drop
			default:
				c.frame = append(c.frame, b)
			}
		}

		if !time.Now().Before(deadline) {
			return nil, errTimeout
		}
		n, err := c.port.Read(c.readBuf[:]) // returns (0, nil) after pollInterval
		if err != nil {
			return nil, fmt.Errorf("nvs: read: %w", err)
		}
		c.pending = c.readBuf[:n]
	}
}

func (c *Client) discard(reason string) {
	if c.cfg.OnDiscard != nil {
		c.cfg.OnDiscard(reason)
	}
}

func (c *Client) writeAll(b []byte) error {
	for len(b) > 0 {
		n, err := c.port.Write(b)
		if err != nil {
			return fmt.Errorf("nvs: write: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("nvs: write: %w", io.ErrShortWrite)
		}
		b = b[n:]
	}
	return nil
}

// ---------------------------------------------------------------------------
// Validation and conversion [C1]
// ---------------------------------------------------------------------------

// validateKey is the cheap NVS name check (keys and namespaces).
func validateKey(key string) error {
	if len(key) == 0 || len(key) > maxKeyLen {
		return &ValidationError{Field: "key", Reason: fmt.Sprintf("must be 1-%d bytes", maxKeyLen)}
	}
	if strings.IndexByte(key, 0) >= 0 {
		return &ValidationError{Field: "key", Reason: "contains a NUL byte"}
	}
	return nil
}

// NormalizeValue checks value against t and returns it in the form sent on
// the wire. Values from the Wails frontend arrive as JSON types, so numbers
// are float64. Go integer types and json.Number are accepted too. The error
// never echoes the value.
func NormalizeValue(t ValueType, value any) (any, error) {
	switch t {
	case TypeBool:
		if b, ok := value.(bool); ok {
			return b, nil
		}
	case TypeString:
		if s, ok := value.(string); ok {
			if len(s) > maxStringLen {
				return nil, &ValidationError{Field: "value", Reason: fmt.Sprintf("string longer than %d bytes", maxStringLen)}
			}
			if strings.IndexByte(s, 0) >= 0 {
				return nil, &ValidationError{Field: "value", Reason: "string contains a NUL byte"}
			}
			return s, nil
		}
	case TypeInt:
		if n, ok := asInt64(value); ok && n >= math.MinInt32 && n <= math.MaxInt32 {
			return n, nil
		}
	case TypeUint:
		if n, ok := asInt64(value); ok && n >= 0 && n <= math.MaxUint32 {
			return n, nil
		}
	default:
		return nil, &ValidationError{Field: "type", Reason: fmt.Sprintf("unknown type %q", t)}
	}
	return nil, &ValidationError{Field: "value", Reason: fmt.Sprintf("not a valid %s", t)}
}

// asInt64 accepts integer types and integral float64 values (JavaScript
// numbers) up to 2^53.
func asInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case uint32:
		return int64(x), true
	case float64:
		if x == math.Trunc(x) && math.Abs(x) <= 1<<53 {
			return int64(x), true
		}
	case json.Number:
		n, err := x.Int64()
		return n, err == nil
	}
	return 0, false
}

// decodeValue converts a get reply into a typed Value, refusing a type that
// differs from the schema's.
func decodeValue(spec KeySpec, r *Reply) (Value, error) {
	if len(r.Value) == 0 || string(r.Value) == "null" {
		return Value{}, fmt.Errorf("nvs: %s/%s: reply has no value", spec.NS, spec.Key)
	}
	if r.Type != spec.Type {
		return Value{}, fmt.Errorf("nvs: %s/%s: device type %q, schema type %q", spec.NS, spec.Key, r.Type, spec.Type)
	}
	var (
		v   any
		err error
	)
	switch spec.Type {
	case TypeInt:
		v, err = unmarshalAs[int32](r.Value)
	case TypeUint:
		v, err = unmarshalAs[uint32](r.Value)
	case TypeBool:
		v, err = unmarshalAs[bool](r.Value)
	case TypeString:
		v, err = unmarshalAs[string](r.Value)
	default:
		return Value{}, fmt.Errorf("nvs: unknown type %q", spec.Type)
	}
	if err != nil {
		return Value{}, fmt.Errorf("nvs: decode %s/%s: %w", spec.NS, spec.Key, err)
	}
	return Value{Type: spec.Type, Value: v}, nil
}

func unmarshalAs[T any](raw json.RawMessage) (any, error) {
	var v T
	err := json.Unmarshal(raw, &v)
	return v, err
}

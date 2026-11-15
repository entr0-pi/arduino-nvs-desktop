// App is the Wails service over the shared stage 2 backend.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"nvs-desktop/nvs"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"go.bug.st/serial"
)

const (
	evtConnection = "nvs:connection" // payload: ConnectionEvent
	evtUpdated    = "nvs:updated"    // payload: KeyEvent (after a successful set)
	evtErased     = "nvs:erased"     // payload: KeyEvent without value
	evtSchema     = "nvs:schema"     // payload: key count (after a schema load)
)

// Connection states carried by ConnectionEvent.State.
const (
	connConnecting   = "connecting"
	connConnected    = "connected"
	connDisconnected = "disconnected"
	connError        = "error"
)

// ConnectionEvent is the payload of evtConnection.
type ConnectionEvent struct {
	State string `json:"state"`
	Port  string `json:"port,omitempty"`
	Error string `json:"error,omitempty"`
}

// KeyEvent is the payload of evtUpdated and evtErased.
type KeyEvent struct {
	NS    string     `json:"ns"`
	Key   string     `json:"key"`
	Value *nvs.Value `json:"value,omitempty"`
}

// KeyResult is one key of ReadNamespace: a value, or an error with its code.
type KeyResult struct {
	Value *nvs.Value `json:"value,omitempty"`
	Error string     `json:"error,omitempty"`
	Code  string     `json:"code,omitempty"`
}

// SaveResult is the outcome of a successful SaveNamespace.
type SaveResult struct {
	Written []string `json:"written"`
}

const (
	configJSONName = "nvs-config.json"
	configFilePerm = 0o600 // the file contains passwords in clear text
)

// App is the Wails v3 service. Its exported methods are callable from
// JavaScript.
type App struct {
	opMu       sync.Mutex
	active     atomic.Int32
	cfg        nvs.Config
	mu         sync.Mutex // guards client, portName, schema
	client     *nvs.Client
	portName   string      // set by Connect, reported in connection events
	schema     *nvs.Schema // set by LoadSchemaCSV / LoadConfigJSON; survives reconnects
	schemaPath string

	// Hooks replaced by tests. Defaults use the real serial port, the Wails
	// dialogs and the Wails event bus.
	open     func(port string, cfg nvs.Config) (*nvs.Client, error)
	pickOpen func(title, pattern string) (string, error) // "" = cancelled
	pickSave func(name string) (string, error)           // "" = cancelled
	emitFn   func(event string, data any)
}

// NewApp creates the service to register in main.go.
func NewApp() *App {
	return &App{
		cfg:      nvs.DefaultConfig(),
		open:     nvs.Open,
		pickOpen: dialogOpenFile,
		pickSave: dialogSaveFile,
		emitFn:   wailsEmit,
	}
}

// ServiceStartup is the Wails v3 service startup hook.
func (a *App) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	return nil
}

// ServiceShutdown is the Wails v3 service shutdown hook.
func (a *App) ServiceShutdown() error {
	_ = a.Disconnect()
	return nil
}

func (a *App) emit(event string, data any) {
	if a.emitFn != nil {
		a.emitFn(event, data)
	}
}

func wailsEmit(event string, data any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(event, data)
	}
}

func dialogOpenFile(title, pattern string) (string, error) {
	app := application.Get()
	if app == nil {
		return "", errors.New("nvs: Wails application not running")
	}
	path, err := app.Dialog.OpenFile().
		SetTitle(title).
		CanChooseFiles(true).
		AddFilter(title, pattern).
		PromptForSingleSelection()
	return normalizeDialogResult(path, err)
}

func dialogSaveFile(name string) (string, error) {
	app := application.Get()
	if app == nil {
		return "", errors.New("nvs: Wails application not running")
	}
	path, err := app.Dialog.SaveFile().
		SetFilename(name).
		AddFilter("Configuration JSON", "*.json").
		PromptForSingleSelection()
	return normalizeDialogResult(path, err)
}

// Wails v3.1.0's Windows dialog implementation returns an unexported internal
// ErrorCancelled sentinel. Normalize that exact native result here; it is not
// a serial error and must not turn an operator cancellation into a UI error.
func normalizeDialogResult(path string, err error) (string, error) {
	if runtime.GOOS == "windows" && path == "" && err != nil && err.Error() == "cancelled by user" {
		return "", nil
	}
	return path, err
}

// ---------------------------------------------------------------------------
// Errors returned to the frontend
// ---------------------------------------------------------------------------

// uiErrorPayload is what the frontend parses from a rejected promise.
type uiErrorPayload struct {
	Code    string   `json:"code"`
	Detail  string   `json:"detail"`
	Op      string   `json:"op,omitempty"`
	NS      string   `json:"ns,omitempty"`
	Key     string   `json:"key,omitempty"`
	Written []string `json:"written,omitempty"` // SaveNamespace: keys stored before the failure
}

// uiError carries the payload. Wails v3 rejects the promise with
// message = Error() and cause = the error marshalled to JSON, so both carry
// the same object: the frontend can read error.cause directly, or parse
// error.message.
type uiError struct{ p uiErrorPayload }

func (e *uiError) Error() string {
	b, _ := json.Marshal(e.p)
	return string(b)
}

// MarshalJSON makes the Wails "cause" field the payload instead of {}.
func (e *uiError) MarshalJSON() ([]byte, error) { return json.Marshal(e.p) }

// Go-side codes, next to the firmware codes in the same "code" field.
const (
	uiNoSchema     = "no_schema"     // nothing sent
	uiValidation   = "validation"    // nothing sent
	uiNotConnected = "not_connected" // no session, or the connection was lost
	uiNoReply      = "no_reply"      // outcome unknown
	uiUnknown      = "unknown"
)

// connectionLost reports errors after which the session is unusable:
// anything that isn't a clean refusal or a timeout.
func connectionLost(err error) bool {
	var ve *nvs.ValidationError
	var de *nvs.DeviceError
	switch {
	case err == nil, errors.As(err, &ve), errors.As(err, &de),
		errors.Is(err, nvs.ErrNoReply), errors.Is(err, nvs.ErrNoSchema),
		errors.Is(err, nvs.ErrFrameTooLarge), errors.Is(err, nvs.ErrNotConnected):
		return false
	}
	return true
}

// toUIError builds the payload. It never touches App state, so it is safe
// to call with a.mu held.
func toUIError(err error) error {
	if err == nil {
		return nil
	}
	if ue, ok := err.(*uiError); ok {
		return ue
	}
	p := uiErrorPayload{Code: uiUnknown, Detail: err.Error()}
	var ve *nvs.ValidationError
	var de *nvs.DeviceError
	switch {
	case errors.Is(err, nvs.ErrNoSchema):
		p.Code = uiNoSchema
	case errors.As(err, &ve):
		p.Code, p.Detail = uiValidation, ve.Field+": "+ve.Reason
	case errors.Is(err, nvs.ErrFrameTooLarge):
		p.Code, p.Detail = uiValidation, "value: too long once encoded (512-byte frame limit)"
	case errors.As(err, &de):
		p.Code, p.Detail, p.Op, p.NS, p.Key = de.Code, de.Detail, de.Op, de.NS, de.Key
	case errors.Is(err, nvs.ErrNoReply):
		p.Code = uiNoReply
	case errors.Is(err, nvs.ErrNotConnected), connectionLost(err):
		p.Code = uiNotConnected
	}
	return &uiError{p}
}

// result is the single exit point for errors returned to the frontend.
// After a lost connection it drops client c and emits evtConnection error.
// Callers must not hold a.mu.
func (a *App) result(c *nvs.Client, err error) error {
	if err != nil && c != nil && (connectionLost(err) || errors.Is(err, nvs.ErrNotConnected)) {
		a.dropClient(c, err)
	}
	return toUIError(err)
}

// dropClient closes c if it is still the current client.
func (a *App) dropClient(c *nvs.Client, cause error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client != c {
		return
	}
	_ = c.Close()
	a.client = nil
	a.emit(evtConnection, ConnectionEvent{State: connError, Port: a.portName, Error: cause.Error()})
}

// ---------------------------------------------------------------------------
// Connection
// ---------------------------------------------------------------------------

// ListPorts returns the serial ports available for Connect.
func (a *App) ListPorts() ([]string, error) {
	ports, err := serial.GetPortsList()
	if ports == nil {
		ports = []string{}
	}
	return ports, toUIError(err)
}

// Connect opens a session on portName and closes any previous one.
// Progress is reported through evtConnection.
func (a *App) Connect(portName string) error {
	done := a.begin()
	defer done()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client != nil {
		_ = a.client.Close()
		a.client = nil
	}

	a.emit(evtConnection, ConnectionEvent{State: connConnecting, Port: portName})
	c, err := a.open(portName, a.cfg)
	if err != nil {
		a.emit(evtConnection, ConnectionEvent{State: connError, Port: portName, Error: err.Error()})
		return toUIError(err)
	}
	a.client = c
	a.portName = portName
	a.emit(evtConnection, ConnectionEvent{State: connConnected, Port: portName})
	return nil
}

// Disconnect closes the current session, if any.
func (a *App) Disconnect() error {
	done := a.begin()
	defer done()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client == nil {
		return nil
	}
	err := a.client.Close()
	a.client = nil
	a.emit(evtConnection, ConnectionEvent{State: connDisconnected, Port: a.portName})
	return toUIError(err)
}

// ---------------------------------------------------------------------------
// nvs.Schema
// ---------------------------------------------------------------------------

// LoadSchemaCSV parses the CSV at path and makes it the active schema. An
// empty path opens a native dialog; cancellation returns (nil, nil) and
// keeps the current schema. It never talks to the device.
func (a *App) LoadSchemaCSV(path string) (*nvs.Schema, error) {
	done := a.begin()
	defer done()
	return a.loadSchema(path, "CSV schema", "*.csv", ".csv", nvs.ParseSchemaCSV)
}

// LoadConfigJSON parses a version-2 configuration JSON and makes it the
// active schema, with its values as proposals. Same rules as LoadSchemaCSV.
func (a *App) LoadConfigJSON(path string) (*nvs.Schema, error) {
	done := a.begin()
	defer done()
	return a.loadSchema(path, "Configuration JSON", "*.json", ".json", nvs.ParseConfigJSON)
}

func (a *App) loadSchema(path, title, pattern, ext string, parse func(io.Reader) (*nvs.Schema, error)) (*nvs.Schema, error) {
	if path == "" {
		p, err := a.pickOpen(title, pattern)
		if err != nil {
			return nil, toFileUIError(err)
		}
		if p == "" {
			return nil, nil // cancelled
		}
		path = p
	}
	data, err := nvs.ReadSchemaFile(path, ext)
	if err != nil {
		return nil, toFileUIError(err)
	}
	s, err := parse(bytes.NewReader(data))
	if err != nil {
		return nil, toUIError(err)
	}
	a.mu.Lock()
	a.schema = s
	a.schemaPath = path
	a.mu.Unlock()
	a.emit(evtSchema, len(s.Keys))
	return s, nil
}

// GetSchema returns the active schema, or nil if none is loaded.
func (a *App) GetSchema() *nvs.Schema {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.schema
}

// GetSchemaFileName exposes the chosen filename after a native Browse dialog.
func (a *App) GetSchemaFileName() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.schemaPath == "" {
		return ""
	}
	return filepath.Base(a.schemaPath)
}

// SaveConfigJSON writes values (ns → key → value) as a version-2 JSON
// through a native Save dialog. Everything is validated before the dialog
// opens; cancellation returns nil. Passwords are written in clear text: the
// frontend confirms before calling it.
func (a *App) SaveConfigJSON(values map[string]map[string]any) error {
	done := a.begin()
	defer done()
	data, err := nvs.MarshalConfigJSON(a.GetSchema(), values)
	if err != nil {
		return toUIError(err)
	}
	path, err := a.pickSave(configJSONName)
	if err != nil {
		return toFileUIError(err)
	}
	if path == "" {
		return nil // cancelled
	}
	if err := os.WriteFile(path, data, configFilePerm); err != nil {
		return toFileUIError(fmt.Errorf("nvs: save configuration: %w", err))
	}
	return nil
}

// Filesystem and dialog failures do not indicate that the USB session failed.
func toFileUIError(err error) error {
	var validation *nvs.ValidationError
	if err == nil || errors.As(err, &validation) || errors.Is(err, nvs.ErrNoSchema) {
		return toUIError(err)
	}
	return &uiError{uiErrorPayload{Code: uiUnknown, Detail: err.Error()}}
}

// ---------------------------------------------------------------------------
// NVS operations
// ---------------------------------------------------------------------------

// session returns the current client and schema together.
func (a *App) session() (*nvs.Client, *nvs.Schema, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.schema == nil {
		return nil, nil, nvs.ErrNoSchema
	}
	if a.client == nil {
		return nil, a.schema, nvs.ErrNotConnected
	}
	return a.client, a.schema, nil
}

// ReadNamespace reads every schema key of ns. Device refusals and timeouts
// are reported per key; a lost connection fails the whole call.
func (a *App) ReadNamespace(ns string) (map[string]KeyResult, error) {
	done := a.begin()
	defer done()
	c, s, err := a.session()
	if err != nil {
		return nil, toUIError(err)
	}
	keys := []nvs.KeySpec{}
	for _, spec := range s.Keys {
		if spec.NS == ns {
			keys = append(keys, spec)
		}
	}
	if len(keys) == 0 {
		return nil, toUIError(&nvs.ValidationError{Field: "ns", Reason: fmt.Sprintf("%q has no keys in the loaded schema", ns)})
	}
	out := make(map[string]KeyResult, len(keys))
	for _, spec := range keys {
		v, err := c.Get(spec)
		if err == nil {
			out[spec.Key] = KeyResult{Value: &v}
			continue
		}
		if connectionLost(err) || errors.Is(err, nvs.ErrNotConnected) {
			return nil, a.result(c, err)
		}
		p := toUIError(err).(*uiError).p
		detail := p.Detail
		if detail == "" {
			detail = err.Error()
		}
		out[spec.Key] = KeyResult{Error: detail, Code: p.Code}
	}
	return out, nil
}

// SaveNamespace writes several keys of one namespace with one set each:
//  1. validate every entry; on any error nothing is sent;
//  2. write in sorted key order, stopping at the first error (the error
//     payload lists the keys written so far).
func (a *App) SaveNamespace(ns string, values map[string]any) (SaveResult, error) {
	done := a.begin()
	defer done()
	c, s, err := a.session()
	if err != nil {
		return SaveResult{}, toUIError(err)
	}
	written, err := nvs.SaveNamespace(c, s, ns, values, func(spec nvs.KeySpec, value any) {
		a.emit(evtUpdated, KeyEvent{NS: spec.NS, Key: spec.Key, Value: &nvs.Value{Type: spec.Type, Value: value}})
	})
	if err != nil {
		return SaveResult{}, a.withWritten(a.result(c, err), written)
	}
	return SaveResult{Written: written}, nil
}

// withWritten adds the keys stored before a failure to the error payload.
func (a *App) withWritten(err error, written []string) error {
	if ue, ok := err.(*uiError); ok {
		ue.p.Written = append([]string{}, written...)
	}
	return err
}

// SetNVSKey stores one value with the schema's type.
func (a *App) SetNVSKey(ns, key string, value any) error {
	done := a.begin()
	defer done()
	c, s, err := a.session()
	if err != nil {
		return toUIError(err)
	}
	spec, err := s.Lookup(ns, key)
	if err != nil {
		return toUIError(err)
	}
	if err := c.Set(spec, value); err != nil {
		return a.result(c, err)
	}
	v, _ := nvs.NormalizeValue(spec.Type, value)
	a.emit(evtUpdated, KeyEvent{NS: ns, Key: key, Value: &nvs.Value{Type: spec.Type, Value: v}})
	return nil
}

// GetNVSKey reads one value from the device.
func (a *App) GetNVSKey(ns, key string) (nvs.Value, error) {
	done := a.begin()
	defer done()
	c, s, err := a.session()
	if err != nil {
		return nvs.Value{}, toUIError(err)
	}
	spec, err := s.Lookup(ns, key)
	if err != nil {
		return nvs.Value{}, toUIError(err)
	}
	v, err := c.Get(spec)
	if err != nil {
		return nvs.Value{}, a.result(c, err)
	}
	return v, nil
}

// EraseNVSKey removes a key from the device. Erasing a key that is not
// stored succeeds; a later get returns not_found.
func (a *App) EraseNVSKey(ns, key string) error {
	done := a.begin()
	defer done()
	c, s, err := a.session()
	if err != nil {
		return toUIError(err)
	}
	spec, err := s.Lookup(ns, key)
	if err != nil {
		return toUIError(err)
	}
	if err := c.Erase(spec); err != nil {
		return a.result(c, err)
	}
	a.emit(evtErased, KeyEvent{NS: ns, Key: key})
	return nil
}

// begin serializes whole service operations, including schema changes and dialogs.
func (a *App) begin() func() {
	a.opMu.Lock()
	a.active.Add(1)
	return func() { a.active.Add(-1); a.opMu.Unlock() }
}

// buildDate is set by the Wails build tasks with -X main.buildDate=YYYY-MM-DD.
var buildDate = "Development build"

// GetAppInfo exposes the license, build date, and runtime metadata.
func (a *App) GetAppInfo() map[string]string {
	info := map[string]string{"name": "ESP32 NVS Key-Pair Manager", "version": buildDate, "platform": runtime.GOOS + "/" + runtime.GOARCH, "go": runtime.Version(), "wails": "Unknown", "license": "MIT"}
	if build, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range build.Deps {
			if dep.Path == "github.com/wailsapp/wails/v3" {
				info["wails"] = dep.Version
			}
		}
	}
	return info
}

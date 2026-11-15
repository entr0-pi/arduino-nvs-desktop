// Key schema of the desktop tool: which (namespace, key) pairs exist, their
// types, and proposed values for the draft (WAILS_GO.md §3).
//
// The schema comes from a file the user picks in the frontend:
//   - a CSV in the ESP-IDF NVS layout (key,type,encoding,value), or
//   - a version-2 configuration JSON, usually written by SaveConfigJSON.
//
// Both parsers share addRow, so they produce the same *Schema. The schema
// never reaches the device: the firmware validates every request on its own.
package nvs

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	maxSchemaFile = 64 << 10 // bytes, CSV and JSON
	maxSchemaKeys = 100      // data rows / JSON entries
	configVersion = 2        // current configuration JSON version
)

// ErrNoSchema is returned by every NVS operation until a schema is loaded.
var ErrNoSchema = errors.New("nvs: no schema loaded; load a CSV or JSON file first")

// KeySpec is one key of the loaded schema.
type KeySpec struct {
	NS   string    `json:"ns"`
	Key  string    `json:"key"`
	Type ValueType `json:"type"`
}

// Proposed is a value from the file, already converted to its type. It only
// fills the draft: nothing is written until the user confirms.
type Proposed struct {
	NS    string `json:"ns"`
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// Schema is the active key list. Keys keep the file order.
type Schema struct {
	Keys     []KeySpec  `json:"keys"`
	Proposed []Proposed `json:"proposed"`
	index    map[[2]string]KeySpec
}

// Lookup resolves (ns, key). A nil schema returns ErrNoSchema.
func (s *Schema) Lookup(ns, key string) (KeySpec, error) {
	if s == nil {
		return KeySpec{}, ErrNoSchema
	}
	spec, ok := s.index[[2]string{ns, key}]
	if !ok {
		return KeySpec{}, &ValidationError{Field: "key", Reason: fmt.Sprintf("%s/%s is not in the loaded schema", ns, key)}
	}
	return spec, nil
}

// keysOf returns the keys of ns in schema order.
func (s *Schema) keysOf(ns string) []KeySpec {
	var out []KeySpec
	for _, k := range s.Keys {
		if k.NS == ns {
			out = append(out, k)
		}
	}
	return out
}

func newSchema() *Schema { return &Schema{index: map[[2]string]KeySpec{}} }

// fileError reports a problem in an imported file. where is "line N" or
// "entry N"; the message never contains the value.
func fileError(where, format string, args ...any) error {
	return &ValidationError{Field: "file", Reason: where + ": " + fmt.Sprintf(format, args...)}
}

// validateNamespace accepts only the namespaces in Namespaces.
func validateNamespace(ns string) error {
	if !Namespaces[ns] {
		return fmt.Errorf("unknown namespace %q (expected gnss, wifi or ntrip)", ns)
	}
	return nil
}

// addRow applies the checks shared by both formats and appends the row.
// value is only used when hasValue is true.
func (s *Schema) addRow(ns, key string, t ValueType, value any, hasValue bool) error {
	if err := validateNamespace(ns); err != nil {
		return err
	}
	if err := validateKey(key); err != nil {
		return errors.New(err.(*ValidationError).Reason)
	}
	switch t {
	case TypeInt, TypeUint, TypeBool, TypeString:
	default:
		return fmt.Errorf("key %q: unsupported type %q", key, t)
	}
	id := [2]string{ns, key}
	if _, dup := s.index[id]; dup {
		return fmt.Errorf("duplicate key %s/%s", ns, key)
	}
	if len(s.Keys) >= maxSchemaKeys {
		return fmt.Errorf("more than %d keys", maxSchemaKeys)
	}
	spec := KeySpec{NS: ns, Key: key, Type: t}
	if hasValue {
		v, err := NormalizeValue(t, value)
		if err != nil {
			return fmt.Errorf("key %s/%s: value is not a valid %s", ns, key, t)
		}
		s.Proposed = append(s.Proposed, Proposed{NS: ns, Key: key, Value: v})
	}
	s.Keys = append(s.Keys, spec)
	s.index[id] = spec
	return nil
}

// ReadSchemaFile reads at most maxSchemaFile bytes of a regular file with
// the expected extension (".csv" or ".json", any case).
func ReadSchemaFile(path, ext string) ([]byte, error) {
	if !strings.EqualFold(filepath.Ext(path), ext) {
		return nil, &ValidationError{Field: "file", Reason: fmt.Sprintf("expected a %s file", ext)}
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("nvs: open schema file: %w", err)
	}
	if fi.IsDir() {
		return nil, &ValidationError{Field: "file", Reason: "is a directory"}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("nvs: open schema file: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSchemaFile+1))
	if err != nil {
		return nil, fmt.Errorf("nvs: read schema file: %w", err)
	}
	if len(data) > maxSchemaFile {
		return nil, &ValidationError{Field: "file", Reason: fmt.Sprintf("larger than %d KiB", maxSchemaFile>>10)}
	}
	return data, nil
}

// ---------------------------------------------------------------------------
// CSV (WAILS_GO.md §3.1-3.2)
// ---------------------------------------------------------------------------

// csvEncodings maps the CSV encoding column to the protocol type.
var csvEncodings = map[string]ValueType{
	"i32":    TypeInt,
	"u32":    TypeUint,
	"u8":     TypeBool,
	"string": TypeString,
}

// ParseSchemaCSV parses key,type,encoding,value rows. A namespace row starts
// a namespace; data rows add keys to it. An empty value means no proposal.
func ParseSchemaCSV(r io.Reader) (*Schema, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxSchemaFile+1))
	if err != nil {
		return nil, fmt.Errorf("nvs: read schema: %w", err)
	}
	if len(data) > maxSchemaFile {
		return nil, &ValidationError{Field: "file", Reason: fmt.Sprintf("larger than %d KiB", maxSchemaFile>>10)}
	}
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF")) // BOM written by spreadsheet tools

	cr := csv.NewReader(bytes.NewReader(data))
	cr.FieldsPerRecord = 4
	cr.TrimLeadingSpace = true

	s := newSchema()
	ns := ""
	for first := true; ; first = false {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			var pe *csv.ParseError
			if errors.As(err, &pe) {
				return nil, fileError(fmt.Sprintf("line %d", pe.Line), "%v", pe.Err)
			}
			return nil, fmt.Errorf("nvs: read schema: %w", err)
		}
		line, _ := cr.FieldPos(0)
		where := fmt.Sprintf("line %d", line)
		key, kind, enc, value := rec[0], rec[1], rec[2], rec[3]

		if first {
			if key != "key" || kind != "type" || enc != "encoding" || value != "value" {
				return nil, fileError(where, "header must be exactly key,type,encoding,value")
			}
			continue
		}
		switch kind {
		case "namespace":
			if err := validateNamespace(key); err != nil {
				return nil, fileError(where, "%v", err)
			}
			if enc != "" || value != "" {
				return nil, fileError(where, "namespace row must have empty encoding and value")
			}
			ns = key
		case "data":
			if ns == "" {
				return nil, fileError(where, "data row before any namespace row")
			}
			t, ok := csvEncodings[enc]
			if !ok {
				return nil, fileError(where, "unsupported encoding %q (expected i32, u32, u8 or string)", enc)
			}
			v, err := parseCSVValue(t, value)
			if err != nil {
				return nil, fileError(where, "key %s/%s: %v", ns, key, err)
			}
			if err := s.addRow(ns, key, t, v, value != ""); err != nil {
				return nil, fileError(where, "%v", err)
			}
		default:
			return nil, fileError(where, "unsupported row type %q (expected namespace or data)", kind)
		}
	}
	if len(s.Keys) == 0 {
		return nil, &ValidationError{Field: "file", Reason: "no data rows"}
	}
	return s, nil
}

// parseCSVValue converts the text of the value column. Empty text is "no
// proposal" and returns nil.
func parseCSVValue(t ValueType, text string) (any, error) {
	if text == "" {
		return nil, nil
	}
	switch t {
	case TypeInt:
		n, err := strconv.ParseInt(text, 10, 32)
		if err != nil {
			return nil, errors.New("value is not a decimal int32")
		}
		return n, nil
	case TypeUint:
		n, err := strconv.ParseUint(text, 10, 32)
		if err != nil {
			return nil, errors.New("value is not a decimal uint32")
		}
		return int64(n), nil
	case TypeBool:
		switch text {
		case "0":
			return false, nil
		case "1":
			return true, nil
		}
		return nil, errors.New("u8 value must be 0 or 1")
	}
	return text, nil // TypeString; length and NUL checked by normalizeValue
}

// ---------------------------------------------------------------------------
// Configuration JSON, version 2 (WAILS_GO.md §3.5)
// ---------------------------------------------------------------------------

type configFile struct {
	Version int           `json:"version"`
	Entries []configEntry `json:"entries"`
}

type configEntry struct {
	NS    string          `json:"ns"`
	Key   string          `json:"key"`
	Type  ValueType       `json:"type"`
	Value json.RawMessage `json:"value"`
}

// ParseConfigJSON parses a self-contained configuration: every entry has its
// namespace, key, type and value.
func ParseConfigJSON(r io.Reader) (*Schema, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxSchemaFile+1))
	if err != nil {
		return nil, fmt.Errorf("nvs: read configuration: %w", err)
	}
	if len(data) > maxSchemaFile {
		return nil, &ValidationError{Field: "file", Reason: fmt.Sprintf("larger than %d KiB", maxSchemaFile>>10)}
	}
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var cf configFile
	if err := dec.Decode(&cf); err != nil {
		return nil, &ValidationError{Field: "file", Reason: "not a valid configuration JSON: " + jsonErrorText(err)}
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, &ValidationError{Field: "file", Reason: "unexpected data after the JSON object"}
	}
	switch {
	case cf.Version == 1:
		return nil, &ValidationError{Field: "file", Reason: "version 1 (no namespace, standalone firmware) is not supported; expected version 2"}
	case cf.Version != configVersion:
		return nil, &ValidationError{Field: "file", Reason: fmt.Sprintf("unsupported version %d; expected %d", cf.Version, configVersion)}
	}
	if len(cf.Entries) < 1 || len(cf.Entries) > maxSchemaKeys {
		return nil, &ValidationError{Field: "file", Reason: fmt.Sprintf("entries must hold 1-%d items", maxSchemaKeys)}
	}

	s := newSchema()
	for i, e := range cf.Entries {
		where := fmt.Sprintf("entry %d (%s/%s)", i, e.NS, e.Key)
		if e.Type == "" {
			return nil, fileError(where, "missing type")
		}
		if len(e.Value) == 0 || string(e.Value) == "null" {
			return nil, fileError(where, "missing value")
		}
		v, err := decodeJSONValue(e.Type, e.Value)
		if err != nil {
			return nil, fileError(where, "%v", err)
		}
		if err := s.addRow(e.NS, e.Key, e.Type, v, true); err != nil {
			return nil, fileError(where, "%v", err)
		}
	}
	return s, nil
}

// decodeJSONValue checks that the JSON type matches t exactly: a number for
// int/uint, true/false for bool, a string for string.
func decodeJSONValue(t ValueType, raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, errors.New("unreadable value")
	}
	ok := false
	switch v.(type) {
	case json.Number:
		ok = t == TypeInt || t == TypeUint
	case bool:
		ok = t == TypeBool
	case string:
		ok = t == TypeString
	}
	if !ok {
		return nil, fmt.Errorf("value has the wrong JSON type for %s", t)
	}
	return v, nil // range and length checked by normalizeValue in addRow
}

// jsonErrorText describes a decode error without quoting the input.
func jsonErrorText(err error) string {
	var se *json.SyntaxError
	var te *json.UnmarshalTypeError
	switch {
	case errors.As(err, &se):
		return fmt.Sprintf("syntax error at byte %d", se.Offset)
	case errors.As(err, &te):
		return fmt.Sprintf("field %q has the wrong type", te.Field)
	case strings.HasPrefix(err.Error(), "json: unknown field"):
		return strings.TrimPrefix(err.Error(), "json: ")
	}
	return "unreadable JSON"
}

// MarshalConfigJSON builds a version-2 configuration from the schema and the
// values (ns → key → value). Only keys with a value are written, in schema
// order, so the file loads back to the same keys and values.
func MarshalConfigJSON(s *Schema, values map[string]map[string]any) ([]byte, error) {
	if s == nil {
		return nil, ErrNoSchema
	}
	for ns, kv := range values {
		for key := range kv {
			if _, err := s.Lookup(ns, key); err != nil {
				return nil, err
			}
		}
	}
	cf := configFile{Version: configVersion}
	for _, spec := range s.Keys {
		v, ok := values[spec.NS][spec.Key]
		if !ok {
			continue
		}
		nv, err := NormalizeValue(spec.Type, v)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(nv)
		if err != nil {
			return nil, fmt.Errorf("nvs: encode %s/%s: %w", spec.NS, spec.Key, err)
		}
		cf.Entries = append(cf.Entries, configEntry{NS: spec.NS, Key: spec.Key, Type: spec.Type, Value: raw})
	}
	if len(cf.Entries) == 0 {
		return nil, &ValidationError{Field: "values", Reason: "nothing to save"}
	}
	out, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("nvs: encode configuration: %w", err)
	}
	return append(out, '\n'), nil
}

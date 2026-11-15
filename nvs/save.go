package nvs

import "sort"

// Namespaces lists the namespaces accepted by configuration schemas.
var Namespaces = map[string]bool{"gnss": true, "wifi": true, "ntrip": true}

// SaveNamespace validates all values before writing in sorted key order.
// Written includes successful keys even when a later write fails.
func SaveNamespace(c *Client, s *Schema, ns string, values map[string]any, onWritten func(KeySpec, any)) (written []string, err error) {
	if s == nil {
		return nil, ErrNoSchema
	}
	if c == nil {
		return nil, ErrNotConnected
	}
	if !Namespaces[ns] {
		return nil, &ValidationError{Field: "ns", Reason: "unknown namespace"}
	}
	if len(values) == 0 {
		return nil, &ValidationError{Field: "values", Reason: "nothing to write"}
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	specs := make([]KeySpec, len(keys))
	normalized := make([]any, len(keys))
	for i, k := range keys {
		specs[i], err = s.Lookup(ns, k)
		if err != nil {
			return nil, err
		}
		normalized[i], err = NormalizeValue(specs[i].Type, values[k])
		if err != nil {
			return nil, &ValidationError{Field: ns + "/" + k, Reason: err.(*ValidationError).Reason}
		}
	}
	written = []string{}
	for i, spec := range specs {
		if err = c.Set(spec, normalized[i]); err != nil {
			return written, err
		}
		written = append(written, spec.Key)
		if onWritten != nil {
			onWritten(spec, normalized[i])
		}
	}
	return written, nil
}

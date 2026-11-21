package cardigann

import (
	"fmt"

	"github.com/goccy/go-yaml"
)

// OrderedInputs keeps query parameters in the order the definition declared
// them. Some sites (Torrent Downloads, for example) reject a request when a
// specific parameter comes first, so alphabetical sorting breaks them.
type OrderedInputs struct {
	Keys   []string
	Values map[string]string
}

// UnmarshalYAML decodes the mapping while preserving declaration order.
func (o *OrderedInputs) UnmarshalYAML(unmarshal func(any) error) error {
	var ordered yaml.MapSlice
	if err := unmarshal(&ordered); err != nil {
		return err
	}
	o.Keys = make([]string, 0, len(ordered))
	o.Values = make(map[string]string, len(ordered))
	for _, item := range ordered {
		key, ok := item.Key.(string)
		if !ok {
			continue
		}
		if o.Values == nil {
			o.Values = map[string]string{}
		}
		o.Keys = append(o.Keys, key)
		switch v := item.Value.(type) {
		case nil:
			o.Values[key] = ""
		default:
			o.Values[key] = asStringValue(v)
		}
	}
	return nil
}

// MarshalYAML renders the mapping back in the declared order.
func (o OrderedInputs) MarshalYAML() (any, error) {
	out := make(yaml.MapSlice, 0, len(o.Keys))
	for _, key := range o.Keys {
		out = append(out, yaml.MapItem{Key: key, Value: o.Values[key]})
	}
	return out, nil
}

// Len reports how many inputs are declared.
func (o OrderedInputs) Len() int { return len(o.Keys) }

// Get returns one input value.
func (o OrderedInputs) Get(key string) (string, bool) {
	v, ok := o.Values[key]
	return v, ok
}

// asStringValue stringifies a YAML scalar without losing its text form.
func asStringValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		// Reuse RawValue's quoting rules by decoding through YAML is overkill for
		// scalars; fmt keeps numbers and strings intact.
		return sprintScalar(v)
	}
}

func sprintScalar(v any) string {
	return fmt.Sprint(v)
}

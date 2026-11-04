package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

// applySelect projects data down to the comma-separated dotted paths in
// selectors, so agents don't spend tokens on fields they won't read. Data is
// round-tripped through JSON, so SDK structs and raw responses behave alike.
//
// A path walks object keys ("owner.name"); where it meets an array it applies
// the rest of the path to every element ("data.id" over {"data":[...]} yields
// the ids). An array at the top level is projected element by element. Each
// result is keyed by its full path, and a path that matches nothing is null.
func applySelect(data any, selectors string) (any, error) {
	var paths []string
	for _, p := range strings.Split(selectors, ",") {
		if p = strings.TrimSpace(p); p != "" {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("--select needs at least one field, e.g. --select id,name")
	}

	var generic any
	if raw, ok := data.(json.RawMessage); ok {
		if err := json.Unmarshal(raw, &generic); err != nil {
			return nil, err
		}
	} else {
		b, err := json.Marshal(data)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &generic); err != nil {
			return nil, err
		}
	}

	if list, ok := generic.([]any); ok {
		out := make([]any, len(list))
		for i, item := range list {
			out[i] = project(item, paths)
		}
		return out, nil
	}
	return project(generic, paths), nil
}

func project(v any, paths []string) map[string]any {
	out := make(map[string]any, len(paths))
	for _, p := range paths {
		out[p] = walk(v, strings.Split(p, "."))
	}
	return out
}

func walk(v any, path []string) any {
	if len(path) == 0 {
		return v
	}
	switch t := v.(type) {
	case map[string]any:
		child, ok := t[path[0]]
		if !ok {
			return nil
		}
		return walk(child, path[1:])
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = walk(item, path)
		}
		return out
	}
	return nil
}

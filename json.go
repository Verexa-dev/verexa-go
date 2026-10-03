package verexa

import (
	"encoding/json"
	"strings"
)

type jsonObject map[string]json.RawMessage

func parseObject(raw []byte) (jsonObject, error) {
	var obj jsonObject
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func parseArray(raw json.RawMessage) []json.RawMessage {
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil {
		return nil
	}
	return arr
}

func (o jsonObject) str(key string) (string, bool) {
	var s string
	if json.Unmarshal(o[key], &s) != nil {
		return "", false
	}
	return s, true
}

func (o jsonObject) object(key string) jsonObject {
	obj, _ := parseObject(o[key])
	return obj
}

func (o jsonObject) set(key string, value any) {
	encoded, err := json.Marshal(value)
	if err == nil {
		o[key] = encoded
	}
}

// contentText reads a content field that is either a plain string or an
// array of parts carrying a "text" field. single reports whether the text came
// from exactly one place, so a rewrite can put it back.
func contentText(raw json.RawMessage) (text string, single bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	var texts []string
	for _, part := range parseArray(raw) {
		obj, err := parseObject(part)
		if err != nil {
			continue
		}
		if t, ok := obj.str("text"); ok && t != "" {
			texts = append(texts, t)
		}
	}
	return strings.Join(texts, "\n"), len(texts) == 1
}

// setContentText rewrites the text that contentText read, when it came from
// exactly one place.
func setContentText(raw json.RawMessage, text string) (json.RawMessage, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		encoded, err := json.Marshal(text)
		return encoded, err == nil
	}
	parts := parseArray(raw)
	for i, part := range parts {
		obj, err := parseObject(part)
		if err != nil {
			continue
		}
		if t, ok := obj.str("text"); ok && t != "" {
			obj.set("text", text)
			encoded, err := json.Marshal(obj)
			if err != nil {
				return nil, false
			}
			parts[i] = encoded
			out, err := json.Marshal(parts)
			return out, err == nil
		}
	}
	return nil, false
}

package verexa

import (
	"encoding/json"
	"strings"
)

type chatEndpoint struct{}

func (chatEndpoint) messages(body jsonObject) []jsonObject {
	var out []jsonObject
	for _, m := range parseArray(body["messages"]) {
		obj, err := parseObject(m)
		if err != nil {
			obj = jsonObject{}
		}
		out = append(out, obj)
	}
	return out
}

func (e chatEndpoint) lastUser(messages []jsonObject) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if role, _ := messages[i].str("role"); role == "user" {
			return i
		}
	}
	return -1
}

func (e chatEndpoint) inputText(body jsonObject) (string, string) {
	messages := e.messages(body)
	system := ""
	for _, m := range messages {
		if role, _ := m.str("role"); role == "system" {
			system, _ = contentText(m["content"])
			break
		}
	}
	i := e.lastUser(messages)
	if i < 0 {
		return "", system
	}
	text, _ := contentText(messages[i]["content"])
	return text, system
}

func (e chatEndpoint) redactInput(body jsonObject, text string) bool {
	messages := e.messages(body)
	i := e.lastUser(messages)
	if i < 0 {
		return false
	}
	if _, single := contentText(messages[i]["content"]); !single {
		return false
	}
	content, ok := setContentText(messages[i]["content"], text)
	if !ok {
		return false
	}
	messages[i]["content"] = content
	body.set("messages", messages)
	return true
}

func (chatEndpoint) outputs(body jsonObject) []outputSlot {
	choicesRaw := parseArray(body["choices"])
	choices := make([]jsonObject, len(choicesRaw))
	var slots []outputSlot
	for i, raw := range choicesRaw {
		choice, err := parseObject(raw)
		if err != nil {
			choices[i] = nil
			continue
		}
		choices[i] = choice
		message := choice.object("message")
		text, ok := message.str("content")
		if !ok || text == "" {
			continue
		}
		slots = append(slots, outputSlot{text: text, set: func(redacted string) {
			message.set("content", redacted)
			choice.set("message", message)
			encoded := make([]json.RawMessage, len(choicesRaw))
			for j := range choicesRaw {
				encoded[j] = choicesRaw[j]
				if choices[j] != nil {
					encoded[j], _ = json.Marshal(choices[j])
				}
			}
			body.set("choices", encoded)
		}})
	}
	return slots
}

func (chatEndpoint) streamEvent(event []byte) streamEvent {
	var chunk struct {
		Choices []struct {
			Index int `json:"index"`
			Delta struct {
				Content *string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if json.Unmarshal(event, &chunk) != nil || len(chunk.Choices) == 0 || chunk.Choices[0].Delta.Content == nil {
		return streamEvent{}
	}
	return streamEvent{index: chunk.Choices[0].Index, text: *chunk.Choices[0].Delta.Content}
}

type responsesEndpoint struct{}

func (responsesEndpoint) inputText(body jsonObject) (string, string) {
	system, _ := body.str("instructions")
	if s, ok := body.str("input"); ok {
		return s, system
	}
	var texts []string
	for _, item := range parseArray(body["input"]) {
		var s string
		if json.Unmarshal(item, &s) == nil {
			if s != "" {
				texts = append(texts, s)
			}
			continue
		}
		obj, err := parseObject(item)
		if err != nil {
			continue
		}
		if t, _ := contentText(obj["content"]); t != "" {
			texts = append(texts, t)
		}
	}
	return strings.Join(texts, "\n"), system
}

func (responsesEndpoint) redactInput(body jsonObject, text string) bool {
	if _, ok := body.str("input"); !ok {
		return false
	}
	body.set("input", text)
	return true
}

func (responsesEndpoint) outputs(body jsonObject) []outputSlot {
	items := parseArray(body["output"])
	var slots []outputSlot
	for i, itemRaw := range items {
		item, err := parseObject(itemRaw)
		if err != nil {
			continue
		}
		parts := parseArray(item["content"])
		for j, partRaw := range parts {
			part, err := parseObject(partRaw)
			if err != nil {
				continue
			}
			if kind, _ := part.str("type"); kind != "output_text" {
				continue
			}
			text, ok := part.str("text")
			if !ok || text == "" {
				continue
			}
			slots = append(slots, outputSlot{text: text, set: func(redacted string) {
				part.set("text", redacted)
				parts[j], _ = json.Marshal(part)
				item.set("content", parts)
				items[i], _ = json.Marshal(item)
				body.set("output", items)
			}})
		}
	}
	return slots
}

func (responsesEndpoint) streamEvent(event []byte) streamEvent {
	var e struct {
		Type        string `json:"type"`
		Delta       string `json:"delta"`
		OutputIndex int    `json:"output_index"`
	}
	if json.Unmarshal(event, &e) != nil {
		return streamEvent{}
	}
	switch e.Type {
	case "response.output_text.delta":
		return streamEvent{index: e.OutputIndex, text: e.Delta}
	case "response.completed", "response.incomplete", "response.failed":
		return streamEvent{done: true}
	}
	return streamEvent{}
}

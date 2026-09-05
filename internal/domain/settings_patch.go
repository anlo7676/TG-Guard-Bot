package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// ApplySettingsPatch preserves omitted fields and rejects null or unknown values.
func ApplySettingsPatch(v *Settings, raw []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return fmt.Errorf("设置必须是 JSON 对象")
	}
	for key, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("%s 不能为 null", key)
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("仅允许一个设置对象")
	}
	if v.Rules == nil {
		v.Rules = map[string]RuleSetting{}
	}
	return v.Validate()
}
